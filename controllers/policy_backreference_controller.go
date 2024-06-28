package controllers

import (
	"context"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/handler"

	"github.com/go-logr/logr"
	"github.com/google/uuid"
	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/pkg/library/mappers"
	"github.com/kuadrant/kuadrant-operator/pkg/library/reconcilers"
	"github.com/kuadrant/kuadrant-operator/pkg/rlptools"
)

// PolicyBackReferenceReconciler reconciles annotation on objects targeted by policies
// The goal is to ensure only one policy of a kind is referencing the object at a given time.
type PolicyBackReferenceReconciler struct {
	*reconcilers.BaseReconciler
	policyType     kuadrantgatewayapi.PolicyType
	gatewayAPIType kuadrantgatewayapi.GatewayAPIType
}

//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=httproutes,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways,verbs=get;list;watch;update;patch

// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.10.0/pkg/reconcile
func (r *PolicyBackReferenceReconciler) Reconcile(eventCtx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := r.Logger().WithValues("req", req.NamespacedName, "request id", uuid.NewString())
	// TODO: to be included in main (and in integration tests)
	//logger := r.Logger().WithName("policybackreferencereconciler").WithName(r.policyType.GetGVK().Kind).WithName(r.gatewayAPIType.GetGVK().Kind),
	logger.V(1).Info("Reconciling RateLimitPolicy instances")
	ctx := logr.NewContext(eventCtx, logger)

	// TODO read resource and check it is not being deleted

	topology, err := rlptools.Topology(ctx, r.Client())
	if err != nil {
		return err
	}
	// TODO
	// Get all policies from a kind
	// build topology
	// get attached policies
	//  then -> run ReconcilePolicyReferenceOnObject
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
		mappers.WithLogger(r.Logger().
			WithName("policytotargetrefmapper").
			WithName(r.policyType.GetGVK().Kind).
			WithName(r.gatewayAPIType.GetGVK().Kind),
		),
	)

	return ctrl.NewControllerManagedBy(mgr).
		For(r.gatewayAPIType.GetInstance()).
		Watches(
			r.policyType.GetInstance(),
			handler.EnqueueRequestsFromMapFunc(policyToTargetRefEventMapper.Map),
		).
		Complete(r)
}
