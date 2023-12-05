package controllers

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/go-logr/logr"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
)

func (r *RateLimitPolicyReconciler) reconcileStatus(ctx context.Context, rlp *kuadrantv1beta2.RateLimitPolicy, specErr error) (ctrl.Result, error) {
	logger, _ := logr.FromContext(ctx)
	newStatus := r.calculateStatus(ctx, rlp, specErr)

	equalStatus := rlp.Status.Equals(newStatus, logger)
	logger.V(1).Info("Status", "status is different", !equalStatus)
	logger.V(1).Info("Status", "generation is different", rlp.Generation != rlp.Status.ObservedGeneration)
	if equalStatus && rlp.Generation == rlp.Status.ObservedGeneration {
		// Steady state
		logger.V(1).Info("Status was not updated")
		return reconcile.Result{}, nil
	}

	// Save the generation number we acted on, otherwise we might wrongfully indicate
	// that we've seen a spec update when we retry.
	// TODO: This can clobber an update if we allow multiple agents to write to the
	// same status.
	newStatus.ObservedGeneration = rlp.Generation

	logger.V(1).Info("Updating Status", "sequence no:", fmt.Sprintf("sequence No: %v->%v", rlp.Status.ObservedGeneration, newStatus.ObservedGeneration))

	rlp.Status = *newStatus
	updateErr := r.Client().Status().Update(ctx, rlp)
	logger.V(1).Info("Updating Status", "err", updateErr)
	if updateErr != nil {
		// Ignore conflicts, resource might just be outdated.
		if apierrors.IsConflict(updateErr) {
			logger.Info("Failed to update status: resource might just be outdated")
			return reconcile.Result{Requeue: true}, nil
		}

		return reconcile.Result{}, fmt.Errorf("failed to update status: %w", updateErr)
	}
	return ctrl.Result{}, nil
}

func (r *RateLimitPolicyReconciler) calculateStatus(_ context.Context, rlp *kuadrantv1beta2.RateLimitPolicy, specErr error) *kuadrantv1beta2.RateLimitPolicyStatus {
	newStatus := &kuadrantv1beta2.RateLimitPolicyStatus{
		// Copy initial conditions. Otherwise, status will always be updated
		Conditions:         slices.Clone(rlp.Status.Conditions),
		ObservedGeneration: rlp.Status.ObservedGeneration,
	}

	acceptedCond := r.acceptedCondition(specErr)

	meta.SetStatusCondition(&newStatus.Conditions, *acceptedCond)

	return newStatus
}

// TODO - Accepted Condition
func (r *RateLimitPolicyReconciler) acceptedCondition(specErr error) *metav1.Condition {
	// Accepted
	cond := &metav1.Condition{
		Type:    string(gatewayapiv1alpha2.PolicyConditionAccepted),
		Status:  metav1.ConditionTrue,
		Reason:  string(gatewayapiv1alpha2.PolicyReasonAccepted),
		Message: "RateLimitPolicy has been accepted",
	}

	if specErr != nil {
		cond.Status = metav1.ConditionFalse
		cond.Message = specErr.Error()

		// TargetNotFound
		switch {
		case errors.As(specErr, &common.ErrTargetNotFound{}):
			cond.Reason = string(gatewayapiv1alpha2.PolicyReasonTargetNotFound)
		default:
			cond.Reason = "ReconciliationError"
		}
	}

	// TODO - Invalid Reason
	// TODO - Conflicted Reason

	return cond
}
