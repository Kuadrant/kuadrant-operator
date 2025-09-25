package controller

import (
	"context"
	"fmt"

	"k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/kuadrant/kuadrant-operator/cmd/extensions/telemetry-policy/api/v1alpha1"
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

func (r *TelemetryPolicyReconciler) Reconcile(ctx context.Context, request reconcile.Request, kuadrantCtx types.KuadrantCtx) (reconcile.Result, error) {
	if err := r.Configure(ctx); err != nil {
		return reconcile.Result{}, fmt.Errorf("failed to configure extension: %w", err)
	}
	r.Logger.Info("reconciling telemetrypolicies started")
	defer r.Logger.Info("reconciling telemetrypolicies completed")

	telemetryPolicy := &v1alpha1.TelemetryPolicy{}
	if err := r.Client.Get(ctx, request.NamespacedName, telemetryPolicy); err != nil {
		if errors.IsNotFound(err) {
			r.Logger.Error(err, "telemetrypolicy not found")
			return reconcile.Result{}, nil
		}
		r.Logger.Error(err, "failed to retrieve telemetrypolicy")
		return reconcile.Result{}, err
	}

	if telemetryPolicy.GetDeletionTimestamp() != nil {
		r.Logger.Info("telemetrypolicy marked for deletion")
		return reconcile.Result{}, nil
	}

	_, specErr := r.reconcileSpec(ctx, telemetryPolicy, kuadrantCtx)
	if specErr != nil {
		return reconcile.Result{}, specErr
	}

	return reconcile.Result{}, nil
}

func (r *TelemetryPolicyReconciler) reconcileSpec(ctx context.Context, telemetryPolicy *v1alpha1.TelemetryPolicy, kuadrantCtx types.KuadrantCtx) (*v1alpha1.TelemetryPolicyStatus, error) {
	for binding, expression := range telemetryPolicy.Spec.Metrics.Default.Labels {
		if err := kuadrantCtx.AddDataTo(ctx, telemetryPolicy, types.DomainRequest, types.KuadrantMetricBinding(binding), expression); err != nil {
			r.Logger.Error(err, "failed to add data to request domain")
			return &v1alpha1.TelemetryPolicyStatus{}, err
		}
	}
	return &v1alpha1.TelemetryPolicyStatus{}, nil
}
