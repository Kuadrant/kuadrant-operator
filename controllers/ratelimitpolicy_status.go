package controllers

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
	"github.com/kuadrant/kuadrant-operator/pkg/rlptools"
)

func (r *RateLimitPolicyReconciler) reconcileStatus(ctx context.Context) error {
	logger, err := logr.FromContext(ctx)
	if err != nil {
		return err
	}

	topology, err := rlptools.Topology(ctx, r.Client())
	if err != nil {
		return err
	}

	policies := topology.Policies()

	logger.V(1).Info("reconcile status", "#rlp", len(policies))

	for _, policy := range policies {
		rlp, ok := policy.Policy.(*kuadrantv1beta2.RateLimitPolicy)
		if !ok {
			logger.Info("reconcile status", "topology did not return expected type", client.ObjectKeyFromObject(policy))
		}

		newStatus := r.calculateStatus(ctx, rlp, topology)
		err := r.ReconcileResourceStatus(ctx, client.ObjectKeyFromObject(policy), &kuadrantv1beta2.RateLimitPolicy{}, newStatus)
		if err != nil {
			return err
		}
	}

	return nil
}

func (r *RateLimitPolicyReconciler) calculateStatus(ctx context.Context, rlp *kuadrantv1beta2.RateLimitPolicy, topology *kuadrantgatewayapi.Topology) *kuadrantv1beta2.RateLimitPolicyStatus {
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

func (r *RateLimitPolicyReconciler) acceptedCondition(ctx context.Context, rlp *kuadrantv1beta2.RateLimitPolicy, topology *kuadrantgatewayapi.Topology) *metav1.Condition {
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
	policyNode, ok := topology.GetPolicy(rlp)
	if !ok {
		return fmt.Errorf("internal error. rlp %s not found in gateway api topology", client.ObjectKeyFromObject(rlp))
	}

	targetRef := policyNode.TargetRef()
	if targetRef == nil {
		return kuadrant.NewErrTargetNotFound(
			rlp.Kind(),
			rlp.GetTargetRef(),
			errors.New("not found in gateway api topology"),
		)
	}

	return nil
}

func (r *RateLimitPolicyReconciler) validatePolicyHostnames(ctx context.Context, rlp *kuadrantv1beta2.RateLimitPolicy, topology *kuadrantgatewayapi.Topology) error {
	policyNode, ok := topology.GetPolicy(rlp)
	if !ok {
		return fmt.Errorf("internal error. rlp %s not found in gateway api topology", client.ObjectKeyFromObject(rlp))
	}

	var targetNetworkObject client.Object

	if gNode := policyNode.TargetRef().GetGatewayNode(); gNode != nil {
		targetNetworkObject = gNode.Gateway
	} else if rNode := policyNode.TargetRef().GetRouteNode(); rNode != nil {
		targetNetworkObject = rNode.HTTPRoute
	}

	if err := kuadrant.ValidateHierarchicalRules(rlp, targetNetworkObject); err != nil {
		return kuadrant.NewErrInvalid(rlp.Kind(), err)
	}

	return nil
}

func (r *RateLimitPolicyReconciler) checkDirectReferences(ctx context.Context, rlp *kuadrantv1beta2.RateLimitPolicy, topology *kuadrantgatewayapi.Topology) error {
	policyNode, ok := topology.GetPolicy(rlp)
	if !ok {
		return fmt.Errorf("internal error. rlp %s not found in gateway api topology", client.ObjectKeyFromObject(rlp))
	}

	var targetNetworkObject client.Object

	if gNode := policyNode.TargetRef().GetGatewayNode(); gNode != nil {
		targetNetworkObject = gNode.Gateway
	} else if rNode := policyNode.TargetRef().GetRouteNode(); rNode != nil {
		targetNetworkObject = rNode.HTTPRoute
	}

	targetNetworkObjectAnnotations := utils.ReadAnnotationsFromObject(targetNetworkObject)
	targetNetworkObjectKey := client.ObjectKeyFromObject(targetNetworkObject)
	targetNetworkObjectKind := targetNetworkObject.GetObjectKind().GroupVersionKind()

	directAnnotationValue, ok := targetNetworkObjectAnnotations[rlp.DirectReferenceAnnotationName()]
	if ok && directAnnotationValue != client.ObjectKeyFromObject(rlp).String() {
		return kuadrant.NewErrConflict(
			rlp.Kind(),
			directAnnotationValue,
			fmt.Errorf("the %s target %s is already referenced by policy %s",
				targetNetworkObjectKind, targetNetworkObjectKey, directAnnotationValue),
		)
	}

	return nil
}
