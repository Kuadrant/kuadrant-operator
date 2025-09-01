package controller

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/kuadrant/kuadrant-operator/pkg/extension/types"
)

// +kubebuilder:rbac:groups=extensions.kuadrant.io,resources=telemetrypolicies,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=extensions.kuadrant.io,resources=telemetrypolicies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=extensions.kuadrant.io,resources=telemetrypolicies/finalizers,verbs=update

type TelemetryPolicyReconciler struct {
	types.ExtensionBase
}

func NewTelemetryPolicyReconciler() *TelemetryPolicyReconciler {
	return &TelemetryPolicyReconciler{}
}

func (r *TelemetryPolicyReconciler) Reconcile(_ context.Context, _ reconcile.Request, _ types.KuadrantCtx) (reconcile.Result, error) {
	return reconcile.Result{}, nil
}
