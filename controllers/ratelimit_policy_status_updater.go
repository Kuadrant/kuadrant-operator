package controllers

import (
	"context"
	"fmt"
	"slices"
	"sync"

	envoygatewayv1alpha1 "github.com/envoyproxy/gateway/api/v1alpha1"
	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"github.com/samber/lo"
	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/utils/ptr"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	kuadrantv1beta3 "github.com/kuadrant/kuadrant-operator/api/v1beta3"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
	kuadrantenvoygateway "github.com/kuadrant/kuadrant-operator/pkg/envoygateway"
	kuadrantistio "github.com/kuadrant/kuadrant-operator/pkg/istio"
	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
)

type RateLimitPolicyStatusUpdater struct {
	client *dynamic.DynamicClient
}

// RateLimitPolicyStatusUpdater subscribe to events with potential impact on the status of RateLimitPolicy resources
func (r *RateLimitPolicyStatusUpdater) Subscription() controller.Subscription {
	return controller.Subscription{
		ReconcileFunc: r.UpdateStatus,
		Events: []controller.ResourceEventMatcher{
			{Kind: &kuadrantv1beta1.KuadrantGroupKind},
			{Kind: &machinery.GatewayClassGroupKind},
			{Kind: &machinery.GatewayGroupKind},
			{Kind: &machinery.HTTPRouteGroupKind},
			{Kind: &kuadrantv1beta3.RateLimitPolicyGroupKind},
			{Kind: &kuadrantv1beta1.LimitadorGroupKind},
			{Kind: &kuadrantistio.EnvoyFilterGroupKind},
			{Kind: &kuadrantistio.WasmPluginGroupKind},
			{Kind: &kuadrantenvoygateway.EnvoyPatchPolicyGroupKind},
			{Kind: &kuadrantenvoygateway.EnvoyExtensionPolicyGroupKind},
		},
	}
}

func (r *RateLimitPolicyStatusUpdater) UpdateStatus(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, state *sync.Map) error {
	logger := controller.LoggerFromContext(ctx).WithName("RateLimitPolicyStatusUpdater")

	policies := lo.FilterMap(topology.Policies().Items(), func(item machinery.Policy, index int) (*kuadrantv1beta3.RateLimitPolicy, bool) {
		p, ok := item.(*kuadrantv1beta3.RateLimitPolicy)
		return p, ok
	})

	policyAcceptedFunc := rateLimitPolicyAcceptedStatusFunc(state)

	logger.V(1).Info("updating ratelimitpolicy statuses", "policies", len(policies))
	defer logger.V(1).Info("finished updating ratelimitpolicy statuses")

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
			enforcedCond := r.enforcedCondition(policy, topology, state)
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
			// TODO: handle error
		}
	}

	return nil
}

