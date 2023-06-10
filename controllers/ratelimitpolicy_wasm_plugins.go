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

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
	"github.com/kuadrant/kuadrant-operator/pkg/reconcilers"
	"github.com/kuadrant/kuadrant-operator/pkg/rlptools"
)

func (r *RateLimitPolicyReconciler) reconcileWASMPluginConf(ctx context.Context, rlp *kuadrantv1beta1.RateLimitPolicy, gwDiffObj *reconcilers.GatewayDiff) error {
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
func (r *RateLimitPolicyReconciler) wasmPluginConfig(ctx context.Context,
	gw common.GatewayWrapper, rlpRefs []client.ObjectKey) (*rlptools.WASMPlugin, error) {
	logger, _ := logr.FromContext(ctx)

	routeRLPList := make([]*kuadrantv1beta1.RateLimitPolicy, 0)
	var gwRLP *kuadrantv1beta1.RateLimitPolicy
	for _, rlpKey := range rlpRefs {
		rlp := &kuadrantv1beta1.RateLimitPolicy{}
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

	gatewayActions := rlptools.GatewayActionsByDomain{}

	if gwRLP != nil {
		if len(gw.Hostnames()) == 0 {
			// wildcard domain
			gatewayActions["*"] = append(gatewayActions["*"], rlptools.GatewayActionsFromRateLimitPolicy(gwRLP, nil)...)
		} else {
			for _, gwHostname := range gw.Hostnames() {
				gatewayActions[gwHostname] = append(gatewayActions[gwHostname], rlptools.GatewayActionsFromRateLimitPolicy(gwRLP, nil)...)
			}
		}
	}

	for _, httpRouteRLP := range routeRLPList {
		httpRoute, err := r.FetchValidHTTPRoute(ctx, httpRouteRLP.TargetKey())
		if err != nil {
			return nil, err
		}

		// gateways limits merged with the route level limits
		mergedGatewayActions := mergeGatewayActions(httpRouteRLP, gwRLP, httpRoute)
		// routeLimits referenced by multiple hostnames
		for _, hostname := range httpRoute.Spec.Hostnames {
			gatewayActions[string(hostname)] = append(gatewayActions[string(hostname)], mergedGatewayActions...)
		}
	}

	wasmPlugin := &rlptools.WASMPlugin{
		FailureModeDeny:   true,
		RateLimitPolicies: make([]rlptools.RateLimitPolicy, 0),
	}

	// One RateLimitPolicy per domain
	for domain, gatewayActionList := range gatewayActions {
		rateLimitPolicy := rlptools.RateLimitPolicy{
			Name:            domain,
			RateLimitDomain: common.MarshallNamespace(gw.Key(), domain),
			UpstreamCluster: common.KuadrantRateLimitClusterName,
			Hostnames:       []string{domain},
			GatewayActions:  gatewayActionList,
		}
		wasmPlugin.RateLimitPolicies = append(wasmPlugin.RateLimitPolicies, rateLimitPolicy)
	}

	return wasmPlugin, nil
}

// merge operations currently implemented with list append operation
func mergeGatewayActions(routeRLP *kuadrantv1beta1.RateLimitPolicy, gwRLP *kuadrantv1beta1.RateLimitPolicy, route *gatewayapiv1beta1.HTTPRoute) []rlptools.GatewayAction {
	gatewayActions := rlptools.GatewayActionsFromRateLimitPolicy(routeRLP, route)

	if gwRLP == nil {
		return gatewayActions
	}

	// add gateway level actions
	return append(gatewayActions, rlptools.GatewayActionsFromRateLimitPolicy(gwRLP, nil)...)
}
