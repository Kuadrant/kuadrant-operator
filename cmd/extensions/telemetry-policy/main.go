package main

import (
	"os"

	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/kuadrant/kuadrant-operator/cmd/extensions/telemetry-policy/api/v1alpha1"
	"github.com/kuadrant/kuadrant-operator/cmd/extensions/telemetry-policy/internal/controller"
	extcontroller "github.com/kuadrant/kuadrant-operator/pkg/extension/controller"
)

var (
	scheme = k8sruntime.NewScheme()
)

func init() {
	utilruntime.Must(v1alpha1.AddToScheme(scheme))
}

func main() {
	telemetryPolicyReconciler := controller.NewTelemetryPolicyReconciler()
	builder, logger := extcontroller.NewBuilder("telemetry-policy-extension-controller")
	controller, err := builder.
		WithScheme(scheme).
		WithReconciler(telemetryPolicyReconciler.Reconcile).
		For(&v1alpha1.TelemetryPolicy{}).
		Build()
	if err != nil {
		logger.Error(err, "unable to create controller")
		os.Exit(1)
	}
	if err := controller.Start(ctrl.SetupSignalHandler()); err != nil {
		logger.Error(err, "unable to start extensioncontroller")
		os.Exit(1)
	}
}
