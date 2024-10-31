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

// Reconcile function to manage gateway and listener status based on policy conditions
func (r *GatewayPolicyDiscoverabilityReconciler) reconcile(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, syncMap *sync.Map) error {
	logger := controller.LoggerFromContext(ctx).WithName("GatewayPolicyDiscoverabilityReconciler").WithName("reconcile")

	// Extract gateways to process
	gateways := lo.FilterMap(topology.Targetables().Items(), func(item machinery.Targetable, index int) (*machinery.Gateway, bool) {
		gw, ok := item.(*machinery.Gateway)
		return gw, ok
	})

	// Policy kinds to evaluate
	policyGroupKinds := []*schema.GroupKind{
		&kuadrantv1beta3.AuthPolicyGroupKind,
		&kuadrantv1beta3.RateLimitPolicyGroupKind,
		&kuadrantv1alpha1.TLSPolicyGroupKind,
		&kuadrantv1alpha1.DNSPolicyGroupKind,
	}

	for _, gw := range gateways {
		updatedGwStatus := r.updateGatewayStatus(ctx, syncMap, gw, topology, logger, policyGroupKinds)
		if !equality.Semantic.DeepEqual(updatedGwStatus, gw.Status) {
			gw.Status = *updatedGwStatus
			if err := r.updateGateway(ctx, gw); err != nil {
				logger.Error(err, "failed to update gateway status", "gateway", gw.GetName())
			}
		}
	}

	return nil
}

// Updates the gateway status for a given policy group kind
func (r *GatewayPolicyDiscoverabilityReconciler) updateGatewayStatus(ctx context.Context, syncMap *sync.Map, gw *machinery.Gateway, topology *machinery.Topology, logger logr.Logger, policyGroupKinds []*schema.GroupKind) *gatewayapiv1.GatewayStatus {
	gwStatus := gw.Status.DeepCopy()

	// Update Listener Status for each listener in the gateway
	for _, listener := range r.extractListeners(topology, gw) {
		listenerStatus, index, updated := r.buildExpectedListenerStatus(ctx, syncMap, gw, listener, logger, policyGroupKinds)
		if updated {
			gwStatus.Listeners[index] = listenerStatus
		}
	}

	// Set conditions for each policy kind
	for _, policyKind := range policyGroupKinds {
		conditionType := PolicyAffectedConditionType(policyKind.Kind)
		policies := r.extractPolicies(ctx, syncMap, policyKind, gw)

		if len(policies) == 0 {
			r.removeGatewayConditionIfExists(gwStatus, conditionType, logger, gw.GetName())
		} else {
			r.updateGatewayCondition(gwStatus, PolicyAffectedCondition(policyKind.Kind, policies), gw, logger)
		}
	}

	return gwStatus
}

func (r *GatewayPolicyDiscoverabilityReconciler) buildExpectedListenerStatus(ctx context.Context, syncMap *sync.Map, gw *machinery.Gateway, listener *machinery.Listener, logger logr.Logger, policyGroupKinds []*schema.GroupKind) (gatewayapiv1.ListenerStatus, int, bool) {
	listenerStatus, index, found := lo.FindIndexOf(gw.Status.Listeners, func(item gatewayapiv1.ListenerStatus) bool {
		return item.Name == listener.Name
	})
	if !found {
		logger.V(1).Info("listener status not found", "listener", listener.GetName())
		return gatewayapiv1.ListenerStatus{}, index, false
	}

	updated := false
	for _, groupKind := range policyGroupKinds {
		conditionType := PolicyAffectedConditionType(groupKind.Kind)
		policies := r.extractPolicies(ctx, syncMap, groupKind, gw, listener)

		if len(policies) == 0 {
			updated = r.removeListenerConditionIfExists(&listenerStatus, conditionType, logger, listener.GetName()) || updated
		} else {
			condition := PolicyAffectedCondition(groupKind.Kind, policies)
			updated = r.updateListenerCondition(&listenerStatus, condition, gw, logger) || updated
		}
	}

	return listenerStatus, index, updated
}

