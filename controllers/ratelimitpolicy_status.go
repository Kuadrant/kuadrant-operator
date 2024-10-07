package controllers

import (
	"context"
	"slices"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1beta3 "github.com/kuadrant/kuadrant-operator/api/v1beta3"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
)

func (r *RateLimitPolicyReconciler) reconcileStatus(ctx context.Context, rlp *kuadrantv1beta3.RateLimitPolicy, specErr error) (ctrl.Result, error) {
	logger, _ := logr.FromContext(ctx)
	newStatus := r.calculateStatus(rlp, specErr)
	if err := r.ReconcileResourceStatus(
		ctx,
		client.ObjectKeyFromObject(rlp),
		&kuadrantv1beta3.RateLimitPolicy{},
		kuadrantv1beta3.RateLimitPolicyStatusMutator(newStatus, logger),
	); err != nil {
		// Ignore conflicts, resource might just be outdated.
		if apierrors.IsConflict(err) {
			logger.V(1).Info("Failed to update status: resource might just be outdated")
			return reconcile.Result{Requeue: true}, nil
		}

		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func (r *RateLimitPolicyReconciler) calculateStatus(rlp *kuadrantv1beta3.RateLimitPolicy, specErr error) *kuadrantv1beta3.RateLimitPolicyStatus {
	newStatus := &kuadrantv1beta3.RateLimitPolicyStatus{
		// Copy initial conditions. Otherwise, status will always be updated
		Conditions: slices.Clone(rlp.Status.Conditions),
	}

	newStatus.SetObservedGeneration(rlp.GetGeneration())

	acceptedCond := kuadrant.AcceptedCondition(rlp, specErr)

	meta.SetStatusCondition(&newStatus.Conditions, *acceptedCond)

	// Do not set enforced condition if Accepted condition is false
	if meta.IsStatusConditionFalse(newStatus.Conditions, string(gatewayapiv1alpha2.PolicyReasonAccepted)) {
		meta.RemoveStatusCondition(&newStatus.Conditions, string(kuadrant.PolicyConditionEnforced))
		return newStatus
	}

	return newStatus
}
