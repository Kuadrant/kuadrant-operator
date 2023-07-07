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

	routeRLPList := make([]*kuadrantv1beta2.RateLimitPolicy, 0)
	var gwRLP *kuadrantv1beta2.RateLimitPolicy
	for _, rlpKey := range rlpRefs {
		rlp := &kuadrantv1beta2.RateLimitPolicy{}
		err := r.Client().Get(ctx, rlpKey, rlp)
		logger.V(1).Info("wasmPluginConfig", "get rlp", rlpKey, "err", err)
		if err != nil {
			return nil, err
		}

		if common.IsTargetRefHTTPRoute(rlp.Spec.TargetRef) {
			routeRLPList = append(routeRLPList, rlp)
		} else if common.IsTargetRefGateway(rlp.Spec.TargetRef) {
			if gwRLP == nil {
				gwRLP = rlp
			} else {
				return nil, fmt.Errorf("wasmPluginConfig: multiple gateway RLP found and only one expected. rlp keys: %v", rlpRefs)
			}
		}
	}

	wasmPlugin := &wasm.WASMPlugin{
		FailureMode:       wasm.FailureModeDeny,
		RateLimitPolicies: make([]wasm.RateLimitPolicy, 0),
	}

	gwHostnames := gw.Hostnames()
	if len(gwHostnames) == 0 {
		gwHostnames = []gatewayapiv1beta1.Hostname{"*"}
	}

	if gwRLP != nil {
		// TODO(guicassolato): this is a hack until we start going through all the httproutes that are children of the gateway and build the rules for each httproute
		route := &gatewayapiv1beta1.HTTPRoute{
			Spec: gatewayapiv1beta1.HTTPRouteSpec{
				Hostnames: gwHostnames,
				Rules:     []gatewayapiv1beta1.HTTPRouteRule{{}},
			},
		}

		wasmPlugin.RateLimitPolicies = append(wasmPlugin.RateLimitPolicies, wasm.RateLimitPolicy{
			Name:      client.ObjectKeyFromObject(gwRLP).String(),
			Domain:    common.MarshallNamespace(gw.Key(), string(gwHostnames[0])), // TODO(guicassolato): https://github.com/Kuadrant/kuadrant-operator/issues/201. Meanwhile, we are using the first hostname so it matches at least one set of limit definitions in the Limitador CR
			Rules:     rlptools.WasmRules(gwRLP, route, gwHostnames),
			Hostnames: common.HostnamesToStrings(gwHostnames), // we might be listing more hostnames than needed due to route selectors hostnames possibly being more restrictive
			Service:   common.KuadrantRateLimitClusterName,
		})
	}

	for _, httpRouteRLP := range routeRLPList {
		httpRoute, err := r.FetchValidHTTPRoute(ctx, httpRouteRLP.TargetKey())
		if err != nil {
			return nil, err
		}

		// filter the route hostnames to only the ones that are children of the gateway
		hostnames := common.FilterValidSubdomains(gwHostnames, httpRoute.Spec.Hostnames)
		if len(hostnames) == 0 {
			hostnames = gwHostnames
		}

		wasmPlugin.RateLimitPolicies = append(wasmPlugin.RateLimitPolicies, wasm.RateLimitPolicy{
			Name:      client.ObjectKeyFromObject(httpRouteRLP).String(),
			Domain:    common.MarshallNamespace(gw.Key(), string(hostnames[0])), // TODO(guicassolato): https://github.com/Kuadrant/kuadrant-operator/issues/201. Meanwhile, we are using the first hostname so it matches at least one set of limit definitions in the Limitador CR
			Rules:     rlptools.WasmRules(httpRouteRLP, httpRoute, hostnames),
			Hostnames: common.HostnamesToStrings(hostnames), // we might be listing more hostnames than needed due to route selectors hostnames possibly being more restrictive
			Service:   common.KuadrantRateLimitClusterName,
		})
	}

	return wasmPlugin, nil
}
