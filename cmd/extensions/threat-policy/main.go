package main

import (
	"os"

	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/kuadrant/kuadrant-operator/cmd/extensions/threat-policy/api/v1alpha1"
	"github.com/kuadrant/kuadrant-operator/cmd/extensions/threat-policy/internal/controller"
	extcontroller "github.com/kuadrant/kuadrant-operator/pkg/extension/controller"
)

var (
	scheme = k8sruntime.NewScheme()
)

func init() {
	utilruntime.Must(v1alpha1.AddToScheme(scheme))
}

func main() {
	reconciler := controller.NewThreatPolicyReconciler()
	builder, logger := extcontroller.NewBuilder("threat-policy-extension-controller")
	extController, err := builder.
		WithScheme(scheme).
		WithReconciler(reconciler.Reconcile).
		For(&v1alpha1.ThreatPolicy{}).
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
