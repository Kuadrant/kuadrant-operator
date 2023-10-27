package controllers

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"golang.org/x/exp/slices"
	istioextensionsv1alpha1 "istio.io/api/extensions/v1alpha1"
	istioclientgoextensionv1alpha1 "istio.io/client-go/pkg/apis/extensions/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
	"github.com/kuadrant/kuadrant-operator/pkg/reconcilers"
	"github.com/kuadrant/kuadrant-operator/pkg/rlptools"
	"github.com/kuadrant/kuadrant-operator/pkg/rlptools/wasm"
)

func (r *RateLimitPolicyReconciler) reconcileWASMPluginConf(ctx context.Context, rlp *kuadrantv1beta2.RateLimitPolicy, gwDiffObj *reconcilers.GatewayDiff) error {
	logger, _ := logr.FromContext(ctx)

	for _, gw := range gwDiffObj.GatewaysWithInvalidPolicyRef {
		logger.V(1).Info("reconcileWASMPluginConf: gateway with invalid policy ref", "gw key", gw.Key())
		rlpRefs := gw.PolicyRefs()
		rlpKey := client.ObjectKeyFromObject(rlp)
		// Remove the RLP key from the reference list. Only if it exists (it should)
		if refID := common.FindObjectKey(rlpRefs, rlpKey); refID != len(rlpRefs) {
			// remove index
			rlpRefs = append(rlpRefs[:refID], rlpRefs[refID+1:]...)
		}
		wp, err := r.gatewayWASMPlugin(ctx, gw, rlpRefs)
		if err != nil {
			return err
		}
		err = r.ReconcileResource(ctx, &istioclientgoextensionv1alpha1.WasmPlugin{}, wp, rlptools.WASMPluginMutator)
		if err != nil {
			return err
		}
	}

	for _, gw := range gwDiffObj.GatewaysWithValidPolicyRef {
		logger.V(1).Info("reconcileWASMPluginConf: gateway with valid policy ref", "gw key", gw.Key())
		wp, err := r.gatewayWASMPlugin(ctx, gw, gw.PolicyRefs())
		if err != nil {
			return err
		}
		err = r.ReconcileResource(ctx, &istioclientgoextensionv1alpha1.WasmPlugin{}, wp, rlptools.WASMPluginMutator)
		if err != nil {
			return err
		}
	}

	for _, gw := range gwDiffObj.GatewaysMissingPolicyRef {
		logger.V(1).Info("reconcileWASMPluginConf: gateway missing policy ref", "gw key", gw.Key())
		rlpRefs := gw.PolicyRefs()
		rlpKey := client.ObjectKeyFromObject(rlp)
		// Add the RLP key to the reference list. Only if it does not exist (it should not)
		if !slices.Contains(rlpRefs, rlpKey) {
			rlpRefs = append(gw.PolicyRefs(), rlpKey)
		}
		wp, err := r.gatewayWASMPlugin(ctx, gw, rlpRefs)
		if err != nil {
			return err
		}
		err = r.ReconcileResource(ctx, &istioclientgoextensionv1alpha1.WasmPlugin{}, wp, rlptools.WASMPluginMutator)
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *RateLimitPolicyReconciler) gatewayWASMPlugin(ctx context.Context, gw common.GatewayWrapper, rlpRefs []client.ObjectKey) (*istioclientgoextensionv1alpha1.WasmPlugin, error) {
	logger, _ := logr.FromContext(ctx)
	logger.V(1).Info("gatewayWASMPlugin", "gwKey", gw.Key(), "rlpRefs", rlpRefs)

	wasmPlugin := &istioclientgoextensionv1alpha1.WasmPlugin{
		TypeMeta: metav1.TypeMeta{
			Kind:       "WasmPlugin",
			APIVersion: "extensions.istio.io/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("kuadrant-%s", gw.Name),
			Namespace: gw.Namespace,
		},
		Spec: istioextensionsv1alpha1.WasmPlugin{
			Selector:     common.IstioWorkloadSelectorFromGateway(ctx, r.Client(), gw.Gateway),
			Url:          rlptools.WASMFilterImageURL,
			PluginConfig: nil,
			// Insert plugin before Istio stats filters and after Istio authorization filters.
			Phase: istioextensionsv1alpha1.PluginPhase_STATS,
		},
	}

	if len(rlpRefs) < 1 {
		common.TagObjectToDelete(wasmPlugin)
		return wasmPlugin, nil
	}

	pluginConfig, err := r.wasmPluginConfig(ctx, gw, rlpRefs)
	if err != nil {
		return nil, err
	}

	if pluginConfig == nil {
		common.TagObjectToDelete(wasmPlugin)
		return wasmPlugin, nil
	}

	pluginConfigStruct, err := pluginConfig.ToStruct()
	if err != nil {
		return nil, err
	}

	wasmPlugin.Spec.PluginConfig = pluginConfigStruct

	return wasmPlugin, nil
}

// returns nil when there is no rate limit policy to apply
func (r *RateLimitPolicyReconciler) wasmPluginConfig(ctx context.Context, gw common.GatewayWrapper, rlpRefs []client.ObjectKey) (*wasm.Plugin, error) {
	logger, _ := logr.FromContext(ctx)
	logger = logger.WithName("wasmPluginConfig").WithValues("gateway", gw.Key())

	type store struct {
		rlp   kuadrantv1beta2.RateLimitPolicy
		route gatewayapiv1.HTTPRoute
		skip  bool
	}
	rlps := make(map[string]*store, len(rlpRefs))
	routeKeys := make(map[string]struct{}, 0)
	var gwRLPKey string

	// store all rlps and find the one that targets the gateway (if there is one)
	for _, rlpKey := range rlpRefs {
		rlp := &kuadrantv1beta2.RateLimitPolicy{}
		err := r.Client().Get(ctx, rlpKey, rlp)
		logger.V(1).Info("get rlp", "ratelimitpolicy", rlpKey, "err", err)
		if err != nil {
			return nil, err
		}

		// target ref is a HTTPRoute
		if common.IsTargetRefHTTPRoute(rlp.Spec.TargetRef) {
			route, err := r.FetchValidHTTPRoute(ctx, rlp.TargetKey())
			if err != nil {
				return nil, err
			}
			rlps[rlpKey.String()] = &store{rlp: *rlp, route: *route}
			routeKeys[client.ObjectKeyFromObject(route).String()] = struct{}{}
			continue
		}

		// target ref is a Gateway
		if rlps[rlpKey.String()] != nil {
			return nil, fmt.Errorf("wasmPluginConfig: multiple gateway RLP found and only one expected. rlp keys: %v", rlpRefs)
		}
		gwRLPKey = rlpKey.String()
		rlps[gwRLPKey] = &store{rlp: *rlp}
	}

	gwHostnames := gw.Hostnames()
	if len(gwHostnames) == 0 {
		gwHostnames = []gatewayapiv1.Hostname{"*"}
	}

	// if there is a gateway rlp, fake a single httproute with all rules from all httproutes accepted by the gateway,
	// that do not have a rlp of its own, so we can generate wasm rules for those cases
	if gwRLPKey != "" {
		rules := make([]gatewayapiv1.HTTPRouteRule, 0)
		routes := r.FetchAcceptedGatewayHTTPRoutes(ctx, rlps[gwRLPKey].rlp.TargetKey())
		for idx := range routes {
			route := routes[idx]
			// skip routes that have a rlp of its own
			if _, found := routeKeys[client.ObjectKeyFromObject(&route).String()]; found {
				continue
			}
			rules = append(rules, route.Spec.Rules...)
		}
		if len(rules) == 0 {
			logger.V(1).Info("no httproutes attached to the targeted gateway, skipping wasm config for the gateway rlp", "ratelimitpolicy", gwRLPKey)
			rlps[gwRLPKey].skip = true
		} else {
			rlps[gwRLPKey].route = gatewayapiv1.HTTPRoute{
				Spec: gatewayapiv1.HTTPRouteSpec{
					Hostnames: gwHostnames,
					Rules:     rules,
				},
			}
		}
	}

	wasmPlugin := &wasm.Plugin{
		FailureMode:       wasm.FailureModeDeny,
		RateLimitPolicies: make([]wasm.RateLimitPolicy, 0),
	}

	for _, rlpKey := range rlpRefs {
		s := rlps[rlpKey.String()]
		if s.skip {
			continue
		}
		rlp := s.rlp
		route := s.route

		// narrow the list of hostnames specified in the route so we don't generate wasm rules that only apply to other gateways
		// this is a no-op for the gateway rlp
		hostnames := common.FilterValidSubdomains(gwHostnames, route.Spec.Hostnames)
		if len(hostnames) == 0 { // it should only happen when the route specifies no hostnames
			hostnames = gwHostnames
		}
		route.Spec.Hostnames = hostnames

		rules := rlptools.WasmRules(&rlp, &route)
		if len(rules) == 0 {
			continue // no need to add the policy if there are no rules; a rlp can return no rules if all its limits fail to match any route rule
		}

		wasmPlugin.RateLimitPolicies = append(wasmPlugin.RateLimitPolicies, wasm.RateLimitPolicy{
			Name:      rlpKey.String(),
			Domain:    rlptools.LimitsNamespaceFromRLP(&rlp),
			Rules:     rules,
			Hostnames: common.HostnamesToStrings(hostnames), // we might be listing more hostnames than needed due to route selectors hostnames possibly being more restrictive
			Service:   common.KuadrantRateLimitClusterName,
		})
	}

	// avoid building a wasm plugin config if there are no rules to apply
	if len(wasmPlugin.RateLimitPolicies) == 0 {
		return nil, nil
	}

	return wasmPlugin, nil
}
