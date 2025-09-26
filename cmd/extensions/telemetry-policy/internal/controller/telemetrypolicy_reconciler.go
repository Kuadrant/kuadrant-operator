package controller

import (
	"context"
	"fmt"
	"slices"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/kuadrant/kuadrant-operator/cmd/extensions/telemetry-policy/api/v1alpha1"
	extcontroller "github.com/kuadrant/kuadrant-operator/pkg/extension/controller"
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

	telemetryPolicyStatus, specErr := r.reconcileSpec(ctx, telemetryPolicy, kuadrantCtx)
	statusResult, statusErr := r.reconcileStatus(ctx, telemetryPolicy, telemetryPolicyStatus)

	if specErr != nil {
		return reconcile.Result{}, specErr
	}
	if statusErr != nil {
		return reconcile.Result{}, statusErr
	}

	if statusResult.RequeueAfter > 0 {
		r.Logger.Info("Reconciling status not finished. Requeueing.")
		return statusResult, nil
	}

	return reconcile.Result{}, nil
}

func (r *TelemetryPolicyReconciler) reconcileSpec(ctx context.Context, pol *v1alpha1.TelemetryPolicy, kuadrantCtx types.KuadrantCtx) (*v1alpha1.TelemetryPolicyStatus, error) {
	for binding, expression := range pol.Spec.Metrics.Default.Labels {
		if err := kuadrantCtx.AddDataTo(ctx, pol, types.DomainRequest, types.KuadrantMetricBinding(binding), expression); err != nil {
			r.Logger.Error(err, "failed to add data to request domain")
			return calculateErrorStatus(pol, err), err
		}
	}
	return calculateEnforcedStatus(pol, nil),
		nil
}

func (r *TelemetryPolicyReconciler) reconcileStatus(ctx context.Context, pol *v1alpha1.TelemetryPolicy, newStatus *v1alpha1.TelemetryPolicyStatus) (ctrl.Result, error) {
	equalStatus := pol.Status.Equals(newStatus, r.Logger)
	r.Logger.Info("Status", "status is different", !equalStatus)
	r.Logger.Info("Status", "generation is different", pol.Generation != pol.Status.ObservedGeneration)
	if equalStatus && pol.Generation == pol.Status.ObservedGeneration {
		// Steady state
		r.Logger.Info("Status was not updated")
		return reconcile.Result{}, nil
	}

	r.Logger.V(1).Info("Updating Status", "sequence no:", fmt.Sprintf("sequence No: %v->%v", pol.Status.ObservedGeneration, newStatus.ObservedGeneration))

	pol.Status = *newStatus
	updateErr := r.Client.Status().Update(ctx, pol)
	if updateErr != nil {
		// Ignore conflicts, resource might just be outdated.
		if errors.IsConflict(updateErr) {
			r.Logger.Info("Failed to update status: resource might just be outdated")
			return reconcile.Result{Requeue: true}, nil
		}

		return reconcile.Result{}, fmt.Errorf("failed to update status: %w", updateErr)
	}
	return ctrl.Result{}, nil
}

// TODO: We could make these kind of func part of the SDK, we'd need to create a `PolicyStatus` interface and common struct for its fields
func calculateErrorStatus(pol *v1alpha1.TelemetryPolicy, specErr error) *v1alpha1.TelemetryPolicyStatus {
	newStatus := &v1alpha1.TelemetryPolicyStatus{
		ObservedGeneration: pol.Generation,
		// Copy initial conditions. Otherwise, status will always be updated
		Conditions: slices.Clone(pol.Status.Conditions),
	}
	meta.SetStatusCondition(&newStatus.Conditions, *extcontroller.AcceptedCondition(pol, specErr))
	meta.RemoveStatusCondition(&newStatus.Conditions, string(types.PolicyConditionEnforced))
	return newStatus
}

func calculateEnforcedStatus(pol *v1alpha1.TelemetryPolicy, enforcedErr error) *v1alpha1.TelemetryPolicyStatus {
	newStatus := &v1alpha1.TelemetryPolicyStatus{
		ObservedGeneration: pol.Generation,
		// Copy initial conditions. Otherwise, status will always be updated
		Conditions: slices.Clone(pol.Status.Conditions),
	}

	meta.SetStatusCondition(&newStatus.Conditions, *extcontroller.AcceptedCondition(pol, nil))
	meta.SetStatusCondition(&newStatus.Conditions, *extcontroller.EnforcedCondition(pol, enforcedErr, true))
	return newStatus
}
