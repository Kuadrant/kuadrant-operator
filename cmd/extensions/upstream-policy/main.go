package main

import (
	"os"

	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/kuadrant/kuadrant-operator/cmd/extensions/upstream-policy/api/v1alpha1"
	"github.com/kuadrant/kuadrant-operator/cmd/extensions/upstream-policy/internal/controller"
	extcontroller "github.com/kuadrant/kuadrant-operator/pkg/extension/controller"
)

var (
	scheme = k8sruntime.NewScheme()
)

func init() {
	utilruntime.Must(v1alpha1.AddToScheme(scheme))
}

func main() {
	reconciler := controller.NewUpstreamPolicyReconciler()
	builder, logger := extcontroller.NewBuilder("upstream-policy-extension-controller")
	extController, err := builder.
		WithScheme(scheme).
		WithReconciler(reconciler.Reconcile).
		For(&v1alpha1.UpstreamPolicy{}).
		Build()
	if err != nil {
		logger.Error(err, "unable to create controller")
		os.Exit(1)
	}
	if err := extController.Start(ctrl.SetupSignalHandler()); err != nil {
		logger.Error(err, "unable to start extension controller")
		os.Exit(1)
	}
}
