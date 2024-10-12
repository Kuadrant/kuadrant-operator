package controllers

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"

	"github.com/samber/lo"
	istioextensionsv1alpha1 "istio.io/api/extensions/v1alpha1"
	istiov1beta1 "istio.io/api/type/v1beta1"
	istioclientgoextensionv1alpha1 "istio.io/client-go/pkg/apis/extensions/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/dynamic"
	"k8s.io/utils/ptr"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	kuadrantv1beta3 "github.com/kuadrant/kuadrant-operator/api/v1beta3"
	kuadrantistio "github.com/kuadrant/kuadrant-operator/pkg/istio"
	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
	"github.com/kuadrant/kuadrant-operator/pkg/rlptools/wasm"
)

// istioExtensionReconciler reconciles Istio WasmPlugin custom resources
type istioExtensionReconciler struct {
	client *dynamic.DynamicClient
}

func (r *istioExtensionReconciler) Subscription() controller.Subscription {
	return controller.Subscription{
		ReconcileFunc: r.Reconcile,
		Events: []controller.ResourceEventMatcher{ // matches reconciliation events that change the rate limit definitions or status of rate limit policies
			{Kind: &kuadrantv1beta1.KuadrantGroupKind},
			{Kind: &machinery.GatewayClassGroupKind},
			{Kind: &machinery.GatewayGroupKind},
			{Kind: &machinery.HTTPRouteGroupKind},
			{Kind: &kuadrantv1beta3.RateLimitPolicyGroupKind},
			{Kind: &kuadrantistio.WasmPluginGroupKind},
		},
	}
}

func (r *istioExtensionReconciler) Reconcile(ctx context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, state *sync.Map) error {
	logger := controller.LoggerFromContext(ctx).WithName("istioExtensionReconciler")

	logger.V(1).Info("building istio extension")
	defer logger.V(1).Info("finished building istio extension")

	// build wasm plugin policies for each gateway
	wasmPolicies, err := r.buildWasmPoliciesPerGateway(ctx, state)
	if err != nil {
		if errors.Is(err, ErrMissingStateEffectiveRateLimitPolicies) {
			logger.V(1).Info(err.Error())
		} else {
			return err
		}
	}

	// reconcile for each gateway based on the desired wasm plugin policies calculated before
	gateways := topology.Targetables().Items(func(o machinery.Object) bool {
		return o.GroupVersionKind().GroupKind() == machinery.GatewayGroupKind
	})

	for _, gateway := range gateways {
		gatewayKey := k8stypes.NamespacedName{Name: gateway.GetName(), Namespace: gateway.GetNamespace()}

		desiredWasmPlugin := r.buildWasmPluginForGateway(gateway, wasmPolicies[gateway.GetLocator()])

		resource := r.client.Resource(kuadrantistio.WasmPluginsResource).Namespace(desiredWasmPlugin.GetNamespace())

		existingWasmPluginObj, found := lo.Find(topology.Objects().Children(gateway), func(child machinery.Object) bool {
			return child.GroupVersionKind().GroupKind() == kuadrantistio.WasmPluginGroupKind && child.GetName() == desiredWasmPlugin.GetName() && child.GetNamespace() == desiredWasmPlugin.GetNamespace()
		})

		// create
		if !found {
			if utils.IsObjectTaggedToDelete(desiredWasmPlugin) {
				continue
			}
			desiredWasmPluginUnstructured, err := controller.Destruct(desiredWasmPlugin)
			if err != nil {
				logger.Error(err, "failed to destruct wasmplugin object", "gateway", gatewayKey.String(), "wasmplugin", desiredWasmPlugin)
				continue
			}
			if _, err = resource.Create(ctx, desiredWasmPluginUnstructured, metav1.CreateOptions{}); err != nil {
				logger.Error(err, "failed to create wasmplugin object", "gateway", gatewayKey.String(), "wasmplugin", desiredWasmPluginUnstructured.Object)
				// TODO: handle error
			}
			continue
		}

		existingWasmPlugin := existingWasmPluginObj.(*controller.RuntimeObject).Object.(*istioclientgoextensionv1alpha1.WasmPlugin)

		// delete
		if utils.IsObjectTaggedToDelete(desiredWasmPlugin) && !utils.IsObjectTaggedToDelete(existingWasmPlugin) {
			if err := resource.Delete(ctx, existingWasmPlugin.GetName(), metav1.DeleteOptions{}); err != nil {
				logger.Error(err, "failed to delete wasmplugin object", "gateway", gatewayKey.String(), "wasmplugin", fmt.Sprintf("%s/%s", existingWasmPlugin.GetNamespace(), existingWasmPlugin.GetName()))
				// TODO: handle error
			}
			continue
		}

		if equalWasmPlugins(existingWasmPlugin, desiredWasmPlugin) {
			logger.V(1).Info("wasmplugin object is up to date, nothing to do")
			continue
		}

		// update
		existingWasmPlugin.Spec.Url = desiredWasmPlugin.Spec.Url
		existingWasmPlugin.Spec.Phase = desiredWasmPlugin.Spec.Phase
		existingWasmPlugin.Spec.TargetRefs = desiredWasmPlugin.Spec.TargetRefs
		existingWasmPlugin.Spec.PluginConfig = desiredWasmPlugin.Spec.PluginConfig

		existingWasmPluginUnstructured, err := controller.Destruct(existingWasmPlugin)
		if err != nil {
			logger.Error(err, "failed to destruct wasmplugin object", "gateway", gatewayKey.String(), "wasmplugin", existingWasmPlugin)
			continue
		}
		if _, err = resource.Update(ctx, existingWasmPluginUnstructured, metav1.UpdateOptions{}); err != nil {
			logger.Error(err, "failed to update wasmplugin object", "gateway", gatewayKey.String(), "wasmplugin", existingWasmPluginUnstructured.Object)
			// TODO: handle error
		}
	}

	return nil
}

