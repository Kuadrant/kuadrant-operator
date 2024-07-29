package controllers

import (
	"context"

	egv1alpha1 "github.com/envoyproxy/gateway/api/v1alpha1"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/kuadrant/kuadrant-operator/pkg/library/reconcilers"
)

// RateLimitingEnvoyExtensionPolicyReconciler reconciles an EnvoyGateway EnvoyExtensionPolicy object for rate limiting
type RateLimitingEnvoyExtensionPolicyReconciler struct {
	*reconcilers.BaseReconciler
}

//+kubebuilder:rbac:groups=gateway.envoyproxy.io,resources=envoyextensionpolicies,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=gateways,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=gateway.networking.k8s.io,resources=httproutes,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=kuadrant.io,resources=ratelimitpolicies,verbs=get;list;watch;update;patch

// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.10.0/pkg/reconcile
func (r *RateLimitingEnvoyExtensionPolicyReconciler) Reconcile(eventCtx context.Context, req ctrl.Request) (ctrl.Result, error) {
	a := &egv1alpha1.EnvoyExtensionPolicy{}
	if a != nil {
		return ctrl.Result{}, nil
	}
	return ctrl.Result{}, nil
}