func (r *GatewayPolicyDiscoverabilityReconciler) updateGateway(ctx context.Context, gw *machinery.Gateway) error {
	obj, err := controller.Destruct(gw.Gateway)
	if err != nil {
		return err
	}
	_, err = r.Client.Resource(controller.GatewaysResource).Namespace(gw.GetNamespace()).UpdateStatus(ctx, obj, metav1.UpdateOptions{})
	return err
}

// Extract accepted policies of a certain group kind for the specified targets
func (r *GatewayPolicyDiscoverabilityReconciler) extractPolicies(ctx context.Context, syncMap *sync.Map, policyKind *schema.GroupKind, targets ...machinery.Targetable) []machinery.Policy {
	return kuadrantv1.PoliciesInPath(targets, func(policy machinery.Policy) bool {
		return policy.GroupVersionKind().GroupKind() == *policyKind && IsPolicyAccepted(ctx, policy, syncMap)
	})
}

func (r *GatewayPolicyDiscoverabilityReconciler) updateGatewayCondition(status *gatewayapiv1.GatewayStatus, condition metav1.Condition, gw *machinery.Gateway, logger logr.Logger) bool {
	existingCondition := meta.FindStatusCondition(status.Conditions, condition.Type)
	if existingCondition != nil && equality.Semantic.DeepEqual(existingCondition, condition) {
		logger.V(1).Info("condition unchanged", "condition", condition.Type, "gateway", gw.GetName())
		return false
	}

	condition.ObservedGeneration = gw.GetGeneration()
	meta.SetStatusCondition(&status.Conditions, condition)
	logger.V(1).Info("updated condition", "condition", condition.Type, "gateway", gw.GetName())
	return true
}

func (r *GatewayPolicyDiscoverabilityReconciler) removeGatewayConditionIfExists(status *gatewayapiv1.GatewayStatus, conditionType string, logger logr.Logger, name string) bool {
	if existingCondition := meta.FindStatusCondition(status.Conditions, conditionType); existingCondition != nil {
		meta.RemoveStatusCondition(&status.Conditions, conditionType)
		logger.V(1).Info("removed condition", "condition", conditionType, "name", name)
		return true
	}
	logger.V(1).Info("condition absent, skipping removal", "condition", conditionType, "name", name)
	return false
}

func (r *GatewayPolicyDiscoverabilityReconciler) updateListenerCondition(status *gatewayapiv1.ListenerStatus, condition metav1.Condition, gw *machinery.Gateway, logger logr.Logger) bool {
	existingCondition := meta.FindStatusCondition(status.Conditions, condition.Type)
	if existingCondition != nil && equality.Semantic.DeepEqual(existingCondition, condition) {
		logger.V(1).Info("condition unchanged", "condition", condition.Type, "gateway", gw.GetName())
		return false
	}

	condition.ObservedGeneration = gw.GetGeneration()
	meta.SetStatusCondition(&status.Conditions, condition)
	logger.V(1).Info("updated condition", "condition", condition.Type, "gateway", gw.GetName())
	return true
}

func (r *GatewayPolicyDiscoverabilityReconciler) removeListenerConditionIfExists(status *gatewayapiv1.ListenerStatus, conditionType string, logger logr.Logger, name string) bool {
	if existingCondition := meta.FindStatusCondition(status.Conditions, conditionType); existingCondition != nil {
		meta.RemoveStatusCondition(&status.Conditions, conditionType)
		logger.V(1).Info("removed condition", "condition", conditionType, "name", name)
		return true
	}
	logger.V(1).Info("condition absent, skipping removal", "condition", conditionType, "name", name)
	return false
}

func (r *GatewayPolicyDiscoverabilityReconciler) extractListeners(topology *machinery.Topology, gw *machinery.Gateway) []*machinery.Listener {
	return lo.Map(topology.Targetables().Children(gw), func(item machinery.Targetable, index int) *machinery.Listener {
		listener, _ := item.(*machinery.Listener)
		return listener
	})
}
