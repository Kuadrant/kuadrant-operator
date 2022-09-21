package rlptools

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"

	_struct "github.com/golang/protobuf/ptypes/struct"
	istioclientgoextensionv1alpha1 "istio.io/client-go/pkg/apis/extensions/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	apimv1alpha1 "github.com/kuadrant/kuadrant-controller/apis/apim/v1alpha1"
	"github.com/kuadrant/kuadrant-controller/pkg/common"
)

var (
	WASMFilterImageURL = common.FetchEnv("WASM_FILTER_IMAGE", "oci://quay.io/kuadrant/wasm-shim:latest")
)

type GatewayAction struct {
	Configurations []apimv1alpha1.Configuration `json:"configurations"`

	// +optional
	Rules []apimv1alpha1.Rule `json:"rules,omitempty"`
}

// GatewayActionsFromRateLimitPolicy return flatten list from GatewayAction from the RLP
func GatewayActionsFromRateLimitPolicy(rlp *apimv1alpha1.RateLimitPolicy, route *gatewayapiv1alpha2.HTTPRoute) []GatewayAction {
	flattenActions := make([]GatewayAction, 0)
	if rlp == nil {
		return flattenActions
	}

	for idx := range rlp.Spec.RateLimits {
		// if HTTPRoute is available, fill empty rules with defaults from the route
		rules := rlp.Spec.RateLimits[idx].Rules
		if route != nil && len(rules) == 0 {
			rules = HTTPRouteRulesToRLPRules(common.RulesFromHTTPRoute(route))
		}

		flattenActions = append(flattenActions, GatewayAction{
			Configurations: rlp.Spec.RateLimits[idx].Configurations,
			Rules:          rules,
		})
	}

	return flattenActions
}

func HTTPRouteRulesToRLPRules(httpRouteRules []common.HTTPRouteRule) []apimv1alpha1.Rule {
	rlpRules := make([]apimv1alpha1.Rule, 0, len(httpRouteRules))
	for idx := range httpRouteRules {
		var tmp []string
		rlpRules = append(rlpRules, apimv1alpha1.Rule{
			// copy slice
			Paths:   append(tmp, httpRouteRules[idx].Paths...),
			Methods: append(tmp, httpRouteRules[idx].Methods...),
			Hosts:   append(tmp, httpRouteRules[idx].Hosts...),
		})
	}
	return rlpRules
}

type RateLimitPolicy struct {
	Name            string   `json:"name"`
	RateLimitDomain string   `json:"rate_limit_domain"`
	UpstreamCluster string   `json:"upstream_cluster"`
	Hostnames       []string `json:"hostnames"`
	// +optional
	GatewayActions []GatewayAction `json:"gateway_actions,omitempty"`
}

type WASMPlugin struct {
	FailureModeDeny   bool              `json:"failure_mode_deny"`
	RateLimitPolicies []RateLimitPolicy `json:"rate_limit_policies"`
}

func (w *WASMPlugin) ToStruct() (*_struct.Struct, error) {
	wasmPluginJSON, err := json.Marshal(w)
	if err != nil {
		return nil, err
	}

	pluginConfigStruct := &_struct.Struct{}
	if err := pluginConfigStruct.UnmarshalJSON(wasmPluginJSON); err != nil {
		return nil, err
	}
	return pluginConfigStruct, nil
}

func WASMPluginFromStruct(structure *_struct.Struct) (*WASMPlugin, error) {
	if structure == nil {
		return nil, errors.New("cannot desestructure WASMPlugin from nil")
	}
	// Serialize struct into json
	configJSON, err := structure.MarshalJSON()
	if err != nil {
		return nil, err
	}
	// Deserialize struct into PluginConfig struct
	wasmPlugin := &WASMPlugin{}
	if err := json.Unmarshal(configJSON, wasmPlugin); err != nil {
		return nil, err
	}

	return wasmPlugin, nil
}

type GatewayActionsByDomain map[string][]GatewayAction

func (g GatewayActionsByDomain) String() string {
	jsonData, _ := json.MarshalIndent(g, "", "  ")
	return string(jsonData)
}

func WASMPluginMutator(existingObj, desiredObj client.Object) (bool, error) {
	update := false
	existing, ok := existingObj.(*istioclientgoextensionv1alpha1.WasmPlugin)
	if !ok {
		return false, fmt.Errorf("%T is not a *istioclientgoextensionv1alpha1.WasmPlugin", existingObj)
	}
	desired, ok := desiredObj.(*istioclientgoextensionv1alpha1.WasmPlugin)
	if !ok {
		return false, fmt.Errorf("%T is not a *istioclientgoextensionv1alpha1.WasmPlugin", desiredObj)
	}

	existingWASMPlugin, err := WASMPluginFromStruct(existing.Spec.PluginConfig)
	if err != nil {
		return false, err
	}

	desiredWASMPlugin, err := WASMPluginFromStruct(desired.Spec.PluginConfig)
	if err != nil {
		return false, err
	}

	// TODO(eastizle): reflect.DeepEqual does not work well with lists without order
	if !reflect.DeepEqual(desiredWASMPlugin, existingWASMPlugin) {
		update = true
		existing.Spec.PluginConfig = desired.Spec.PluginConfig
	}

	return update, nil
}
