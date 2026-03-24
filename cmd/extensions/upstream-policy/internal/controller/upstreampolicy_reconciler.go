package controller

import (
	"context"
	"fmt"
	"slices"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/kuadrant/kuadrant-operator/cmd/extensions/upstream-policy/api/v1alpha1"
	extcontroller "github.com/kuadrant/kuadrant-operator/pkg/extension/controller"
	"github.com/kuadrant/kuadrant-operator/pkg/extension/types"
)

// +kubebuilder:rbac:groups=extensions.kuadrant.io,resources=upstreampolicies,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=extensions.kuadrant.io,resources=upstreampolicies/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=extensions.kuadrant.io,resources=upstreampolicies/finalizers,verbs=update

type UpstreamPolicyReconciler struct {
	types.ExtensionBase
}

func NewUpstreamPolicyReconciler() *UpstreamPolicyReconciler {
	return &UpstreamPolicyReconciler{}
}

func (r *UpstreamPolicyReconciler) Reconcile(ctx context.Context, request reconcile.Request, kuadrantCtx types.KuadrantCtx) (reconcile.Result, error) {
	if err := r.Configure(ctx); err != nil {
		return reconcile.Result{}, fmt.Errorf("failed to configure extension: %w", err)
	}
	r.Logger.Info("reconciling upstreampolicy started")
	defer r.Logger.Info("reconciling upstreampolicy completed")

	upstreamPolicy := &v1alpha1.UpstreamPolicy{}
	if err := r.Client.Get(ctx, request.NamespacedName, upstreamPolicy); err != nil {
		if errors.IsNotFound(err) {
			r.Logger.Error(err, "upstreampolicy not found")
			return reconcile.Result{}, nil
		}
		r.Logger.Error(err, "failed to retrieve upstreampolicy")
		return reconcile.Result{}, err
	}

	if upstreamPolicy.GetDeletionTimestamp() != nil {
		r.Logger.Info("upstreampolicy marked for deletion")
		return reconcile.Result{}, nil
	}

	policyStatus, specErr := r.reconcileSpec(ctx, upstreamPolicy, kuadrantCtx)
	statusResult, statusErr := r.reconcileStatus(ctx, upstreamPolicy, policyStatus)

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

func (r *UpstreamPolicyReconciler) reconcileSpec(ctx context.Context, pol *v1alpha1.UpstreamPolicy, kuadrantCtx types.KuadrantCtx) (*v1alpha1.UpstreamPolicyStatus, error) {
	r.Logger.Info("registering upstream", "url", pol.Spec.URL)

	if err := kuadrantCtx.RegisterUpstreamMethod(ctx, pol, types.UpstreamConfig{
		URL: pol.Spec.URL,
	}); err != nil {
		r.Logger.Error(err, "failed to register upstream")
		return calculateErrorStatus(pol, err), err
	}

	r.Logger.Info("upstream registered successfully", "url", pol.Spec.URL)
	return calculateEnforcedStatus(pol, nil), nil
}

func (r *UpstreamPolicyReconciler) reconcileStatus(ctx context.Context, pol *v1alpha1.UpstreamPolicy, newStatus *v1alpha1.UpstreamPolicyStatus) (ctrl.Result, error) {
	equalStatus := pol.Status.Equals(newStatus, r.Logger)
	r.Logger.Info("Status", "status is different", !equalStatus)
	r.Logger.Info("Status", "generation is different", pol.Generation != pol.Status.ObservedGeneration)
	if equalStatus && pol.Generation == pol.Status.ObservedGeneration {
		r.Logger.Info("Status was not updated")
		return reconcile.Result{}, nil
	}

	r.Logger.V(1).Info("Updating Status", "sequence no:", fmt.Sprintf("sequence No: %v->%v", pol.Status.ObservedGeneration, newStatus.ObservedGeneration))

	pol.Status = *newStatus
	updateErr := r.Client.Status().Update(ctx, pol)
	if updateErr != nil {
		if errors.IsConflict(updateErr) {
			r.Logger.Info("Failed to update status: resource might just be outdated")
			return reconcile.Result{Requeue: true}, nil
		}
		return reconcile.Result{}, fmt.Errorf("failed to update status: %w", updateErr)
	}
	return ctrl.Result{}, nil
}

func calculateErrorStatus(pol *v1alpha1.UpstreamPolicy, specErr error) *v1alpha1.UpstreamPolicyStatus {
	newStatus := &v1alpha1.UpstreamPolicyStatus{
		ObservedGeneration: pol.Generation,
		Conditions:         slices.Clone(pol.Status.Conditions),
	}
	meta.SetStatusCondition(&newStatus.Conditions, *extcontroller.AcceptedCondition(pol, specErr))
	meta.RemoveStatusCondition(&newStatus.Conditions, string(types.PolicyConditionEnforced))
	return newStatus
}

func calculateEnforcedStatus(pol *v1alpha1.UpstreamPolicy, enforcedErr error) *v1alpha1.UpstreamPolicyStatus {
	newStatus := &v1alpha1.UpstreamPolicyStatus{
		ObservedGeneration: pol.Generation,
		Conditions:         slices.Clone(pol.Status.Conditions),
	}
	meta.SetStatusCondition(&newStatus.Conditions, *extcontroller.AcceptedCondition(pol, nil))
	meta.SetStatusCondition(&newStatus.Conditions, *extcontroller.EnforcedCondition(pol, enforcedErr, true))
	return newStatus
}
