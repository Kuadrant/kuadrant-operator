package controllers

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	istioextensionsv1alpha1 "istio.io/api/extensions/v1alpha1"
	istioclientgoextensionv1alpha1 "istio.io/client-go/pkg/apis/extensions/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

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
		if !common.ContainsObjectKey(rlpRefs, rlpKey) {
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
func (r *RateLimitPolicyReconciler) wasmPluginConfig(ctx context.Context, gw common.GatewayWrapper, rlpRefs []client.ObjectKey) (*wasm.WASMPlugin, error) {
	logger, _ := logr.FromContext(ctx)
	logger = logger.WithName("wasmPluginConfig").WithValues("gateway", gw.Key())

	gwHostnames := gw.Hostnames()
	if len(gwHostnames) == 0 {
		gwHostnames = []gatewayapiv1beta1.Hostname{"*"}
	}

	type routesPerRLP struct {
		rlp    kuadrantv1beta2.RateLimitPolicy
		routes []gatewayapiv1beta1.HTTPRoute
	}
	rlps := make(map[string]*routesPerRLP, 0)

	for _, rlpKey := range rlpRefs {
		rlp := &kuadrantv1beta2.RateLimitPolicy{}
		err := r.Client().Get(ctx, rlpKey, rlp)
		logger.V(1).Info("get rlp", "ratelimitpolicy", rlpKey, "err", err)
		if err != nil {
			return nil, err
		}

		// target ref is a HTTPRoute
		if common.IsTargetRefHTTPRoute(rlp.Spec.TargetRef) {
			httpRoute, err := r.FetchValidHTTPRoute(ctx, rlp.TargetKey())
			if err != nil {
				return nil, err
			}
			rlps[rlpKey.String()] = &routesPerRLP{rlp: *rlp, routes: []gatewayapiv1beta1.HTTPRoute{*httpRoute}}
			continue
		}

		// target ref is a Gateway
		if rlps[rlpKey.String()] != nil {
			return nil, fmt.Errorf("wasmPluginConfig: multiple gateway RLP found and only one expected. rlp keys: %v", rlpRefs)
		}
		rlps[rlpKey.String()] = &routesPerRLP{rlp: *rlp, routes: r.FetchAcceptedGatewayHTTPRoutes(ctx, rlp.TargetKey())}
		// should we reset the hostnames in the route with the hostnames of the gateway? otherwise the rlp that targets
		// the gateway can only specify hostnames for route selection exactly as they are stated in the routes, not as they
		// stated in the gateway, and this would be counterintuitive for the user, because, unlike other types of route
		// selection (e.g. by path), the hostname is part of the gateway spec.
	}

	wasmPlugin := &wasm.WASMPlugin{
		FailureMode:       wasm.FailureModeDeny,
		RateLimitPolicies: make([]wasm.RateLimitPolicy, 0),
	}

	if logger.V(1).Enabled() {
		numRoutes := 0
		for _, rlp := range rlps {
			numRoutes += len(rlp.routes)
		}
		logger.V(1).Info("build configs for rlps and routes", "#rlps", len(rlps), "#routes", numRoutes)
	}

	for _, routesPerRLP := range rlps {
		rlp := routesPerRLP.rlp
		for _, httpRoute := range routesPerRLP.routes {
			logger.V(1).Info("building config for rlp and route", "ratelimitpolicy", client.ObjectKeyFromObject(&rlp), "httproute", client.ObjectKeyFromObject(&httpRoute))

			// modifies (in memory) the list of hostnames specified in the route so we don't generate rules that only apply to other gateways
			hostnames := common.FilterValidSubdomains(gwHostnames, httpRoute.Spec.Hostnames)
			if len(hostnames) == 0 { // it should only happen when the route specifies no hostnames
				hostnames = gwHostnames
			}
			httpRoute.Spec.Hostnames = hostnames

			wasmPlugin.RateLimitPolicies = append(wasmPlugin.RateLimitPolicies, wasm.RateLimitPolicy{
				Name:      client.ObjectKeyFromObject(&rlp).String(),
				Domain:    common.MarshallNamespace(gw.Key(), string(hostnames[0])), // TODO(guicassolato): https://github.com/Kuadrant/kuadrant-operator/issues/201. Meanwhile, we are using the first hostname so it matches at least one set of limit definitions in the Limitador CR
				Rules:     rlptools.WasmRules(&rlp, &httpRoute),
				Hostnames: common.HostnamesToStrings(hostnames), // we might be listing more hostnames than needed due to route selectors hostnames possibly being more restrictive
				Service:   common.KuadrantRateLimitClusterName,
			})
		}
	}

	return wasmPlugin, nil
}