// buildWasmPoliciesPerGateway returns a map of gateway locators to a list of corresponding wasm policies
func (r *istioExtensionReconciler) buildWasmPoliciesPerGateway(ctx context.Context, state *sync.Map) (map[string][]wasm.Policy, error) {
	logger := controller.LoggerFromContext(ctx).WithName("istioExtensionReconciler").WithName("buildWasmPolicies")

	effectivePolicies, ok := state.Load(StateEffectiveRateLimitPolicies)
	if !ok {
		return nil, ErrMissingStateEffectiveRateLimitPolicies
	}

	logger.V(1).Info("building wasm policies for istio extension", "effectivePolicies", len(effectivePolicies.(EffectiveRateLimitPolicies)))

	wasmPolicies := make(map[string]kuadrantgatewayapi.SortableHTTPRouteRuleConfigs)

	// build wasm config for effective rate limit policies
	for pathID, effectivePolicy := range effectivePolicies.(EffectiveRateLimitPolicies) {
		// assumes the path is always [gatewayclass, gateway, listener, httproute, httprouterule]
		gatewayClass, _ := effectivePolicy.Path[0].(*machinery.GatewayClass)
		gateway, _ := effectivePolicy.Path[1].(*machinery.Gateway)
		listener, _ := effectivePolicy.Path[2].(*machinery.Listener)
		httpRoute, _ := effectivePolicy.Path[3].(*machinery.HTTPRoute)
		httpRouteRule, _ := effectivePolicy.Path[4].(*machinery.HTTPRouteRule)

		// ignore if not an istio gateway
		if gatewayClass.Spec.ControllerName != istioGatewayControllerName {
			continue
		}

		limitsNamespace := wasm.LimitsNamespaceFromRoute(httpRoute.HTTPRoute)

		var wasmRules []wasm.Rule
		for limitKey, mergeableLimit := range effectivePolicy.Spec.Rules() {
			policy, found := lo.Find(kuadrantv1.PoliciesInPath(effectivePolicy.Path, isRateLimitPolicyAcceptedAndNotDeletedFunc(state)), func(p machinery.Policy) bool {
				return p.GetLocator() == mergeableLimit.Source
			})
			if !found { // should never happen
				logger.Error(fmt.Errorf("origin policy %s not found in path %s", mergeableLimit.Source, pathID), "failed to build limitador limit definition")
				continue
			}
			limitIdentifier := wasm.LimitNameToLimitadorIdentifier(k8stypes.NamespacedName{Name: policy.GetName(), Namespace: policy.GetNamespace()}, limitKey)
			limit := mergeableLimit.Spec.(kuadrantv1beta3.Limit)
			wasmRule := wasm.RuleFromLimit(limit, limitIdentifier, limitsNamespace, *httpRouteRule.HTTPRouteRule)
			wasmRules = append(wasmRules, wasmRule)
		}

		hostnames := hostnamesFromListenerAndHTTPRoute(listener, httpRoute)

		wasmPolicies[gateway.GetLocator()] = append(wasmPolicies[gateway.GetLocator()], lo.Map(hostnames, func(hostname gatewayapiv1.Hostname, i int) kuadrantgatewayapi.HTTPRouteRuleConfig {
			return kuadrantgatewayapi.HTTPRouteRuleConfig{
				HTTPRouteRule: *httpRouteRule.HTTPRouteRule,
				Hostname:      string(hostname),
				Config: wasm.Policy{
					Name:      fmt.Sprintf("%s-%d", pathID, i),
					Hostnames: []string{string(hostname)},
					Rules:     wasmRules,
				},
			}
		})...)
	}

	return lo.MapValues(wasmPolicies, func(configs kuadrantgatewayapi.SortableHTTPRouteRuleConfigs, _ string) []wasm.Policy {
		sortedConfigs := make(kuadrantgatewayapi.SortableHTTPRouteRuleConfigs, len(configs))
		copy(sortedConfigs, configs)
		sort.Sort(sortedConfigs)
		return lo.Map(sortedConfigs, func(c kuadrantgatewayapi.HTTPRouteRuleConfig, _ int) wasm.Policy {
			wasmPolicy, _ := c.Config.(wasm.Policy)
			return wasmPolicy
		})
	}), nil
}