func (r *RateLimitPolicyStatusUpdater) enforcedCondition(policy *kuadrantv1beta3.RateLimitPolicy, topology *machinery.Topology, state *sync.Map) *metav1.Condition {
	policyKind := kuadrantv1beta3.RateLimitPolicyGroupKind.Kind

	effectivePolicies, ok := state.Load(StateEffectiveRateLimitPolicies)
	if !ok {
		return kuadrant.EnforcedCondition(policy, kuadrant.NewErrUnknown(policyKind, ErrMissingStateEffectiveRateLimitPolicies), false)
	}

	type affectedGateway struct {
		gateway      *machinery.Gateway
		gatewayClass *machinery.GatewayClass
	}

	// check the state of the rules of the policy in the effective policies
	policyRuleKeys := lo.Keys(policy.Rules())
	overridingPolicies := map[string][]string{}      // policyRuleKey → locators of policies overriding the policy rule
	affectedGateways := map[string]affectedGateway{} // Gateway locator → {GatewayClass, Gateway}
	for _, effectivePolicy := range effectivePolicies.(EffectiveRateLimitPolicies) {
		if len(kuadrantv1.PoliciesInPath(effectivePolicy.Path, func(p machinery.Policy) bool { return p.GetLocator() == policy.GetLocator() })) == 0 {
			continue
		}
		gatewayClass, gateway, listener, httpRoute, _, _ := common.ObjectsInRequestPath(effectivePolicy.Path)
		if !kuadrantgatewayapi.IsListenerReady(listener.Listener, gateway.Gateway) || !kuadrantgatewayapi.IsHTTPRouteReady(httpRoute.HTTPRoute, gateway.Gateway, gatewayClass.GatewayClass.Spec.ControllerName) {
			continue
		}
		effectivePolicyRules := effectivePolicy.Spec.Rules()
		for _, policyRuleKey := range policyRuleKeys {
			if effectivePolicyRule, ok := effectivePolicyRules[policyRuleKey]; !ok || (ok && effectivePolicyRule.GetSource() != policy.GetLocator()) { // policy rule has been overridden by another policy
				var overriddenBy string
				if ok { // TODO(guicassolato): !ok → we cannot tell which policy is overriding the rule, this information is lost when the policy rule is dropped during an atomic override
					overriddenBy = effectivePolicyRule.GetSource()
				}
				overridingPolicies[policyRuleKey] = append(overridingPolicies[policyRuleKey], overriddenBy)
				continue
			}
			// policy rule is in the effective policy, track the Gateway affected by the policy
			affectedGateways[gateway.GetLocator()] = affectedGateway{
				gateway:      gateway,
				gatewayClass: gatewayClass,
			}
		}
	}

	if len(affectedGateways) == 0 { // no rules of the policy found in the effective policies
		if len(overridingPolicies) == 0 { // no rules of the policy have been overridden by any other policy
			return kuadrant.EnforcedCondition(policy, kuadrant.NewErrNoRoutes(policyKind), false)
		}
		// all rules of the policy have been overridden by at least one other policy
		overridingPoliciesKeys := lo.FilterMap(lo.Uniq(lo.Flatten(lo.Values(overridingPolicies))), func(policyLocator string, _ int) (k8stypes.NamespacedName, bool) {
			policyKey, err := common.NamespacedNameFromLocator(policyLocator)
			return policyKey, err == nil
		})
		return kuadrant.EnforcedCondition(policy, kuadrant.NewErrOverridden(policyKind, overridingPoliciesKeys), false)
	}

	var componentsToSync []string

	// check the status of Limitador
	if limitadorLimitsModified, stateLimitadorLimitsModifiedPresent := state.Load(StateLimitadorLimitsModified); stateLimitadorLimitsModifiedPresent && limitadorLimitsModified.(bool) {
		componentsToSync = append(componentsToSync, kuadrantv1beta1.LimitadorGroupKind.Kind)
	} else {
		limitador, err := GetLimitadorFromTopology(topology)
		if err != nil {
			return kuadrant.EnforcedCondition(policy, kuadrant.NewErrUnknown(policyKind, err), false)
		}
		if !meta.IsStatusConditionTrue(limitador.Status.Conditions, limitadorv1alpha1.StatusConditionReady) {
			componentsToSync = append(componentsToSync, kuadrantv1beta1.LimitadorGroupKind.Kind)
		}
	}

	// check the status of the gateways' configuration resources
	for _, g := range affectedGateways {
		switch g.gatewayClass.Spec.ControllerName {
		case istioGatewayControllerName:
			// EnvoyFilter
			istioRateLimitClustersModifiedGateways, _ := state.Load(StateIstioRateLimitClustersModified)
			componentsToSync = append(componentsToSync, gatewayComponentsToSync(g.gateway, kuadrantistio.EnvoyFilterGroupKind, istioRateLimitClustersModifiedGateways, topology, func(obj machinery.Object) bool {
				// return meta.IsStatusConditionTrue(lo.Map(obj.(*controller.RuntimeObject).Object.(*istioclientgonetworkingv1alpha3.EnvoyFilter).Status.Conditions, kuadrantistio.ConditionToProperConditionFunc), "Ready")
				return true // Istio won't ever populate the status stanza of EnvoyFilter resources, so we cannot expect to find a given a condition there
			})...)
			// WasmPlugin
			istioExtensionsModifiedGateways, _ := state.Load(StateIstioExtensionsModified)
			componentsToSync = append(componentsToSync, gatewayComponentsToSync(g.gateway, kuadrantistio.WasmPluginGroupKind, istioExtensionsModifiedGateways, topology, func(obj machinery.Object) bool {
				// return meta.IsStatusConditionTrue(lo.Map(obj.(*controller.RuntimeObject).Object.(*istioclientgoextensionv1alpha1.WasmPlugin).Status.Conditions, kuadrantistio.ConditionToProperConditionFunc), "Ready")
				return true // Istio won't ever populate the status stanza of WasmPlugin resources, so we cannot expect to find a given a condition there
			})...)
		case envoyGatewayGatewayControllerName:
			gatewayAncestor := gatewayapiv1.ParentReference{Name: gatewayapiv1.ObjectName(g.gateway.GetName()), Namespace: ptr.To(gatewayapiv1.Namespace(g.gateway.GetNamespace()))}
			// EnvoyPatchPolicy
			envoyGatewayRateLimitClustersModifiedGateways, _ := state.Load(StateEnvoyGatewayRateLimitClustersModified)
			componentsToSync = append(componentsToSync, gatewayComponentsToSync(g.gateway, kuadrantenvoygateway.EnvoyPatchPolicyGroupKind, envoyGatewayRateLimitClustersModifiedGateways, topology, func(obj machinery.Object) bool {
				return meta.IsStatusConditionTrue(kuadrantgatewayapi.PolicyStatusConditionsFromAncestor(obj.(*controller.RuntimeObject).Object.(*envoygatewayv1alpha1.EnvoyPatchPolicy).Status, envoyGatewayGatewayControllerName, gatewayAncestor, gatewayapiv1.Namespace(obj.GetNamespace())), string(envoygatewayv1alpha1.PolicyConditionProgrammed))
			})...)
			// EnvoyExtensionPolicy
			envoyGatewayExtensionsModifiedGateways, _ := state.Load(StateEnvoyGatewayExtensionsModified)
			componentsToSync = append(componentsToSync, gatewayComponentsToSync(g.gateway, kuadrantenvoygateway.EnvoyExtensionPolicyGroupKind, envoyGatewayExtensionsModifiedGateways, topology, func(obj machinery.Object) bool {
				return meta.IsStatusConditionTrue(kuadrantgatewayapi.PolicyStatusConditionsFromAncestor(obj.(*controller.RuntimeObject).Object.(*envoygatewayv1alpha1.EnvoyExtensionPolicy).Status, envoyGatewayGatewayControllerName, gatewayAncestor, gatewayapiv1.Namespace(obj.GetNamespace())), string(gatewayapiv1alpha2.PolicyConditionAccepted))
			})...)
		default:
			componentsToSync = append(componentsToSync, fmt.Sprintf("%s (%s/%s)", machinery.GatewayGroupKind.Kind, g.gateway.GetNamespace(), g.gateway.GetName()))
		}
	}

	if len(componentsToSync) > 0 {
		return kuadrant.EnforcedCondition(policy, kuadrant.NewErrOutOfSync(policyKind, componentsToSync), false)
	}

	return kuadrant.EnforcedCondition(policy, nil, len(overridingPolicies) == 0)
}
