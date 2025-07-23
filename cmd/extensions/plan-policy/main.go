package main

import (
	"os"

	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	ctrl "sigs.k8s.io/controller-runtime"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	kuadrantv1alpha1 "github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	"github.com/kuadrant/kuadrant-operator/cmd/extensions/plan-policy/internal/controller"
	extcontroller "github.com/kuadrant/kuadrant-operator/pkg/extension/controller"
)

var (
	scheme = k8sruntime.NewScheme()
)

func init() {
	utilruntime.Must(kuadrantv1.AddToScheme(scheme))
	utilruntime.Must(kuadrantv1alpha1.AddToScheme(scheme))
}

func main() {
	planPolicyReconciler := controller.NewPlanPolicyReconciler()
	builder, logger := extcontroller.NewBuilder("plan-policy-extension-controller")
	controller, err := builder.
		WithScheme(scheme).
		WithReconciler(planPolicyReconciler.Reconcile).
		For(&kuadrantv1alpha1.PlanPolicy{}).
		Build()
	if err != nil {
		logger.Error(err, "unable to create controller")
		os.Exit(1)
	}

	planPolicyReconciler.SetupWithManager(controller.Manager())

	if err = controller.Start(ctrl.SetupSignalHandler()); err != nil {
		logger.Error(err, "unable to start extension controller")
		os.Exit(1)
	}
}
