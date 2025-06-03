package controller

import (
	"context"

	"github.com/go-logr/logr"
	"github.com/kuadrant/kuadrant-operator/pkg/extension/types"
	"github.com/kuadrant/kuadrant-operator/pkg/extension/utils"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// +kubebuilder:rbac:groups=kuadrant.io,resources=planpolicies,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=kuadrant.io,resources=planpolicies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=kuadrant.io,resources=planpolicies/finalizers,verbs=update

// +kubebuilder:rbac:groups=kuadrant.io,resources=ratelimitpolicies,verbs=create;delete

type PlanPolicyReconciler struct {
	logger logr.Logger
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
