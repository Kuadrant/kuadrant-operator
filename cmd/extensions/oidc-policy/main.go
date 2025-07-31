package main

import (
	"os"

	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kuadrant/kuadrant-operator/cmd/extensions/oidc-policy/internal/controller"

	corev1 "k8s.io/api/core/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	ctrl "sigs.k8s.io/controller-runtime"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	kuadrantv1alpha1 "github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	extcontroller "github.com/kuadrant/kuadrant-operator/pkg/extension/controller"
)

var (
	scheme = k8sruntime.NewScheme()
)

func init() {
	utilruntime.Must(corev1.AddToScheme(scheme))
	utilruntime.Must(gatewayapiv1.Install(scheme))
	utilruntime.Must(kuadrantv1.AddToScheme(scheme))
	utilruntime.Must(kuadrantv1alpha1.AddToScheme(scheme))
}

func main() {
	oidcPolicyReconciler := controller.NewOIDCPolicyReconciler()
	builder, logger := extcontroller.NewBuilder("oidc-policy-controller")
	extController, err := builder.
		WithScheme(scheme).
		WithReconciler(oidcPolicyReconciler.Reconcile).
		For(&kuadrantv1alpha1.OIDCPolicy{}).
		Owns(&kuadrantv1.AuthPolicy{}).
		Owns(&gatewayapiv1.HTTPRoute{}).
		Build()
	if err != nil {
		logger.Error(err, "unable to create controller")
		os.Exit(1)
	}

	if err = extController.Start(ctrl.SetupSignalHandler()); err != nil {
		logger.Error(err, "unable to start extension controller")
		os.Exit(1)
	}
}
