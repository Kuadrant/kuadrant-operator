package controllers

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/source"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	api "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
	"github.com/kuadrant/kuadrant-operator/pkg/reconcilers"
)

const authPolicyFinalizer = "authpolicy.kuadrant.io/finalizer"

// AuthPolicyReconciler reconciles a AuthPolicy object
type AuthPolicyReconciler struct {
	reconcilers.TargetRefReconciler
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

	markedForDeletion := ap.GetDeletionTimestamp() != nil

	// fetch the target network object
	targetObj, err := r.FetchValidTargetRef(ctx, ap.GetTargetRef(), ap.Namespace)
	if err != nil {
		if apierrors.IsNotFound(err) {
			logger.V(1).Info("Network object not found. Cleaning up")
			delResErr := r.deleteResources(ctx, ap, nil)
			if markedForDeletion {
				return ctrl.Result{}, delResErr
			}
			if delResErr == nil {
				delResErr = err
			}
			return r.reconcileStatus(ctx, ap, delResErr)
		}
		return ctrl.Result{}, err
	}

	// handle authpolicy marked for deletion
	if markedForDeletion {
		if controllerutil.ContainsFinalizer(ap, authPolicyFinalizer) {
			logger.V(1).Info("Handling removal of authpolicy object")

			if err := r.deleteResources(ctx, ap, targetObj); err != nil {
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
	specResult, specErr := r.reconcileResources(ctx, ap, targetObj)
	if specErr == nil && specResult.Requeue {
		logger.V(1).Info("Reconciling spec not finished. Requeueing.")
		return specResult, nil
	}

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

	logger.Info("AuthPolicy reconciled successfully")
	return ctrl.Result{}, nil
}

func (r *AuthPolicyReconciler) reconcileResources(ctx context.Context, ap *api.AuthPolicy, targetObj client.Object) (ctrl.Result, error) {
	if err := ap.Validate(); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.validateHierarchicalRules(ctx, ap, targetObj); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.reconcileNetworkResourceDirectBackReference(ctx, ap, targetObj); err != nil { // direct back ref
		return ctrl.Result{}, err
	}

	gatewayDiffObj, err := r.ComputeGatewayDiffs(ctx, ap, targetObj, &common.KuadrantAuthPolicyRefsConfig{})
	if err != nil {
		return ctrl.Result{}, err
	}

	if err := r.reconcileIstioAuthorizationPolicies(ctx, ap, targetObj, gatewayDiffObj); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.reconcileAuthConfigs(ctx, ap, targetObj); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.ReconcileGatewayPolicyReferences(ctx, ap, gatewayDiffObj); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *AuthPolicyReconciler) deleteResources(ctx context.Context, ap *api.AuthPolicy, targetObj client.Object) error {
	gatewayDiffObj, err := r.ComputeGatewayDiffs(ctx, ap, targetObj, &common.KuadrantAuthPolicyRefsConfig{})
	if err != nil {
		return err
	}

	if err := r.ReconcileGatewayPolicyReferences(ctx, ap, gatewayDiffObj); err != nil {
		return err
	}

	if err := r.deleteIstioAuthorizationPolicies(ctx, ap, gatewayDiffObj); err != nil {
		return err
	}

	if err := r.deleteAuthConfigs(ctx, ap); err != nil {
		return err
	}

	if targetObj != nil {
		return r.deleteNetworkResourceDirectBackReference(ctx, ap, targetObj) // direct back ref
	}
	return nil
}

func (r *AuthPolicyReconciler) validateHierarchicalRules(ctx context.Context, ap *api.AuthPolicy, targetObj client.Object) error {
	targetHostnames, err := r.TargetHostnames(ctx, targetObj)
	if err != nil {
		return err
	}

	ruleHosts := make([]string, 0)
	for _, rule := range ap.Spec.AuthRules {
		ruleHosts = append(ruleHosts, rule.Hosts...)
	}

	if valid, invalidHost := common.ValidSubdomains(targetHostnames, ruleHosts); !valid {
		return fmt.Errorf("rule host (%s) does not follow any hierarchical constraints", invalidHost)
	}

	return nil
}

// Ensures only one RLP targets the network resource
func (r *AuthPolicyReconciler) reconcileNetworkResourceDirectBackReference(ctx context.Context, ap *api.AuthPolicy, targetObj client.Object) error {
	return r.ReconcileTargetBackReference(ctx, client.ObjectKeyFromObject(ap), targetObj, common.AuthPolicyBackRefAnnotation)
}

func (r *AuthPolicyReconciler) deleteNetworkResourceDirectBackReference(ctx context.Context, ap *api.AuthPolicy, targetObj client.Object) error {
	return r.DeleteTargetBackReference(ctx, client.ObjectKeyFromObject(ap), targetObj, common.AuthPolicyBackRefAnnotation)
}

// SetupWithManager sets up the controller with the Manager.
func (r *AuthPolicyReconciler) SetupWithManager(mgr ctrl.Manager) error {
	HTTPRouteEventMapper := &HTTPRouteEventMapper{
		Logger: r.Logger().WithName("httpRouteHandler"),
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&api.AuthPolicy{}).
		Watches(
			&source.Kind{Type: &gatewayapiv1alpha2.HTTPRoute{}},
			handler.EnqueueRequestsFromMapFunc(HTTPRouteEventMapper.MapToAuthPolicy),
		).
		Watches(&source.Kind{Type: &gatewayapiv1alpha2.Gateway{}},
			handler.EnqueueRequestsFromMapFunc(HTTPRouteEventMapper.MapToAuthPolicy)).
		Complete(r)
}
