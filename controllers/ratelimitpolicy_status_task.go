package controllers

import (
	"context"
	"errors"
	"fmt"
	"slices"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrlruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	"github.com/go-logr/logr"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"

	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
	"github.com/kuadrant/kuadrant-operator/pkg/library/reconcilers"
	"github.com/kuadrant/kuadrant-operator/pkg/log"
)

type RateLimitPolicyStatusTask struct {
	*reconcilers.BaseReconciler
}

func NewRateLimitPolicyStatusTask(mgr ctrlruntime.Manager) *RateLimitPolicyStatusTask {
	return &RateLimitPolicyStatusTask{
		BaseReconciler: reconcilers.NewBaseReconciler(
			mgr.GetClient(), mgr.GetScheme(), mgr.GetAPIReader(),
			log.Log.WithName("ratelimitpolicy_status"),
			mgr.GetEventRecorderFor("RateLimitPolicyStatus"),
		),
	}
}

func (r *RateLimitPolicyStatusTask) Events() []controller.ResourceEventMatcher {
	return []controller.ResourceEventMatcher{
		{Kind: ptr.To(kuadrantv1beta2.RateLimitPolicyGVK.GroupKind())},
		{Kind: ptr.To(machinery.HTTPRouteGroupKind)},
		{Kind: ptr.To(machinery.GatewayGroupKind)},
	}
}

func (r *RateLimitPolicyStatusTask) Run(eventCtx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error) {
	logger := r.Logger()
	logger.V(1).Info("RateLimitPolicyStatusTask run started")
	ctx := logr.NewContext(eventCtx, logger)

	allRatelimitPolicies := topology.Policies().Items(func(object machinery.Object) bool {
		return object.GroupVersionKind() == kuadrantv1beta2.RateLimitPolicyGVK
	})

	for _, policy := range allRatelimitPolicies {
		rlp, ok := policy.(*kuadrantv1beta2.RateLimitPolicy)
		if !ok {
			panic(fmt.Errorf("%T is not a *kuadrantv1beta2.RateLimitPolicy", policy))
		}

		rlpKey := client.ObjectKeyFromObject(rlp)

		if rlp.GetDeletionTimestamp() != nil {
			logger.V(1).Info("skipping policy marked for deletion", "key", rlpKey)
			continue
		}

		newStatus := r.computeNewStatus(ctx, rlp, topology)
		if err := r.ReconcileResourceStatus(
			ctx,
			rlpKey,
			&kuadrantv1beta2.RateLimitPolicy{},
			kuadrantv1beta2.RateLimitPolicyStatusMutator(newStatus, logger),
		); err != nil {
			// Ignore conflicts, resource might just be outdated.
			if apierrors.IsConflict(err) {
				logger.V(1).Info("Failed to update status: resource might just be outdated")
				return
			}

			logger.Error(err, "on rate limit policy status", "key", rlpKey)
			return
		}
	}

	logger.V(1).Info("RateLimitPolicyStatusTask run successfully")
	return
}

func (r *RateLimitPolicyStatusTask) computeNewStatus(ctx context.Context, rlp *kuadrantv1beta2.RateLimitPolicy, topology *machinery.Topology) *kuadrantv1beta2.RateLimitPolicyStatus {
	newStatus := &kuadrantv1beta2.RateLimitPolicyStatus{
		// Copy initial conditions. Otherwise, status will always be updated
		Conditions:         slices.Clone(rlp.Status.Conditions),
		ObservedGeneration: rlp.Generation,
	}

	acceptedCond := r.acceptedCondition(ctx, rlp, topology)

	meta.SetStatusCondition(&newStatus.Conditions, *acceptedCond)

	enforcedCond := r.enforcedCondition(ctx, rlp, topology)

	meta.SetStatusCondition(&newStatus.Conditions, *enforcedCond)

	// Do not set enforced condition if Accepted condition is false
	if meta.IsStatusConditionFalse(newStatus.Conditions, string(gatewayapiv1alpha2.PolicyReasonAccepted)) {
		meta.RemoveStatusCondition(&newStatus.Conditions, string(kuadrant.PolicyConditionEnforced))
	}

	return newStatus
}

func (r *RateLimitPolicyStatusTask) enforcedCondition(ctx context.Context, rlp *kuadrantv1beta2.RateLimitPolicy, topology *machinery.Topology) *metav1.Condition {
	// TODO
	return &metav1.Condition{
		Type:    string(gatewayapiv1alpha2.PolicyConditionAccepted),
		Status:  metav1.ConditionTrue,
		Reason:  string(gatewayapiv1alpha2.PolicyReasonAccepted),
		Message: fmt.Sprintf("%s has been accepted", rlp.Kind()),
	}
}

func (r *RateLimitPolicyStatusTask) acceptedCondition(ctx context.Context, rlp *kuadrantv1beta2.RateLimitPolicy, topology *machinery.Topology) *metav1.Condition {
	validations := []func(context.Context, *kuadrantv1beta2.RateLimitPolicy, *machinery.Topology) error{
		r.checkTargetReference,
		r.validatePolicyHostnames,
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

func (r *RateLimitPolicyStatusTask) checkTargetReference(_ context.Context, rlp *kuadrantv1beta2.RateLimitPolicy, topology *machinery.Topology) error {
	rlp.GetTargetRefs
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

func (r *RateLimitPolicyStatusTask) validatePolicyHostnames(_ context.Context, rlp *kuadrantv1beta2.RateLimitPolicy, _ *machinery.Topology) error {
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
