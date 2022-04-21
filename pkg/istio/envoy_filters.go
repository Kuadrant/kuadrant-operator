package istio

import (
	"encoding/json"
	"fmt"

	protobuftypes "github.com/gogo/protobuf/types"
	istioapiv1alpha3 "istio.io/api/networking/v1alpha3"
	istionetworkingv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	apimv1alpha1 "github.com/kuadrant/kuadrant-controller/apis/apim/v1alpha1"
	"github.com/kuadrant/kuadrant-controller/pkg/common"
)

type HTTPFilterStage uint32

const (
	PreAuthStage HTTPFilterStage = iota
	PostAuthStage

	PatchedLimitadorClusterName = "rate-limit-cluster"
	PatchedWasmClusterName      = "remote-wasm-cluster"
)

const (
	EnvoysHTTPPortNumber            = 8080
	EnvoysHTTPConnectionManagerName = "envoy.filters.network.http_connection_manager"
)

type EnvoyFilterFactory struct {
	ObjectName string
	Namespace  string
	Patches    []*istioapiv1alpha3.EnvoyFilter_EnvoyConfigObjectPatch
	Labels     map[string]string
}

func (v *EnvoyFilterFactory) EnvoyFilter() *istionetworkingv1alpha3.EnvoyFilter {
	return &istionetworkingv1alpha3.EnvoyFilter{
		TypeMeta: metav1.TypeMeta{
			Kind:       "EnvoyFilter",
			APIVersion: "networking.istio.io/v1alpha3",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      v.ObjectName,
			Namespace: v.Namespace,
		},
		Spec: istioapiv1alpha3.EnvoyFilter{
			WorkloadSelector: &istioapiv1alpha3.WorkloadSelector{
				Labels: v.Labels,
			},
			ConfigPatches: v.Patches,
		},
	}
}

