package controllers

import (
	"context"

	"github.com/go-logr/logr"
	"k8s.io/client-go/dynamic"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlruntimemanager "sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var _ reconcile.Reconciler = &ExampleExtensionReconciler{}

type ExampleExtensionReconciler struct {
	client *dynamic.DynamicClient
	logger logr.Logger
}

func NewExampleReconciler(client *dynamic.DynamicClient, logger logr.Logger) *ExampleExtensionReconciler {
	return &ExampleExtensionReconciler{
		client: client,
		logger: logger,
	}
}

func (e *ExampleExtensionReconciler) Reconcile(_ context.Context, _ reconcile.Request) (reconcile.Result, error) {
	return reconcile.Result{}, nil
}

func (e *ExampleExtensionReconciler) SetupWithManager(mgr ctrlruntimemanager.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).Named("example-extension-controller").Complete(e)
}
