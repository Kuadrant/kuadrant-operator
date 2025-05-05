package controller

import (
	"context"

	"github.com/kuadrant/kuadrant-operator/pkg/extension/types"
	"github.com/kuadrant/kuadrant-operator/pkg/extension/utils"

	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type OIDCPolicyReconciler struct {
}

func (e *OIDCPolicyReconciler) Reconcile(ctx context.Context, _ reconcile.Request, _ *types.KuadrantCtx) (reconcile.Result, error) {
	logger := utils.LoggerFromContext(ctx).WithName("OIDCPolicyReconciler")
	logger.Info("Reconciling OIDCPolicy")

	_, err := utils.DynamicClientFromContext(ctx)
	if err != nil {
		return reconcile.Result{}, err
	}
	return reconcile.Result{}, nil
}
