package controllers

import (
	"context"
	"sync"

	"github.com/go-logr/logr"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"github.com/samber/lo"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	kuadrantv1alpha1 "github.com/kuadrant/kuadrant-operator/api/v1alpha1"
	kuadrantv1beta3 "github.com/kuadrant/kuadrant-operator/api/v1beta3"
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

func (r *GatewayPolicyDiscoverabilityReconciler) reconcile(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, s *sync.Map) error {
	logger := controller.LoggerFromContext(ctx).WithName("GatewayPolicyDiscoverabilityReconciler").WithName("reconcile")

	// Get all the gateways
	gateways := lo.FilterMap(topology.Targetables().Items(), func(item machinery.Targetable, index int) (*machinery.Gateway, bool) {
		g, ok := item.(*machinery.Gateway)
		return g, ok
	})

	// Policy group kinds to reconcile over
	policyGroupKinds := []*schema.GroupKind{
		&kuadrantv1beta3.AuthPolicyGroupKind,
		&kuadrantv1beta3.RateLimitPolicyGroupKind,
		&kuadrantv1alpha1.TLSPolicyGroupKind,
		&kuadrantv1alpha1.DNSPolicyGroupKind,
	}

	// For each gateway
	for _, gw := range gateways {
		gwStatus := gw.Status.DeepCopy()

		// For each listener in gateway, set/remove the affected by condition in the Listener Status
		listeners := lo.Map(topology.Targetables().Children(gw), func(item machinery.Targetable, index int) *machinery.Listener {
			l, _ := item.(*machinery.Listener)
			return l
		})

		for _, listener := range listeners {
			listenerStatus, index, updated := r.buildExpectedListenerStatus(ctx, s, gw, listener, logger, policyGroupKinds)
			if !updated {
				continue
			}

			gwStatus.Listeners[index] = listenerStatus
		}

		// Set the affected by condition in Gateway conditions
		for _, policyKind := range policyGroupKinds {
			// Only want gw policies
			path := []machinery.Targetable{gw}
			// Filter for policies of kind
			policies := kuadrantv1.PoliciesInPath(path, func(policy machinery.Policy) bool {
				// TODO: Filter by enforced policies?
				return policy.GroupVersionKind().GroupKind() == *policyKind && IsPolicyAccepted(ctx, policy, s)
			})

			// No policies of kind attached - remove condition
			if len(policies) == 0 {
				conditionType := PolicyAffectedConditionType(policyKind.Kind)
				c := meta.FindStatusCondition(gwStatus.Conditions, conditionType)
				if c == nil {
					logger.V(1).Info("condition already absent, skipping", "condition", conditionType, "gateway", gw.GetName())
					continue
				}
				meta.RemoveStatusCondition(&gwStatus.Conditions, conditionType)
				logger.V(1).Info("removing condition", "condition", conditionType, "gateway", gw.GetName())
				continue
			}

			// Has policies of kind attached
			condition := PolicyAffectedCondition(policyKind.Kind, policies)

			if c := meta.FindStatusCondition(gwStatus.Conditions, condition.Type); c != nil && c.Status == condition.Status &&
				c.Reason == condition.Reason && c.Message == condition.Message && c.ObservedGeneration == gw.GetGeneration() {
				logger.V(1).Info("condition already up-to-date, skipping", "condition", condition.Type, "status", condition.Status, "gateway", gw.GetName())
			} else {
				condition.ObservedGeneration = gw.GetGeneration()
				meta.SetStatusCondition(&gwStatus.Conditions, condition)
				logger.V(1).Info("adding condition", "condition", condition.Type, "status", condition.Status, "gateway", gw.GetName())
			}
		}

		// Update GW Status
		equalStatus := equality.Semantic.DeepEqual(gwStatus, gw.Status)
		if equalStatus {
			logger.V(1).Info("gw status unchanged, skipping update")
			continue
		}

		gw.Status = *gwStatus
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

func (r *GatewayPolicyDiscoverabilityReconciler) buildExpectedListenerStatus(ctx context.Context, s *sync.Map, gw *machinery.Gateway, listener *machinery.Listener, logger logr.Logger, groupKinds []*schema.GroupKind) (gatewayapiv1.ListenerStatus, int, bool) {
	listenerStatus, index, found := lo.FindIndexOf(gw.Status.Listeners, func(item gatewayapiv1.ListenerStatus) bool {
		return item.Name == listener.Name
	})

	if !found {
		logger.V(1).Info("unable to find listener status", "listener", listener.GetName())
		return gatewayapiv1.ListenerStatus{}, index, false
	}

	updated := false

	// Want policy of both gateway and listener
	policies := kuadrantv1.PoliciesInPath([]machinery.Targetable{gw, listener}, func(policy machinery.Policy) bool {
		return true
	})

	for _, groupKind := range groupKinds {
		// Filter for policies of kind
		policiesOfKind := lo.Filter(policies, func(item machinery.Policy, index int) bool {
			// TODO: Filter by enforced policies?
			return item.GroupVersionKind().GroupKind() == *groupKind && IsPolicyAccepted(ctx, item, s)
		})

		if len(policiesOfKind) == 0 {
			conditionType := PolicyAffectedConditionType(groupKind.Kind)
			c := meta.FindStatusCondition(listenerStatus.Conditions, conditionType)
			if c == nil {
				logger.V(1).Info("listener condition already absent, skipping", "condition", conditionType, "listener", listener.GetName())
				continue
			}
			meta.RemoveStatusCondition(&listenerStatus.Conditions, conditionType)
			logger.V(1).Info("removing condition", "condition", conditionType, "listener", listener.GetName())
			updated = true
			continue
		}

		// Has policies of kind attached
		condition := PolicyAffectedCondition(groupKind.Kind, policiesOfKind)
		if c := meta.FindStatusCondition(listenerStatus.Conditions, condition.Type); c != nil && c.Status == condition.Status &&
			c.Reason == condition.Reason && c.Message == condition.Message && c.ObservedGeneration == gw.GetGeneration() {
			logger.V(1).Info("condition already up-to-date, skipping", "condition", condition.Type, "status", condition.Status, "listener", listener.GetName())
			continue
		}

		condition.ObservedGeneration = gw.GetGeneration()
		meta.SetStatusCondition(&listenerStatus.Conditions, condition)
		logger.V(1).Info("adding condition", "condition", condition.Type, "status", condition.Status, "listener", listener.GetName())
		updated = true
	}

	return listenerStatus, index, updated
}
