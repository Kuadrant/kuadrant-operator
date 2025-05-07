package controllers

import (
	"context"

	"github.com/kuadrant/kuadrant-operator/pkg/extension/types"
	"github.com/kuadrant/kuadrant-operator/pkg/extension/utils"

	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type ExampleExtensionReconciler struct {
}

func (e *ExampleExtensionReconciler) Reconcile(ctx context.Context, _ reconcile.Request, _ types.KuadrantCtx) (reconcile.Result, error) {
	logger := utils.LoggerFromContext(ctx).WithName("ExampleExtensionReconciler")
	logger.Info("Reconciling ExampleExtension")

	// kuadrantCtx.Resolve()
	_, err := utils.DynamicClientFromContext(ctx)
	if err != nil {
		return reconcile.Result{}, err
	}
	return reconcile.Result{}, nil
}
