package controllers

import (
	"context"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	"github.com/google/uuid"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
	"github.com/kuadrant/kuadrant-operator/pkg/library/mappers"
	"github.com/kuadrant/kuadrant-operator/pkg/library/reconcilers"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
)

// PolicyBackReferenceReconciler reconciles annotation on objects targeted by policies
// The goal is to ensure only one policy of a kind is referencing the object at a given time.
type PolicyBackReferenceReconciler struct {
	*reconcilers.BaseReconciler
	policyType     kuadrantgatewayapi.PolicyType
	gatewayAPIType kuadrantgatewayapi.GatewayAPIType
}

func NewPolicyBackReferenceReconciler(mgr ctrl.Manager,
	policyType kuadrantgatewayapi.PolicyType,
	gatewayAPIType kuadrantgatewayapi.GatewayAPIType,
) *PolicyBackReferenceReconciler {
	return &PolicyBackReferenceReconciler{
		BaseReconciler: reconcilers.NewBaseReconciler(
			mgr.GetClient(), mgr.GetScheme(), mgr.GetAPIReader(),
			log.Log.WithName("policybackreferencereconciler").
				WithName("for").WithName(strings.ToLower(policyType.GetGVK().Kind)).
				WithName("on").WithName(strings.ToLower(gatewayAPIType.GetGVK().Kind)),
			mgr.GetEventRecorderFor(
				fmt.Sprintf("policybackreferencereconciler.for.%s.on.%s",
					strings.ToLower(policyType.GetGVK().Kind),
					strings.ToLower(gatewayAPIType.GetGVK().Kind),
				),
			),
		),
		policyType:     policyType,
		gatewayAPIType: gatewayAPIType,
	}
}

//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=httproutes,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways,verbs=get;list;watch;update;patch

// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.10.0/pkg/reconcile
func (r *PolicyBackReferenceReconciler) Reconcile(eventCtx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Logger().WithValues("req", req.NamespacedName, "request id", uuid.NewString())
	// TODO: to be included in main (and in integration tests)
	//logger := r.Logger().WithName("policybackreferencereconciler").WithName(r.policyType.GetGVK().Kind).WithName(r.gatewayAPIType.GetGVK().Kind),
	logger.V(1).Info("Reconciling policy backreferences")
	ctx := logr.NewContext(eventCtx, logger)

	gatewayAPIobj := r.gatewayAPIType.GetInstance()
	err := r.Client().Get(ctx, req.NamespacedName, gatewayAPIobj)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			logger.Info("resource not found. Ignoring since object must have been deleted")
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return ctrl.Result{}, err
	}

	// Ignore deleted gatewayAPI objects, this can happen when foregroundDeletion is enabled
	// https://kubernetes.io/docs/concepts/workloads/controllers/garbage-collection/#foreground-cascading-deletion
	if gatewayAPIobj.GetDeletionTimestamp() != nil {
		return ctrl.Result{}, nil
	}

	policies, err := r.policyType.GetList(ctx, r.Client())
	logger.V(1).Info("list policies", "#items", len(policies), "err", err)
	if err != nil {
		return ctrl.Result{}, err
	}

	attachedPolicies := utils.Filter(policies, func(p kuadrantgatewayapi.Policy) bool {
		group := string(p.GetTargetRef().Group)
		kind := string(p.GetTargetRef().Kind)
		name := string(p.GetTargetRef().Name)
		namespace := ptr.Deref(p.GetTargetRef().Namespace, gatewayapiv1.Namespace(p.GetNamespace()))

		return group == r.gatewayAPIType.GetGVK().Group &&
			kind == r.gatewayAPIType.GetGVK().Kind &&
			name == gatewayAPIobj.GetName() &&
			namespace == gatewayapiv1.Namespace(gatewayAPIobj.GetNamespace())
	})

	err = kuadrant.ReconcilePolicyReferenceOnObject(
		ctx, r.Client(), r.policyType.GetGVK(),
		gatewayAPIobj, attachedPolicies,
	)
	if err != nil {
		// Ignore conflicts, resource might just be outdated.
		if apierrors.IsConflict(err) {
			logger.V(1).Info("Failed to update status: resource might just be outdated", "error", err)
			return reconcile.Result{Requeue: true}, nil
		}

		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *PolicyBackReferenceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	ok, err := kuadrantgatewayapi.IsGatewayAPIInstalled(mgr.GetRESTMapper())
	if err != nil {
		return err
	}
	if !ok {
		r.Logger().Info("PolicyBackReferenceReconciler controller disabled. GatewayAPI was not found",
			"policyType", r.policyType.GetGVK().Kind,
			"gatewayAPIType", r.gatewayAPIType.GetGVK().Kind,
		)
		return nil
	}

	policyToTargetRefEventMapper := mappers.NewPolicyToTargetRefMapper(
		r.gatewayAPIType,
		mappers.WithLogger(r.Logger().WithName("mapper")),
	)

	return ctrl.NewControllerManagedBy(mgr).
		For(r.gatewayAPIType.GetInstance()).
		Watches(
			r.policyType.GetInstance(),
			handler.EnqueueRequestsFromMapFunc(policyToTargetRefEventMapper.Map),
		).
		Complete(r)
}