// buildWasmPluginForGateway reconciles the WasmPlugin custom resource for a given gateway and slice of wasm policies
func (r *istioExtensionReconciler) buildWasmPluginForGateway(gateway machinery.Targetable, wasmPolicies []wasm.Policy) *istioclientgoextensionv1alpha1.WasmPlugin {
	wasmPlugin := &istioclientgoextensionv1alpha1.WasmPlugin{
		TypeMeta: metav1.TypeMeta{
			Kind:       kuadrantistio.WasmPluginGroupKind.Kind,
			APIVersion: istioclientgoextensionv1alpha1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      wasmExtensionName(gateway.GetName()),
			Namespace: gateway.GetNamespace(),
		},
		Spec: istioextensionsv1alpha1.WasmPlugin{
			TargetRefs: []*istiov1beta1.PolicyTargetReference{
				{
					Group: machinery.GatewayGroupKind.Group,
					Kind:  machinery.GatewayGroupKind.Kind,
					Name:  gateway.GetName(),
				},
			},
			Url:          WASMFilterImageURL,
			PluginConfig: nil,
			Phase:        istioextensionsv1alpha1.PluginPhase_STATS, // insert the plugin before Istio stats filters and after Istio authorization filters.
		},
	}

	if len(wasmPolicies) == 0 {
		utils.TagObjectToDelete(wasmPlugin)
	} else {
		config := wasm.RateLimitConfig(wasmPolicies)
		pluginConfigStruct, err := config.ToStruct()
		if err != nil {
			return nil
		}
		wasmPlugin.Spec.PluginConfig = pluginConfigStruct
	}

	return wasmPlugin
}

func hostnamesFromListenerAndHTTPRoute(listener *machinery.Listener, httpRoute *machinery.HTTPRoute) []gatewayapiv1.Hostname {
	hostname := listener.Listener.Hostname
	if hostname == nil {
		hostname = ptr.To(gatewayapiv1.Hostname("*"))
	}
	hostnames := []gatewayapiv1.Hostname{*hostname}
	if routeHostnames := httpRoute.Spec.Hostnames; len(routeHostnames) > 0 {
		hostnames = lo.Filter(httpRoute.Spec.Hostnames, func(h gatewayapiv1.Hostname, _ int) bool {
			return utils.Name(h).SubsetOf(utils.Name(*hostname))
		})
	}
	return hostnames
}

func equalWasmPlugins(a, b *istioclientgoextensionv1alpha1.WasmPlugin) bool {
	if a.Spec.Url != b.Spec.Url || a.Spec.Phase != b.Spec.Phase || !kuadrantistio.EqualTargetRefs(a.Spec.TargetRefs, b.Spec.TargetRefs) {
		return false
	}

	if a.Spec.PluginConfig == nil && b.Spec.PluginConfig == nil {
		return true
	}

	var err error

	var aConfig *wasm.Config
	var bConfig *wasm.Config

	if a.Spec.PluginConfig != nil {
		aConfig, err = wasm.ConfigFromStruct(a.Spec.PluginConfig)
		if err != nil {
			return false
		}
	}

	if b.Spec.PluginConfig != nil {
		bConfig, err = wasm.ConfigFromStruct(b.Spec.PluginConfig)
		if err != nil {
			return false
		}
	}

	return aConfig != nil && bConfig != nil && aConfig.EqualTo(bConfig)
}
