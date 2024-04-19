package controllers

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
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

func (r *RateLimitPolicyReconciler) calculateStatus(ctx context.Context, rlp *kuadrantv1beta2.RateLimitPolicy, specErr error) *kuadrantv1beta2.RateLimitPolicyStatus {
	newStatus := &kuadrantv1beta2.RateLimitPolicyStatus{
		// Copy initial conditions. Otherwise, status will always be updated
		Conditions:         slices.Clone(rlp.Status.Conditions),
		ObservedGeneration: rlp.Status.ObservedGeneration,
	}

	acceptedCond := kuadrant.AcceptedCondition(rlp, specErr)

	meta.SetStatusCondition(&newStatus.Conditions, *acceptedCond)

	// Do not set enforced condition if Accepted condition is false
	if meta.IsStatusConditionFalse(newStatus.Conditions, string(gatewayapiv1alpha2.PolicyReasonAccepted)) {
		return newStatus
	}

	enforcedCond := r.enforcedCondition(ctx, rlp)
	meta.SetStatusCondition(&newStatus.Conditions, *enforcedCond)

	return newStatus
}

func (r *RateLimitPolicyReconciler) enforcedCondition(ctx context.Context, policy *kuadrantv1beta2.RateLimitPolicy) *metav1.Condition {
	logger, _ := logr.FromContext(ctx)

	limitador, err := r.getLimitador(ctx, policy)
	if err != nil {
		logger.V(1).Error(err, "failed to get limitador")
		return kuadrant.EnforcedCondition(policy, kuadrant.NewErrUnknown(policy.Kind(), err), false)
	}
	if meta.IsStatusConditionFalse(limitador.Status.Conditions, "Ready") {
		logger.V(1).Info("Limitador is not ready")
		return kuadrant.EnforcedCondition(policy, kuadrant.NewErrUnknown(policy.Kind(), errors.New("limitador is not ready")), false)
	}

	if r.OverriddenPolicyMap.IsPolicyOverridden(policy) {
		if len(r.OverriddenPolicyMap.PolicyOverriddenBy(policy)) == 0 {
			return kuadrant.EnforcedCondition(policy, kuadrant.NewErrUnknown(policy.Kind(), errors.New("no free routes to enforce policy")), false) // Maybe this should be a standard condition rather than an unknown condition
		}
		return kuadrant.EnforcedCondition(policy, kuadrant.NewErrOverridden(policy.Kind(), r.OverriddenPolicyMap.PolicyOverriddenBy(policy)), false)
	}

	logger.V(1).Info("RateLimitPolicy is enforced")
	return kuadrant.EnforcedCondition(policy, nil, true)
}
