package main

import (
	"os"

	corev1 "k8s.io/api/core/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	ctrl "sigs.k8s.io/controller-runtime"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	controllers "github.com/kuadrant/kuadrant-operator/internal/controller"
	"github.com/kuadrant/kuadrant-operator/pkg/extension/extensioncontroller"
)

var (
	scheme = k8sruntime.NewScheme()
)

func init() {
	utilruntime.Must(corev1.AddToScheme(scheme))
	utilruntime.Must(kuadrantv1.AddToScheme(scheme))
}

func main() {
	exampleReconciler := controllers.ExampleExtensionReconciler{}
	builder, logger := extensioncontroller.NewBuilder("example-extension-controller")
	controller, err := builder.
		WithScheme(scheme).
		WithReconciler(exampleReconciler.Reconcile).
		Watches(&kuadrantv1.AuthPolicy{}).
		Build()
	if err != nil {
		logger.Error(err, "unable to create controller")
		os.Exit(1)
	}

	if err = controller.Start(ctrl.SetupSignalHandler()); err != nil {
		logger.Error(err, "unable to start extension controller")
		os.Exit(1)
	}
}
