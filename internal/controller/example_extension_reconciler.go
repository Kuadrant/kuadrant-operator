package controllers

import (
	"context"

	"github.com/kuadrant/kuadrant-operator/pkg/extension/extensioncontroller"

	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type ExampleExtensionReconciler struct {
}

func (e *ExampleExtensionReconciler) Reconcile(_ context.Context, _ reconcile.Request, _ *extensioncontroller.KuadrantCtx) (reconcile.Result, error) {
	return reconcile.Result{}, nil
}
