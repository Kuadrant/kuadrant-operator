package controllers

import (
	"context"

	extensioncontroller "github.com/kuadrant/kuadrant-operator/pkg/extension/controller"

	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type ExampleExtensionReconciler struct {
}

func (e *ExampleExtensionReconciler) Reconcile(ctx context.Context, _ reconcile.Request, _ *extensioncontroller.KuadrantCtx) (reconcile.Result, error) {
	logger := extensioncontroller.LoggerFromContext(ctx).WithName("ExampleExtensionReconciler")
	logger.Info("Reconciling ExampleExtension")

	_, err := extensioncontroller.DynamicClientFromContext(ctx)
	if err != nil {
		return reconcile.Result{}, err
	}
	return reconcile.Result{}, nil
}
