package controllers

import (
	"context"

	"github.com/go-logr/logr"
	"github.com/kuadrant/kuadrant-operator/pkg/extension/extensioncontroller"

	"k8s.io/client-go/dynamic"
	ctrlruntimemanager "sigs.k8s.io/controller-runtime/pkg/manager"
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

func (e *ExampleExtensionReconciler) SetupWithManager(mgr ctrlruntimemanager.Manager, logger logr.Logger) *extensioncontroller.ExtensionController {
	return extensioncontroller.NewExtensionController("example-extension-controller", mgr, e.client, logger, e.Reconcile)
}
