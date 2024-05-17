package controllers

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/go-logr/logr"
	authorinoapi "github.com/kuadrant/authorino/api/v1beta2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	api "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
	"github.com/kuadrant/kuadrant-operator/pkg/library/mappers"
	"github.com/kuadrant/kuadrant-operator/pkg/library/reconcilers"
)

const authPolicyFinalizer = "authpolicy.kuadrant.io/finalizer"

// AuthPolicyReconciler reconciles a AuthPolicy object
type AuthPolicyReconciler struct {
	*reconcilers.BaseReconciler
	TargetRefReconciler reconcilers.TargetRefReconciler
	// AffectedPolicyMap tracks the affected policies to report their status.
	AffectedPolicyMap *kuadrant.AffectedPolicyMap
}

//+kubebuilder:rbac:groups=kuadrant.io,resources=authpolicies,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=kuadrant.io,resources=authpolicies/finalizers,verbs=update
//+kubebuilder:rbac:groups=kuadrant.io,resources=authpolicies/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=security.istio.io,resources=authorizationpolicies,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=authorino.kuadrant.io,resources=authconfigs,verbs=get;list;watch;create;update;patch;delete

func (r *AuthPolicyReconciler) Reconcile(eventCtx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Logger().WithValues("AuthPolicy", req.NamespacedName)
	logger.Info("Reconciling AuthPolicy")
	ctx := logr.NewContext(eventCtx, logger)

	// fetch the authpolicy
	ap := &api.AuthPolicy{}
	if err := r.Client().Get(ctx, req.NamespacedName, ap); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("no AuthPolicy found")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "failed to get AuthPolicy")
		return ctrl.Result{}, err
	}

	if logger.V(1).Enabled() {
		jsonData, err := json.MarshalIndent(ap, "", "  ")
		if err != nil {
			return ctrl.Result{}, err
		}
		logger.V(1).Info(string(jsonData))
	}

	markedForDeletion := ap.GetDeletionTimestamp() != nil

	// fetch the target network object
	targetNetworkObject, err := reconcilers.FetchTargetRefObject(ctx, r.Client(), ap.GetTargetRef(), ap.Namespace)
	if err != nil {
		if !markedForDeletion {
			if apierrors.IsNotFound(err) {
				logger.V(1).Info("Network object not found. Cleaning up")
				delResErr := r.deleteResources(ctx, ap, nil)
				if delResErr == nil {
					delResErr = err
				}
				return r.reconcileStatus(ctx, ap, kuadrant.NewErrTargetNotFound(ap.Kind(), ap.GetTargetRef(), delResErr))
			}
			return ctrl.Result{}, err
		}
		targetNetworkObject = nil // we need the object set to nil when there's an error, otherwise deleting the resources (when marked for deletion) will panic
	}

	// handle authpolicy marked for deletion
	if markedForDeletion {
		if controllerutil.ContainsFinalizer(ap, authPolicyFinalizer) {
			logger.V(1).Info("Handling removal of authpolicy object")

			if err := r.deleteResources(ctx, ap, targetNetworkObject); err != nil {
				return ctrl.Result{}, err
			}

			logger.Info("removing finalizer")
			if err := r.RemoveFinalizer(ctx, ap, authPolicyFinalizer); err != nil {
				return ctrl.Result{}, err
			}
		}

		return ctrl.Result{}, nil
	}

	// add finalizer to the authpolicy
	if !controllerutil.ContainsFinalizer(ap, authPolicyFinalizer) {
		if err := r.AddFinalizer(ctx, ap, authPolicyFinalizer); client.IgnoreNotFound(err) != nil {
			return ctrl.Result{Requeue: true}, err
		}
	}

	// reconcile the authpolicy spec
	specErr := r.reconcileResources(ctx, ap, targetNetworkObject)

	// reconcile authpolicy status
	statusResult, statusErr := r.reconcileStatus(ctx, ap, specErr)

	if specErr != nil {
		return ctrl.Result{}, specErr
	}

	if statusErr != nil {
		return ctrl.Result{}, statusErr
	}

	if statusResult.Requeue {
		logger.V(1).Info("Reconciling status not finished. Requeueing.")
		return statusResult, nil
	}

	// trigger concurrent reconciliations of possibly affected gateway policies
	switch route := targetNetworkObject.(type) {
	case *gatewayapiv1.HTTPRoute:
		if err := r.reconcileRouteParentGatewayPolicies(ctx, route); err != nil {
			return ctrl.Result{}, err
		}
	}

	logger.Info("AuthPolicy reconciled successfully")
	return ctrl.Result{}, nil
}

// validate performs validation before proceeding with the reconcile loop, returning a common.ErrInvalid on any failing validation
func (r *AuthPolicyReconciler) validate(ap *api.AuthPolicy, targetNetworkObject client.Object) error {
	if err := ap.Validate(); err != nil {
		return kuadrant.NewErrInvalid(ap.Kind(), err)
	}

	if err := kuadrant.ValidateHierarchicalRules(ap, targetNetworkObject); err != nil {
		return kuadrant.NewErrInvalid(ap.Kind(), err)
	}

	return nil
}

