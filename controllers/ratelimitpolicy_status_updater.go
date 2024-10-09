package controllers

import (
	"context"
	"slices"
	"sync"

	"github.com/samber/lo"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/utils/ptr"

	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	kuadrantv1beta3 "github.com/kuadrant/kuadrant-operator/api/v1beta3"
	kuadrantenvoygateway "github.com/kuadrant/kuadrant-operator/pkg/envoygateway"
	kuadrantistio "github.com/kuadrant/kuadrant-operator/pkg/istio"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
)

type rateLimitPolicyStatusUpdater struct {
	client *dynamic.DynamicClient
}

func (r *rateLimitPolicyStatusUpdater) Subscription() controller.Subscription {
	return controller.Subscription{
		ReconcileFunc: r.UpdateStatus,
		Events: []controller.ResourceEventMatcher{
			{Kind: &kuadrantv1beta1.KuadrantGroupKind},
			{Kind: &machinery.GatewayClassGroupKind},
			{Kind: &machinery.GatewayGroupKind},
			{Kind: &machinery.HTTPRouteGroupKind},
			{Kind: &kuadrantv1beta3.RateLimitPolicyGroupKind, EventType: ptr.To(controller.CreateEvent)},
			{Kind: &kuadrantv1beta3.RateLimitPolicyGroupKind, EventType: ptr.To(controller.UpdateEvent)},
			{Kind: &kuadrantv1beta1.LimitadorGroupKind},
			{Kind: &kuadrantistio.EnvoyFilterGroupKind},
			{Kind: &kuadrantistio.WasmPluginGroupKind},
			{Kind: &kuadrantenvoygateway.EnvoyPatchPolicyGroupKind},
			{Kind: &kuadrantenvoygateway.EnvoyExtensionPolicyGroupKind},
		},
	}
}

func (r *rateLimitPolicyStatusUpdater) UpdateStatus(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, state *sync.Map) error {
	logger := controller.LoggerFromContext(ctx).WithName("rateLimitPolicyStatusUpdater")

	policies := lo.FilterMap(topology.Policies().Items(), func(item machinery.Policy, index int) (*kuadrantv1beta3.RateLimitPolicy, bool) {
		p, ok := item.(*kuadrantv1beta3.RateLimitPolicy)
		return p, ok
	})

	policyAcceptedFunc := rateLimitPolicyAcceptedStatusFunc(state)

	logger.V(1).Info("updating rate limit policy statuses", "policies", len(policies))

	for _, policy := range policies {
		if policy.GetDeletionTimestamp() != nil {
			logger.V(1).Info("ratelimitpolicy is marked for deletion, skipping", "name", policy.Name, "namespace", policy.Namespace)
			continue
		}

		// copy initial conditions, otherwise status will always be updated
		newStatus := &kuadrantv1beta3.RateLimitPolicyStatus{
			Conditions:         slices.Clone(policy.Status.Conditions),
			ObservedGeneration: policy.Status.ObservedGeneration,
		}

		accepted, err := policyAcceptedFunc(policy)
		meta.SetStatusCondition(&newStatus.Conditions, *kuadrant.AcceptedCondition(policy, err))

		// do not set enforced condition if Accepted condition is false
		if !accepted {
			meta.RemoveStatusCondition(&newStatus.Conditions, string(kuadrant.PolicyConditionEnforced))
		} else {
			enforcedCond := r.enforcedCondition(policy, topology)
			meta.SetStatusCondition(&newStatus.Conditions, *enforcedCond)
		}

		equalStatus := equality.Semantic.DeepEqual(newStatus, policy.Status)
		if equalStatus && policy.Generation == policy.Status.ObservedGeneration {
			logger.V(1).Info("policy status unchanged, skipping update")
			continue
		}
		newStatus.ObservedGeneration = policy.Generation
		policy.Status = *newStatus

		obj, err := controller.Destruct(policy)
		if err != nil {
			logger.Error(err, "unable to destruct policy") // should never happen
			continue
		}

		_, err = r.client.Resource(kuadrantv1beta3.RateLimitPoliciesResource).Namespace(policy.GetNamespace()).UpdateStatus(ctx, obj, metav1.UpdateOptions{})
		if err != nil {
			logger.Error(err, "unable to update status for ratelimitpolicy", "name", policy.GetName(), "namespace", policy.GetNamespace())
		}
	}

	logger.V(1).Info("finished updating rate limit policy statuses")

	return nil
}

func (r *rateLimitPolicyStatusUpdater) enforcedCondition(policy *kuadrantv1beta3.RateLimitPolicy, _ *machinery.Topology) *metav1.Condition {
	var err error
	if err != nil { // TODO: check if any condition for the policy to be enforced fails
		return kuadrant.EnforcedCondition(policy, kuadrant.NewErrUnknown(kuadrantv1beta3.RateLimitPolicyGroupKind.Kind, err), false)
	}

	// TODO: check the state of the Limitador CR
	// TODO: check the state of the EnvoyFilter and EnvoyPatchPolicy resources for each gateway
	// TODO: check the state of the WasmPlugin and EnvoyExtensionPolicy resources for each gateway

	return kuadrant.EnforcedCondition(policy, nil, true)
}
