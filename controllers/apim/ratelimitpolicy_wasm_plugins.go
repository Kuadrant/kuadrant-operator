package apim

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	istioextensionsv1alpha1 "istio.io/api/extensions/v1alpha1"
	istiotypev1beta1 "istio.io/api/type/v1beta1"
	istioclientgoextensionv1alpha1 "istio.io/client-go/pkg/apis/extensions/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	apimv1alpha1 "github.com/kuadrant/kuadrant-controller/apis/apim/v1alpha1"
	"github.com/kuadrant/kuadrant-controller/pkg/common"
	"github.com/kuadrant/kuadrant-controller/pkg/rlptools"
)

func (r *RateLimitPolicyReconciler) reconcileWASMPluginConf(ctx context.Context, rlp *apimv1alpha1.RateLimitPolicy, gwDiffObj *gatewayDiff) error {
	logger, _ := logr.FromContext(ctx)

	for _, leftGateway := range gwDiffObj.LeftGateways {
		logger.V(1).Info("reconcileWASMPluginConf: left gateways", "gw key", leftGateway.Key())
		rlpRefs := leftGateway.RLPRefs()
		rlpKey := client.ObjectKeyFromObject(rlp)
		// Remove the RLP key from the reference list. Only if it exists (it should)
		if refID := common.FindObjectKey(rlpRefs, rlpKey); refID != len(rlpRefs) {
			// remove index
			rlpRefs = append(rlpRefs[:refID], rlpRefs[refID+1:]...)
		}
		wp, err := r.gatewayWASMPlugin(ctx, leftGateway.Gateway, rlpRefs)
		if err != nil {
			return err
		}
		err = r.ReconcileResource(ctx, &istioclientgoextensionv1alpha1.WasmPlugin{}, wp, rlptools.WASMPluginMutator)
		if err != nil {
			return err
		}
	}

	for _, sameGateway := range gwDiffObj.SameGateways {
		logger.V(1).Info("reconcileWASMPluginConf: same gateways", "gw key", sameGateway.Key())
		wp, err := r.gatewayWASMPlugin(ctx, sameGateway.Gateway, sameGateway.RLPRefs())
		if err != nil {
			return err
		}
		err = r.ReconcileResource(ctx, &istioclientgoextensionv1alpha1.WasmPlugin{}, wp, rlptools.WASMPluginMutator)
		if err != nil {
			return err
		}
	}

	for _, newGateway := range gwDiffObj.NewGateways {
		logger.V(1).Info("reconcileWASMPluginConf: new gateways", "gw key", newGateway.Key())
		rlpRefs := newGateway.RLPRefs()
		rlpKey := client.ObjectKeyFromObject(rlp)
		// Add the RLP key to the reference list. Only if it does not exist (it should not)
		if !common.ContainsObjectKey(rlpRefs, rlpKey) {
			rlpRefs = append(newGateway.RLPRefs(), rlpKey)
		}
		wp, err := r.gatewayWASMPlugin(ctx, newGateway.Gateway, rlpRefs)
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

func (r *RateLimitPolicyReconciler) gatewayWASMPlugin(ctx context.Context, gw *gatewayapiv1alpha2.Gateway, rlpRefs []client.ObjectKey) (*istioclientgoextensionv1alpha1.WasmPlugin, error) {
	logger, _ := logr.FromContext(ctx)
	logger.V(1).Info("gatewayWASMPlugin", "gwKey", client.ObjectKeyFromObject(gw), "rlpRefs", rlpRefs)

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
			Selector: &istiotypev1beta1.WorkloadSelector{
				MatchLabels: gw.Labels,
			},
			Url:          rlptools.WASMFilterImageURL,
			PluginConfig: nil,
			// Insert plugin before Istio stats filters and after Istio authorization filters.
			Phase: istioextensionsv1alpha1.PluginPhase_STATS,
		},
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
func (r *RateLimitPolicyReconciler) wasmPluginConfig(ctx context.Context, gw *gatewayapiv1alpha2.Gateway, rlpRefs []client.ObjectKey) (*rlptools.WASMPlugin, error) {
	logger, _ := logr.FromContext(ctx)
	gwKey := client.ObjectKeyFromObject(gw)
	// Load all relevant rate limit policies
	routeRLPList := make([]*apimv1alpha1.RateLimitPolicy, 0)
	gwRLPList := make([]*apimv1alpha1.RateLimitPolicy, 0)
	for _, rlpKey := range rlpRefs {
		rlp := &apimv1alpha1.RateLimitPolicy{}
		err := r.Client().Get(ctx, rlpKey, rlp)
		logger.V(1).Info("gatewayWASMPlugin", "get rlp", rlpKey, "err", err)
		if err != nil {
			return nil, err
		}

		if rlp.IsForHTTPRoute() {
			routeRLPList = append(routeRLPList, rlp)
		} else if rlp.IsForGateway() {
			gwRLPList = append(gwRLPList, rlp)
		}
	}

	if len(routeRLPList) < 1 {
		// not
		return nil, nil
	}

	gatewayActions := rlptools.GatewayActionsByDomain{}

	// iterate over HTTPRoute RLP's.
	// Gateway level RLP's alone do not make sense.
	// The request would be rejected by the router with 404
	for _, httpRouteRLP := range routeRLPList {
		httpRoute, err := r.fetchHTTPRoute(ctx, httpRouteRLP)
		if err != nil {
			return nil, err
		}

		// gateways limits merged with the route level limits
		mergedGatewayActions := mergeGatewayActions(httpRouteRLP, gwRLPList)
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
			RateLimitDomain: common.MarshallNamespace(gwKey, domain),
			UpstreamCluster: common.KuadrantRateLimitClusterName,
			Hostnames:       []string{domain},
			GatewayActions:  gatewayActionList,
		}
		wasmPlugin.RateLimitPolicies = append(wasmPlugin.RateLimitPolicies, rateLimitPolicy)
	}

	return wasmPlugin, nil
}

// merge operations currently implemented with list append operation
func mergeGatewayActions(routeRLP *apimv1alpha1.RateLimitPolicy, gwRLPs []*apimv1alpha1.RateLimitPolicy) []rlptools.GatewayAction {
	gatewayActions := make([]rlptools.GatewayAction, 0)

	// add route level gateway actions
	for idx := range routeRLP.Spec.RateLimits {
		gatewayActions = append(gatewayActions, rlptools.GatewayActionFromRateLimit(&routeRLP.Spec.RateLimits[idx]))
	}

	// add gateway level limits
	for _, gwRLP := range gwRLPs {
		for idx := range gwRLP.Spec.RateLimits {
			gatewayActions = append(gatewayActions, rlptools.GatewayActionFromRateLimit(&gwRLP.Spec.RateLimits[idx]))
		}
	}

	return gatewayActions
}
