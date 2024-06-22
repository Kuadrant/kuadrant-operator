package controllers

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
)

func (r *RateLimitPolicyReconciler) reconcileStatus(ctx context.Context, topology *kuadrantgatewayapi.Topology) error {
	logger, err := logr.FromContext(ctx)
	if err != nil {
		return err
	}

	policies := topology.Policies()

	logger.V(1).Info("reconcile status", "#rlp", len(policies))

	for _, policy := range policies {
		err := r.reconcileSinglePolicyStatus(ctx, policy, topology)
		if err != nil {
			return err
		}
	}

	return nil
}

func (r *RateLimitPolicyReconciler) reconcileSinglePolicyStatus(ctx context.Context, policy kuadrantgatewayapi.PolicyNode, topology *kuadrantgatewayapi.Topology) error {
	logger, _ := logr.FromContext(ctx)
	newStatus := r.calculateStatus(ctx, policy, topology)

	equalStatus := rlp.Status.Equals(newStatus, logger)
	logger.V(1).Info("Status", "status is different", !equalStatus)
	logger.V(1).Info("Status", "generation is different", rlp.Generation != rlp.Status.ObservedGeneration)
	if equalStatus && rlp.Generation == rlp.Status.ObservedGeneration {
		// Steady state, early return 🎉
		logger.V(1).Info("Status was not updated")
		return nil
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
		return fmt.Errorf("failed to update status: %w", updateErr)
	}

	return nil
}

func (r *RateLimitPolicyReconciler) calculateStatus(ctx context.Context, policy kuadrantgatewayapi.PolicyNode, topology *kuadrantgatewayapi.Topology) *kuadrantv1beta2.RateLimitPolicyStatus {
	newStatus := &kuadrantv1beta2.RateLimitPolicyStatus{
		// Copy initial conditions. Otherwise, status will always be updated
		Conditions:         slices.Clone(rlp.Status.Conditions),
		ObservedGeneration: rlp.Status.ObservedGeneration,
	}

	acceptedCond := r.acceptedCondition(ctx, rlp, topology)

	meta.SetStatusCondition(&newStatus.Conditions, *acceptedCond)

	// Do not set enforced condition if Accepted condition is false
	if meta.IsStatusConditionFalse(newStatus.Conditions, string(gatewayapiv1alpha2.PolicyReasonAccepted)) {
		meta.RemoveStatusCondition(&newStatus.Conditions, string(kuadrant.PolicyConditionEnforced))
	}

	return newStatus
}

func (r *RateLimitPolicyReconciler) acceptedCondition(ctx context.Context, policy kuadrantgatewayapi.PolicyNode, topology *kuadrantgatewayapi.Topology) *metav1.Condition {
	validations := []func(ctx context.Context, rlp *kuadrantv1beta2.RateLimitPolicy, topology *kuadrantgatewayapi.Topology) error{
		r.validatePolicy,
		r.checkTargetReference,
		r.validatePolicyHostnames,
		r.checkDirectReferences,
	}

	cond := &metav1.Condition{
		Type:    string(gatewayapiv1alpha2.PolicyConditionAccepted),
		Status:  metav1.ConditionTrue,
		Reason:  string(gatewayapiv1alpha2.PolicyReasonAccepted),
		Message: fmt.Sprintf("%s has been accepted", rlp.Kind()),
	}

	for _, validation := range validations {
		err := validation(ctx, rlp, topology)
		if err != nil {
			// Wrap error into a PolicyError if it is not this type
			var policyErr kuadrant.PolicyError
			if !errors.As(err, &policyErr) {
				policyErr = kuadrant.NewErrUnknown(rlp.Kind(), err)
			}

			cond.Status = metav1.ConditionFalse
			cond.Message = policyErr.Error()
			cond.Reason = string(policyErr.Reason())

			return cond
		}
	}

	return cond
}

func (r *RateLimitPolicyReconciler) validatePolicy(_ context.Context, rlp *kuadrantv1beta2.RateLimitPolicy, _ *kuadrantgatewayapi.Topology) error {
	if err := rlp.Validate(); err != nil {
		return kuadrant.NewErrInvalid(rlp.Kind(), err)
	}

	return nil
}

func (r *RateLimitPolicyReconciler) checkTargetReference(ctx context.Context, rlp *kuadrantv1beta2.RateLimitPolicy, topology *kuadrantgatewayapi.Topology) error {
	// TODO
	return nil
}

func (r *RateLimitPolicyReconciler) validatePolicyHostnames(ctx context.Context, rlp *kuadrantv1beta2.RateLimitPolicy, topology *kuadrantgatewayapi.Topology) error {
	// TODO
	return nil
}

func (r *RateLimitPolicyReconciler) checkDirectReferences(ctx context.Context, rlp *kuadrantv1beta2.RateLimitPolicy, topology *kuadrantgatewayapi.Topology) error {
	// TODO
	return nil
}