func LimitadorClusterEnvoyPatch() *istioapiv1alpha3.EnvoyFilter_EnvoyConfigObjectPatch {
	// The patch defines the rate_limit_cluster, which provides the endpoint location of the external rate limit service.

	patchUnstructured := map[string]interface{}{
		"operation": "ADD",
		"value": map[string]interface{}{
			"name":                   PatchedLimitadorClusterName,
			"type":                   "STRICT_DNS",
			"connect_timeout":        "1s",
			"lb_policy":              "ROUND_ROBIN",
			"http2_protocol_options": map[string]interface{}{},
			"load_assignment": map[string]interface{}{
				"cluster_name": PatchedLimitadorClusterName,
				"endpoints": []map[string]interface{}{
					{
						"lb_endpoints": []map[string]interface{}{
							{
								"endpoint": map[string]interface{}{
									"address": map[string]interface{}{
										"socket_address": map[string]interface{}{
											"address":    common.LimitadorServiceClusterHost,
											"port_value": common.LimitadorServiceGrpcPort,
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	patchRaw, _ := json.Marshal(patchUnstructured)

	patch := &istioapiv1alpha3.EnvoyFilter_Patch{}
	err := patch.UnmarshalJSON(patchRaw)
	if err != nil {
		panic(err)
	}

	return &istioapiv1alpha3.EnvoyFilter_EnvoyConfigObjectPatch{
		ApplyTo: istioapiv1alpha3.EnvoyFilter_CLUSTER,
		Match: &istioapiv1alpha3.EnvoyFilter_EnvoyConfigObjectMatch{
			ObjectTypes: &istioapiv1alpha3.EnvoyFilter_EnvoyConfigObjectMatch_Cluster{
				Cluster: &istioapiv1alpha3.EnvoyFilter_ClusterMatch{
					Service: common.LimitadorServiceClusterHost,
				},
			},
		},
		Patch: patch,
	}
}

func WasmClusterEnvoyPatch() *istioapiv1alpha3.EnvoyFilter_EnvoyConfigObjectPatch {
	patchUnstructured := map[string]interface{}{
		"operation": "ADD",
		"value": map[string]interface{}{
			"name":            PatchedWasmClusterName,
			"type":            "STRICT_DNS",
			"connect_timeout": "1s",
			"load_assignment": map[string]interface{}{
				"cluster_name": PatchedWasmClusterName,
				"endpoints": []map[string]interface{}{
					{
						"lb_endpoints": []map[string]interface{}{
							{
								"endpoint": map[string]interface{}{
									"address": map[string]interface{}{
										"socket_address": map[string]interface{}{
											"address":    "raw.githubusercontent.com",
											"port_value": 443,
										},
									},
								},
							},
						},
					},
				},
			},
			"dns_lookup_family": "V4_ONLY",
			"transport_socket": map[string]interface{}{
				"name": "envoy.transport_sockets.tls",
				"typed_config": map[string]interface{}{
					"@type": "type.googleapis.com/envoy.extensions.transport_sockets.tls.v3.UpstreamTlsContext",
					"sni":   "raw.githubusercontent.com",
				},
			},
		},
	}

	patchRaw, _ := json.Marshal(patchUnstructured)

	patch := &istioapiv1alpha3.EnvoyFilter_Patch{}
	err := patch.UnmarshalJSON(patchRaw)
	if err != nil {
		panic(err)
	}
	return &istioapiv1alpha3.EnvoyFilter_EnvoyConfigObjectPatch{
		ApplyTo: istioapiv1alpha3.EnvoyFilter_CLUSTER,
		Match: &istioapiv1alpha3.EnvoyFilter_EnvoyConfigObjectMatch{
			Context: istioapiv1alpha3.EnvoyFilter_GATEWAY,
		},
		Patch: patch,
	}
}

// EnvoyFilterRatelimitsUnstructured returns "rate_limits" envoy filter patch format from kuadrant rate limits
func EnvoyFilterRatelimitsUnstructured(rateLimits []*apimv1alpha1.RateLimit) []map[string]interface{} {
	envoyRateLimits := make([]map[string]interface{}, 0)
	for _, rateLimit := range rateLimits {
		if rateLimit.Stage == apimv1alpha1.RateLimitStageBOTH {
			// Apply same actions to both stages
			stages := []apimv1alpha1.RateLimitStage{
				apimv1alpha1.RateLimitStagePREAUTH,
				apimv1alpha1.RateLimitStagePOSTAUTH,
			}

			for _, stage := range stages {
				envoyRateLimit := map[string]interface{}{
					"stage":   apimv1alpha1.RateLimitStageValue[stage],
					"actions": rateLimit.Actions,
				}
				envoyRateLimits = append(envoyRateLimits, envoyRateLimit)
			}
		} else {
			envoyRateLimit := map[string]interface{}{
				"stage":   apimv1alpha1.RateLimitStageValue[rateLimit.Stage],
				"actions": rateLimit.Actions,
			}
			envoyRateLimits = append(envoyRateLimits, envoyRateLimit)
		}
	}

	return envoyRateLimits
}

func WASMEnvoyFilterKey(gwKey client.ObjectKey) client.ObjectKey {
	return client.ObjectKey{
		Name:      fmt.Sprintf("kuadrant-%s-wasm-ratelimits", gwKey.Name),
		Namespace: gwKey.Namespace,
	}
}

// WASMEnvoyFilter returns desired WASM envoy filter
// - Pre-Authorization ratelimit wasm filter
// - Post-Authorization ratelimit wasm filter
// - Limitador cluster (tmp-fix)
// - Wasm cluster
func WASMEnvoyFilter(rlp *apimv1alpha1.RateLimitPolicy, gwKey client.ObjectKey, gwLabels map[string]string, hosts []gatewayapiv1alpha2.Hostname) (*istionetworkingv1alpha3.EnvoyFilter, error) {
	rlpKey := client.ObjectKeyFromObject(rlp)
	preAuthPluginPolicy := PluginPolicyFromRateLimitPolicy(rlp, apimv1alpha1.RateLimitStagePREAUTH, hosts)
	postAuthPluginPolicy := PluginPolicyFromRateLimitPolicy(rlp, apimv1alpha1.RateLimitStagePOSTAUTH, hosts)

	finalPatches := []*istioapiv1alpha3.EnvoyFilter_EnvoyConfigObjectPatch{}

	preAuthPluginConfig := PluginConfig{
		FailureModeDeny: true,
		PluginPolicies: map[string]PluginPolicy{
			rlpKey.String(): *preAuthPluginPolicy,
		},
	}
	preAuthJSON, err := json.Marshal(preAuthPluginConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to marshall preauth plugin config into json")
	}

	postAuthPluginConfig := PluginConfig{
		FailureModeDeny: true,
		PluginPolicies: map[string]PluginPolicy{
			rlpKey.String(): *postAuthPluginPolicy,
		},
	}
	postAuthJSON, err := json.Marshal(postAuthPluginConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to marshall preauth plugin config into json")
	}

	patchUnstructured := map[string]interface{}{
		"operation": "INSERT_FIRST", // preauth should be the first filter in the chain
		"value": map[string]interface{}{
			"name": "envoy.filters.http.preauth.wasm",
			"typed_config": map[string]interface{}{
				"@type":   "type.googleapis.com/udpa.type.v1.TypedStruct",
				"typeUrl": "type.googleapis.com/envoy.extensions.filters.http.wasm.v3.Wasm",
				"value": map[string]interface{}{
					"config": map[string]interface{}{
						"configuration": map[string]interface{}{
							"@type": "type.googleapis.com/google.protobuf.StringValue",
							"value": string(preAuthJSON),
						},
						"name": "preauth-wasm",
						"vm_config": map[string]interface{}{
							"code": map[string]interface{}{
								"remote": map[string]interface{}{
									"http_uri": map[string]interface{}{
										"uri":     "https://raw.githubusercontent.com/rahulanand16nov/wasm-shim/new-api/deploy/wasm_shim.wasm",
										"cluster": PatchedWasmClusterName,
										"timeout": "10s",
									},
									"sha256": "de54c4d2ce405425515e14e1cc45285acf632c490de1f5f55c00e2acb832c89e",
									"retry_policy": map[string]interface{}{
										"num_retries": 10,
									},
								},
							},
							"allow_precompiled": true,
							"runtime":           "envoy.wasm.runtime.v8",
						},
					},
				},
			},
		},
	}

	patchRaw, _ := json.Marshal(patchUnstructured)
	prePatch := istioapiv1alpha3.EnvoyFilter_Patch{}
	if err := prePatch.UnmarshalJSON(patchRaw); err != nil {
		return nil, err
	}

	postPatch := prePatch.DeepCopy()

	// update filter name
	postPatch.Value.Fields["name"] = &protobuftypes.Value{
		Kind: &protobuftypes.Value_StringValue{
			StringValue: "envoy.filters.http.postauth.wasm",
		},
	}

	// update operation for postauth filter
	postPatch.Operation = istioapiv1alpha3.EnvoyFilter_Patch_INSERT_BEFORE

	pluginConfig := postPatch.Value.Fields["typed_config"].GetStructValue().Fields["value"].GetStructValue().Fields["config"]

	// update plugin config for postauth filter
	pluginConfig.GetStructValue().Fields["configuration"].GetStructValue().Fields["value"] = &protobuftypes.Value{
		Kind: &protobuftypes.Value_StringValue{
			StringValue: string(postAuthJSON),
		},
	}

	// update plugin name
	pluginConfig.GetStructValue().Fields["name"] = &protobuftypes.Value{
		Kind: &protobuftypes.Value_StringValue{
			StringValue: "postauth-wasm",
		},
	}

	preAuthFilterPatch := &istioapiv1alpha3.EnvoyFilter_EnvoyConfigObjectPatch{
		ApplyTo: istioapiv1alpha3.EnvoyFilter_HTTP_FILTER,
		Match: &istioapiv1alpha3.EnvoyFilter_EnvoyConfigObjectMatch{
			Context: istioapiv1alpha3.EnvoyFilter_GATEWAY,
			ObjectTypes: &istioapiv1alpha3.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
				Listener: &istioapiv1alpha3.EnvoyFilter_ListenerMatch{
					PortNumber: EnvoysHTTPPortNumber, // Kuadrant-gateway listens on this port by default
					FilterChain: &istioapiv1alpha3.EnvoyFilter_ListenerMatch_FilterChainMatch{
						Filter: &istioapiv1alpha3.EnvoyFilter_ListenerMatch_FilterMatch{
							Name: EnvoysHTTPConnectionManagerName,
						},
					},
				},
			},
		},
		Patch: &prePatch,
	}

	postAuthFilterPatch := preAuthFilterPatch.DeepCopy()
	postAuthFilterPatch.Patch = postPatch

	// postauth filter should be injected just before the router filter
	postAuthFilterPatch.Match.ObjectTypes = &istioapiv1alpha3.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
		Listener: &istioapiv1alpha3.EnvoyFilter_ListenerMatch{
			PortNumber: EnvoysHTTPPortNumber,
			FilterChain: &istioapiv1alpha3.EnvoyFilter_ListenerMatch_FilterChainMatch{
				Filter: &istioapiv1alpha3.EnvoyFilter_ListenerMatch_FilterMatch{
					Name: EnvoysHTTPConnectionManagerName,
					SubFilter: &istioapiv1alpha3.EnvoyFilter_ListenerMatch_SubFilterMatch{
						Name: "envoy.filters.http.router",
					},
				},
			},
		},
	}

	// since it's the first time, add the Limitador and Wasm cluster into the patches
	finalPatches = append(finalPatches, preAuthFilterPatch, postAuthFilterPatch,
		LimitadorClusterEnvoyPatch(), WasmClusterEnvoyPatch())

	wasmKey := WASMEnvoyFilterKey(gwKey)
	factory := EnvoyFilterFactory{
		ObjectName: wasmKey.Name,
		Namespace:  wasmKey.Namespace,
		Patches:    finalPatches,
		Labels:     gwLabels,
	}
	return factory.EnvoyFilter(), nil
}

func WASMEnvoyFilterPluginMutator(existingObj, desiredObj client.Object) (bool, error) {
	existing, ok := existingObj.(*istionetworkingv1alpha3.EnvoyFilter)
	if !ok {
		return false, fmt.Errorf("%T is not a *istionetworkingv1alpha3.EnvoyFilter", existingObj)
	}
	desired, ok := desiredObj.(*istionetworkingv1alpha3.EnvoyFilter)
	if !ok {
		return false, fmt.Errorf("%T is not a *istionetworkingv1alpha3.EnvoyFilter", desiredObj)
	}

	update := false

	// first patch is PRE
	// second patch is POST
	for idx := 0; idx < 2; idx++ {
		existingPatch := existing.Spec.ConfigPatches[idx]
		desiredPatch := desired.Spec.ConfigPatches[idx]

		// Deserialize existing plugin config
		// existingPluginConfigValue is a pointer to Value. It can me modified.
		existingConfigFields := existingPatch.Patch.Value.Fields["typed_config"].
			GetStructValue().Fields["value"].GetStructValue().Fields["config"].GetStructValue().
			Fields["configuration"].GetStructValue().Fields
		existingPluginConfigValue := existingConfigFields["value"]
		existingPluginConfigStr := existingPluginConfigValue.GetStringValue()
		existingPluginConfig := &PluginConfig{}
		if err := json.Unmarshal([]byte(existingPluginConfigStr), existingPluginConfig); err != nil {
			return false, fmt.Errorf("failed to unmarshal existing plugin config: %w", err)
		}

		// Deserialize desired plugin config
		desiredPluginConfigStr := desiredPatch.Patch.Value.Fields["typed_config"].
			GetStructValue().Fields["value"].GetStructValue().Fields["config"].GetStructValue().
			Fields["configuration"].GetStructValue().Fields["value"].GetStringValue()

		desiredPluginConfig := &PluginConfig{}
		if err := json.Unmarshal([]byte(desiredPluginConfigStr), desiredPluginConfig); err != nil {
			return false, fmt.Errorf("failed to unmarshal desired plugin config: %w", err)
		}
		if len(desiredPluginConfig.PluginPolicies) == 0 {
			return false, fmt.Errorf("desired plugin config has empty plugin policies")
		}
		if len(desiredPluginConfig.PluginPolicies) > 1 {
			return false, fmt.Errorf("desired plugin config has multiple policies")
		}

		patchUpdate := false
		MergeMapStringPluginPolicy(&patchUpdate, &existingPluginConfig.PluginPolicies, desiredPluginConfig.PluginPolicies)

		if patchUpdate {
			update = true
			newExistingPluginConfigSerialized, err := json.Marshal(existingPluginConfig)
			if err != nil {
				return false, fmt.Errorf("failed to marshall new existing plugin config into json: %w", err)
			}
			// Update existing envoyfilter patch value
			existingConfigFields["value"] = &protobuftypes.Value{
				Kind: &protobuftypes.Value_StringValue{
					StringValue: string(newExistingPluginConfigSerialized),
				},
			}
		}
	}

	return update, nil
}