func (r *AuthPolicyReconciler) reconcileResources(ctx context.Context, ap *api.AuthPolicy, targetNetworkObject client.Object) error {
	if err := r.validate(ap, targetNetworkObject); err != nil {
		return err
	}

	// reconcile based on gateway diffs
	gatewayDiffObj, err := reconcilers.ComputeGatewayDiffs(ctx, r.Client(), ap, targetNetworkObject)
	if err != nil {
		return err
	}

	if err := r.reconcileIstioAuthorizationPolicies(ctx, ap, targetNetworkObject, gatewayDiffObj); err != nil {
		return fmt.Errorf("reconcile AuthorizationPolicy error %w", err)
	}

	if err := r.reconcileAuthConfigs(ctx, ap, targetNetworkObject); err != nil {
		return fmt.Errorf("reconcile AuthConfig error %w", err)
	}

	// if the AuthPolicy(ap) targets a Gateway then all policies attached to that Gateway need to be checked.
	// this is due to not knowing if the Gateway AuthPolicy was updated to include or remove the overrides section.
	switch obj := targetNetworkObject.(type) {
	case *gatewayapiv1.Gateway:
		gw := kuadrant.GatewayWrapper{Gateway: obj, Referrer: ap}
		apKey := client.ObjectKeyFromObject(ap)
		for _, policyKey := range gw.PolicyRefs() {
			if policyKey == apKey {
				continue
			}

			ref := &api.AuthPolicy{}
			err = r.Client().Get(ctx, policyKey, ref)
			if err != nil {
				return err
			}

			refNetworkObject, err := reconcilers.FetchTargetRefObject(ctx, r.Client(), ref.GetTargetRef(), ref.Namespace)
			if err != nil {
				return err
			}

			if err = r.reconcileAuthConfigs(ctx, ref, refNetworkObject); err != nil {
				return err
			}
		}
	}

	// set direct back ref - i.e. claim the target network object as taken asap
	if err := r.reconcileNetworkResourceDirectBackReference(ctx, ap, targetNetworkObject); err != nil {
		return fmt.Errorf("reconcile TargetBackReference error %w", err)
	}

	// set annotation of policies affecting the gateway - should be the last step, only when all the reconciliation steps succeed
	if err := r.TargetRefReconciler.ReconcileGatewayPolicyReferences(ctx, ap, gatewayDiffObj); err != nil {
		return fmt.Errorf("ReconcileGatewayPolicyReferences error %w", err)
	}

	return nil
}

func (r *AuthPolicyReconciler) deleteResources(ctx context.Context, ap *api.AuthPolicy, targetNetworkObject client.Object) error {
	// delete based on gateway diffs
	gatewayDiffObj, err := reconcilers.ComputeGatewayDiffs(ctx, r.Client(), ap, targetNetworkObject)
	if err != nil {
		return err
	}

	if err := r.deleteIstioAuthorizationPolicies(ctx, ap, gatewayDiffObj); err != nil {
		return err
	}

	// remove direct back ref
	if targetNetworkObject != nil {
		if err := r.deleteNetworkResourceDirectBackReference(ctx, targetNetworkObject, ap); err != nil {
			return err
		}
	}

	// update annotation of policies affecting the gateway
	return r.TargetRefReconciler.ReconcileGatewayPolicyReferences(ctx, ap, gatewayDiffObj)
}

// Ensures only one RLP targets the network resource
func (r *AuthPolicyReconciler) reconcileNetworkResourceDirectBackReference(ctx context.Context, ap *api.AuthPolicy, targetNetworkObject client.Object) error {
	return r.TargetRefReconciler.ReconcileTargetBackReference(ctx, ap, targetNetworkObject, ap.DirectReferenceAnnotationName())
}

func (r *AuthPolicyReconciler) deleteNetworkResourceDirectBackReference(ctx context.Context, targetNetworkObject client.Object, ap *api.AuthPolicy) error {
	return r.TargetRefReconciler.DeleteTargetBackReference(ctx, targetNetworkObject, ap.DirectReferenceAnnotationName())
}

// reconcileRouteParentGatewayPolicies triggers the concurrent reconciliation of all policies that target gateways that are parents of a route
func (r *AuthPolicyReconciler) reconcileRouteParentGatewayPolicies(ctx context.Context, route *gatewayapiv1.HTTPRoute) error {
	logger, err := logr.FromContext(ctx)
	if err != nil {
		return err
	}
	mapper := HTTPRouteParentRefsEventMapper{
		Logger: logger,
		Client: r.Client(),
	}
	requests := mapper.MapToAuthPolicy(route)
	for i := range requests {
		request := requests[i]
		go r.Reconcile(context.Background(), request)
	}
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *AuthPolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	ok, err := kuadrantgatewayapi.IsGatewayAPIInstalled(mgr.GetRESTMapper())
	if err != nil {
		return err
	}
	if !ok {
		r.Logger().Info("AuthPolicy controller disabled. GatewayAPI was not found")
		return nil
	}

	httpRouteEventMapper := mappers.NewHTTPRouteEventMapper(mappers.WithLogger(r.Logger().WithName("httpRouteEventMapper")))
	gatewayEventMapper := mappers.NewGatewayEventMapper(mappers.WithLogger(r.Logger().WithName("gatewayEventMapper")))

	return ctrl.NewControllerManagedBy(mgr).
		For(&api.AuthPolicy{}).
		Owns(&authorinoapi.AuthConfig{}).
		Watches(
			&gatewayapiv1.HTTPRoute{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, object client.Object) []reconcile.Request {
				return httpRouteEventMapper.MapToPolicy(object, &api.AuthPolicy{})
			}),
		).
		Watches(&gatewayapiv1.Gateway{},
			handler.EnqueueRequestsFromMapFunc(func(ctx context.Context, object client.Object) []reconcile.Request {
				return gatewayEventMapper.MapToPolicy(object, &api.AuthPolicy{})
			}),
		).
		Complete(r)
}
