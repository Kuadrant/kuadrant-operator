package controller

import (
	"context"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/kuadrant/kuadrant-operator/internal/reconcilers"
	"github.com/kuadrant/kuadrant-operator/pkg/extension/types"
	"github.com/kuadrant/kuadrant-operator/pkg/extension/utils"
)

// +kubebuilder:rbac:groups=kuadrant.io,resources=planpolicies,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=kuadrant.io,resources=planpolicies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kuadrant.io,resources=planpolicies/finalizers,verbs=update

// +kubebuilder:rbac:groups=kuadrant.io,resources=ratelimitpolicies,verbs=create;delete

type PlanPolicyReconciler struct {
	*reconcilers.BaseReconciler
	logger logr.Logger
}

func NewPlanPolicyReconciler() *PlanPolicyReconciler {
	return &PlanPolicyReconciler{
		BaseReconciler: reconcilers.NewLazyBaseReconciler(),
	}
}

func (r *PlanPolicyReconciler) WithLogger(logger logr.Logger) *PlanPolicyReconciler {
	r.logger = logger
	return r
}

func (r *PlanPolicyReconciler) Reconcile(ctx context.Context, request reconcile.Request, kuadrantCtx types.KuadrantCtx) (reconcile.Result, error) {
	r.WithLogger(utils.LoggerFromContext(ctx).WithName("PlanPolicyReconciler"))
	r.logger.Info("Reconciling PlanPolicy")

	return reconcile.Result{}, nil
}
