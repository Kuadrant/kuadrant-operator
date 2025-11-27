package controllers

import (
	"context"
	"strings"
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
			{Kind: &kuadrantv1.AuthPolicyGroupKind},
			{Kind: &kuadrantv1.RateLimitPolicyGroupKind},
			{Kind: &kuadrantv1alpha1.TokenRateLimitPolicyGroupKind},
			{Kind: &kuadrantv1.TLSPolicyGroupKind},
			{Kind: &kuadrantv1.DNSPolicyGroupKind},
		},
		ReconcileFunc: r.reconcile,
	}
}

func (r *GatewayPolicyDiscoverabilityReconciler) reconcile(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, syncMap *sync.Map) error {
	logger := controller.LoggerFromContext(ctx).WithName("GatewayPolicyDiscoverabilityReconciler").WithName("reconcile").WithValues("context", ctx)

	gateways := lo.FilterMap(topology.Targetables().Items(), func(item machinery.Targetable, _ int) (*machinery.Gateway, bool) {
		gw, ok := item.(*machinery.Gateway)
		return gw, ok
	})
	policyKinds := policyGroupKinds()

	for _, gw := range gateways {
		updatedStatus := buildGatewayStatus(ctx, syncMap, gw, topology, logger, policyKinds)
		if !equality.Semantic.DeepEqual(updatedStatus, gw.Status) {
			gw.Status = *updatedStatus
			if err := r.updateGatewayStatus(ctx, gw); err != nil {
				if strings.Contains(err.Error(), "StorageError: invalid object") {
					logger.Info("possible error updating resource", "err", err, "possible_cause", "resource has being removed from the cluster already")
					continue
				}
				logger.Error(err, "failed to update gateway status", "gateway", gw.GetName())
			}
		}
	}

	return nil
}

func (r *GatewayPolicyDiscoverabilityReconciler) updateGatewayStatus(ctx context.Context, gw *machinery.Gateway) error {
	obj, err := controller.Destruct(gw.Gateway)
	if err != nil {
		return err
	}
	_, err = r.Client.Resource(controller.GatewaysResource).Namespace(gw.GetNamespace()).UpdateStatus(ctx, obj, metav1.UpdateOptions{})
	return err
}

func buildGatewayStatus(ctx context.Context, syncMap *sync.Map, gw *machinery.Gateway, topology *machinery.Topology, logger logr.Logger, policyKinds []*schema.GroupKind) *gatewayapiv1.GatewayStatus {
	status := gw.Status.DeepCopy()

	listeners := lo.Map(topology.Targetables().Children(gw), func(item machinery.Targetable, _ int) *machinery.Listener {
		listener, _ := item.(*machinery.Listener)
		return listener
	})

	for _, listener := range listeners {
		updatedListenerStatus := updateListenerStatus(ctx, syncMap, gw, listener, logger, policyKinds)
		status.Listeners = updateListenerList(status.Listeners, updatedListenerStatus)
	}

	for _, policyKind := range policyKinds {
		updatePolicyConditions(ctx, syncMap, gw, policyKind, status, logger)
	}

	return status
}

func updateListenerStatus(ctx context.Context, syncMap *sync.Map, gw *machinery.Gateway, listener *machinery.Listener, logger logr.Logger, policyKinds []*schema.GroupKind) gatewayapiv1.ListenerStatus {
	status, _, exists := findListenerStatus(gw.Status.Listeners, listener.Name)
	if !exists {
		status = gatewayapiv1.ListenerStatus{Name: listener.Name, Conditions: []metav1.Condition{}}
	}

	for _, kind := range policyKinds {
		conditionType := PolicyAffectedConditionType(kind.Kind)
		policies := extractAcceptedPolicies(ctx, syncMap, kind, gw, listener)

		if len(policies) == 0 {
			removeConditionIfExists(&status.Conditions, conditionType, logger, listener.GetName())
		} else {
			addOrUpdateCondition(&status.Conditions, PolicyAffectedCondition(kind.Kind, policies), gw.GetGeneration(), logger)
		}
	}

	return status
}

func updatePolicyConditions(ctx context.Context, syncMap *sync.Map, gw *machinery.Gateway, policyKind *schema.GroupKind, status *gatewayapiv1.GatewayStatus, logger logr.Logger) {
	conditionType := PolicyAffectedConditionType(policyKind.Kind)
	policies := extractAcceptedPolicies(ctx, syncMap, policyKind, gw)

	if len(policies) == 0 {
		removeConditionIfExists(&status.Conditions, conditionType, logger, gw.GetName())
	} else {
		addOrUpdateCondition(&status.Conditions, PolicyAffectedCondition(policyKind.Kind, policies), gw.GetGeneration(), logger)
	}
}

func addOrUpdateCondition(conditions *[]metav1.Condition, condition metav1.Condition, generation int64, logger logr.Logger) {
	existingCondition := meta.FindStatusCondition(*conditions, condition.Type)
	if existingCondition != nil && equality.Semantic.DeepEqual(*existingCondition, condition) {
		logger.V(1).Info("condition unchanged", "condition", condition.Type)
		return
	}

	condition.ObservedGeneration = generation
	meta.SetStatusCondition(conditions, condition)
	logger.V(1).Info("updated condition", "condition", condition.Type)
}

func removeConditionIfExists(conditions *[]metav1.Condition, conditionType string, logger logr.Logger, name string) {
	if meta.RemoveStatusCondition(conditions, conditionType) {
		logger.V(1).Info("removed condition", "condition", conditionType, "name", name)
	} else {
		logger.V(1).Info("condition absent, skipping removal", "condition", conditionType, "name", name)
	}
}

func updateListenerList(listeners []gatewayapiv1.ListenerStatus, updatedStatus gatewayapiv1.ListenerStatus) []gatewayapiv1.ListenerStatus {
	_, index, exists := findListenerStatus(listeners, updatedStatus.Name)
	if exists {
		listeners[index] = updatedStatus
	} else {
		listeners = append(listeners, updatedStatus)
	}
	return listeners
}

func findListenerStatus(listeners []gatewayapiv1.ListenerStatus, name gatewayapiv1.SectionName) (gatewayapiv1.ListenerStatus, int, bool) {
	for i, status := range listeners {
		if status.Name == name {
			return status, i, true
		}
	}
	return gatewayapiv1.ListenerStatus{}, -1, false
}

func extractAcceptedPolicies(ctx context.Context, syncMap *sync.Map, policyKind *schema.GroupKind, targets ...machinery.Targetable) []machinery.Policy {
	return kuadrantv1.PoliciesInPath(targets, func(policy machinery.Policy) bool {
		return policy.GroupVersionKind().GroupKind() == *policyKind && IsPolicyAccepted(ctx, policy, syncMap) // Use enforced policies instead?
	})
}
