package controllers

import (
	"context"
	"fmt"
	"slices"
	"sync"

	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"github.com/samber/lo"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	kuadrantv1alpha1 "github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	kuadrantv1beta3 "github.com/kuadrant/kuadrant-operator/api/v1beta3"
	"github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
)

type GatewayPolicyDiscoverabilityReconciler struct {
	Client *dynamic.DynamicClient
}

func NewGatewayPolicyDiscoverabilityReconciler(client *dynamic.DynamicClient) *GatewayPolicyDiscoverabilityReconciler {
	return &GatewayPolicyDiscoverabilityReconciler{Client: client}
}

func (r *GatewayPolicyDiscoverabilityReconciler) Subscription() *controller.Subscription {
	return &controller.Subscription{
		Events: []controller.ResourceEventMatcher{
			{Kind: &machinery.GatewayGroupKind},
			{Kind: &kuadrantv1beta3.AuthPolicyGroupKind},
			{Kind: &kuadrantv1beta3.RateLimitPolicyGroupKind},
			{Kind: &kuadrantv1alpha1.TLSPolicyGroupKind},
			{Kind: &kuadrantv1alpha1.DNSPolicyGroupKind},
		},
		ReconcileFunc: r.reconcile,
	}
}

func (r *GatewayPolicyDiscoverabilityReconciler) reconcile(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, _ *sync.Map) error {
	logger := controller.LoggerFromContext(ctx).WithName("GatewayPolicyDiscoverabilityReconciler").WithName("reconcile")

	gateways := lo.FilterMap(topology.Targetables().Items(), func(item machinery.Targetable, index int) (*machinery.Gateway, bool) {
		g, ok := item.(*machinery.Gateway)
		return g, ok
	})

	policyKinds := []*schema.GroupKind{
		&kuadrantv1beta3.AuthPolicyGroupKind,
		&kuadrantv1beta3.RateLimitPolicyGroupKind,
		&kuadrantv1alpha1.TLSPolicyGroupKind,
		&kuadrantv1alpha1.DNSPolicyGroupKind,
	}

	for _, gw := range gateways {
		conditions := slices.Clone(gw.Status.Conditions)

		// TODO: What happens for multiple policies of the same kind -
		// TODO: Should it be conditions per listener?
		for _, policyKind := range policyKinds {
			// Policies targeting gateway and listeners
			path := []machinery.Targetable{gw}
			path = append(path, topology.Targetables().Children(gw)...)
			policies := kuadrantv1.PoliciesInPath(path, func(policy machinery.Policy) bool {
				// Filter for policies of kind
				return policy.GroupVersionKind().GroupKind() == *policyKind
			})

			// No policies of kind attached - remove condition
			if len(policies) == 0 {
				conditionType := PolicyAffectedConditionType(policyKind.Kind)
				c := meta.FindStatusCondition(conditions, conditionType)
				if c == nil {
					logger.V(1).Info("condition already absent, skipping", "condition", conditionType)
					continue
				}
				meta.RemoveStatusCondition(&conditions, conditionType)
				logger.V(1).Info("removing condition from gateway", "condition", conditionType)
				continue
			}

			// Has policies of kind attached
			for _, policy := range policies {
				condition := metav1.Condition{
					Type:    PolicyAffectedConditionType(policyKind.Kind),
					Status:  metav1.ConditionTrue,
					Reason:  string(gatewayapiv1alpha2.PolicyReasonAccepted),
					Message: fmt.Sprintf("Object affected by %s %s", policyKind, client.ObjectKey{Namespace: policy.GetNamespace(), Name: policy.GetName()}),
				}

				// TODO: Refine
				p, ok := policy.(gatewayapi.Policy)
				if !ok {
					logger.V(1).Info("skipping policy status", "policy", policyKind)
					continue
				}

				// Set condition if policy is not affected
				if c := meta.FindStatusCondition(p.GetStatus().GetConditions(), string(gatewayapiv1alpha2.PolicyConditionAccepted)); c == nil || c.Status != metav1.ConditionTrue { // should we aim for 'Enforced' instead?
					condition.Status = metav1.ConditionFalse
					condition.Message = fmt.Sprintf("Object unaffected by %s %s, policy is not accepted", policyKind, client.ObjectKey{Namespace: policy.GetNamespace(), Name: policy.GetName()})
					condition.Reason = PolicyReasonUnknown
					if c != nil {
						condition.Reason = c.Reason
					}
				}

				if c := meta.FindStatusCondition(conditions, condition.Type); c != nil && c.Status == condition.Status &&
					c.Reason == condition.Reason && c.Message == condition.Message && c.ObservedGeneration == gw.GetGeneration() {
					logger.V(1).Info("condition already up-to-date, skipping", "condition", condition.Type, "status", condition.Status)
				} else {
					gwCondition := condition.DeepCopy()
					gwCondition.ObservedGeneration = gw.GetGeneration()
					meta.SetStatusCondition(&conditions, *gwCondition)
					logger.V(1).Info("adding condition to gateway", "condition", condition.Type, "status", condition.Status)
				}
			}
		}

		// Update GW Status
		equalStatus := equality.Semantic.DeepEqual(conditions, gw.Status.Conditions)
		if equalStatus {
			logger.V(1).Info("gw conditions unchanged, skipping update")
			continue
		}

		gw.Status.Conditions = conditions
		obj, err := controller.Destruct(gw.Gateway)
		if err != nil {
			logger.Error(err, "unable to destruct gateway") // should never happen
			continue
		}

		// Update gw status once
		_, err = r.Client.Resource(controller.GatewaysResource).Namespace(gw.GetNamespace()).UpdateStatus(ctx, obj, metav1.UpdateOptions{})
		if err != nil {
			logger.Error(err, "unable to update gateway status")
		}
	}

	return nil
}
