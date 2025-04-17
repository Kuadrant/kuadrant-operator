package controllers

import (
	"context"

	"github.com/kuadrant/kuadrant-operator/pkg/extension/extensioncontroller"

	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type ExampleExtensionReconciler struct {
	client *dynamic.DynamicClient
}

func NewExampleReconciler(client *dynamic.DynamicClient) *ExampleExtensionReconciler {
	return &ExampleExtensionReconciler{
		client: client,
	}
}

func (e *ExampleExtensionReconciler) Reconcile(_ context.Context, _ reconcile.Request, _ *extensioncontroller.KuadrantCtx) (reconcile.Result, error) {
	return reconcile.Result{}, nil
}
