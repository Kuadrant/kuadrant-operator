package istio

import (
	"encoding/json"
	"fmt"
	"maps"

	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"github.com/samber/lo"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
	istioapimetav1alpha1 "istio.io/api/meta/v1alpha1"
	istioapinetworkingv1alpha3 "istio.io/api/networking/v1alpha3"
	istioclientgoextensionv1alpha1 "istio.io/client-go/pkg/apis/extensions/v1alpha1"
	istioclientgonetworkingv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	istiosecurityv1 "istio.io/client-go/pkg/apis/security/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/internal/utils"
)

const GatewayNameLabel = "gateway.networking.k8s.io/gateway-name"

var (
	EnvoyFiltersResource       = istioclientgonetworkingv1alpha3.SchemeGroupVersion.WithResource("envoyfilters")
	WasmPluginsResource        = istioclientgoextensionv1alpha1.SchemeGroupVersion.WithResource("wasmplugins")
	PeerAuthenticationResource = istiosecurityv1.SchemeGroupVersion.WithResource("peerauthentications")

	EnvoyFilterGroupKind        = schema.GroupKind{Group: istioclientgonetworkingv1alpha3.GroupName, Kind: "EnvoyFilter"}
	PeerAuthenticationGroupKind = schema.GroupKind{Group: istiosecurityv1.GroupName, Kind: "PeerAuthentication"}
)

// BuildEnvoyFilterClusterPatch returns an envoy config patch that adds a cluster to the gateway.
func BuildEnvoyFilterClusterPatch(host string, port int, mtls bool, clusterPatchBuilder func(string, int, bool) map[string]any) ([]*istioapinetworkingv1alpha3.EnvoyFilter_EnvoyConfigObjectPatch, error) {
	patchRaw, _ := json.Marshal(map[string]any{"operation": "ADD", "value": clusterPatchBuilder(host, port, mtls)})
	patch := &istioapinetworkingv1alpha3.EnvoyFilter_Patch{}
	if err := patch.UnmarshalJSON(patchRaw); err != nil {
		return nil, err
	}

	return []*istioapinetworkingv1alpha3.EnvoyFilter_EnvoyConfigObjectPatch{
		{
			ApplyTo: istioapinetworkingv1alpha3.EnvoyFilter_CLUSTER,
			Match: &istioapinetworkingv1alpha3.EnvoyFilter_EnvoyConfigObjectMatch{
				ObjectTypes: &istioapinetworkingv1alpha3.EnvoyFilter_EnvoyConfigObjectMatch_Cluster{
					Cluster: &istioapinetworkingv1alpha3.EnvoyFilter_ClusterMatch{
						Service: host,
					},
				},
			},
			Patch: patch,
		},
	}, nil
}

// BuildEnvoyFilterWasmPatch returns an envoy config patch that adds a wasm HTTP filter to the gateway.
func BuildEnvoyFilterWasmPatch(wasmURL, imagePullSecret, imageSHA, clusterName string, pluginConfig *structpb.Struct) ([]*istioapinetworkingv1alpha3.EnvoyFilter_EnvoyConfigObjectPatch, error) {
	wasmFilterConfig, err := buildWasmFilterConfig(wasmURL, imagePullSecret, imageSHA, clusterName, pluginConfig)
	if err != nil {
		return nil, err
	}

	patchValue := map[string]any{
		"name": "envoy.filters.http.wasm",
		"typed_config": map[string]any{
			"@type":    "type.googleapis.com/udpa.type.v1.TypedStruct",
			"type_url": "type.googleapis.com/envoy.extensions.filters.http.wasm.v3.Wasm",
			"value":    wasmFilterConfig,
		},
	}

	patchRaw, _ := json.Marshal(map[string]any{
		"operation": "INSERT_BEFORE",
		"value":     patchValue,
	})
	patch := &istioapinetworkingv1alpha3.EnvoyFilter_Patch{}
	if err := patch.UnmarshalJSON(patchRaw); err != nil {
		return nil, err
	}

	return []*istioapinetworkingv1alpha3.EnvoyFilter_EnvoyConfigObjectPatch{
		{
			ApplyTo: istioapinetworkingv1alpha3.EnvoyFilter_HTTP_FILTER,
			Match: &istioapinetworkingv1alpha3.EnvoyFilter_EnvoyConfigObjectMatch{
				Context: istioapinetworkingv1alpha3.EnvoyFilter_GATEWAY,
				ObjectTypes: &istioapinetworkingv1alpha3.EnvoyFilter_EnvoyConfigObjectMatch_Listener{
					Listener: &istioapinetworkingv1alpha3.EnvoyFilter_ListenerMatch{
						FilterChain: &istioapinetworkingv1alpha3.EnvoyFilter_ListenerMatch_FilterChainMatch{
							Filter: &istioapinetworkingv1alpha3.EnvoyFilter_ListenerMatch_FilterMatch{
								Name: "envoy.filters.network.http_connection_manager",
								SubFilter: &istioapinetworkingv1alpha3.EnvoyFilter_ListenerMatch_SubFilterMatch{
									Name: "envoy.filters.http.router",
								},
							},
						},
					},
				},
			},
			Patch: patch,
		},
	}, nil
}

// buildWasmFilterConfig builds the Envoy wasm filter configuration
func buildWasmFilterConfig(wasmURL, imagePullSecret, imageSHA, clusterName string, pluginConfig *structpb.Struct) (map[string]any, error) {
	config := map[string]any{
		"name":    "kuadrant-wasm-shim",
		"root_id": "kuadrant_wasm_shim",
		"vm_config": map[string]any{
			"runtime": "envoy.wasm.runtime.v8",
			"code": map[string]any{
				"remote": map[string]any{
					"http_uri": map[string]any{
						"uri":     wasmURL,
						"timeout": "10s",
						"cluster": clusterName,
					},
					"sha256": imageSHA,
				},
			},
			"allow_precompiled": true,
		},
		"failure_policy": "FAIL_RELOAD",
	}

	if pluginConfig != nil {
		configJSON, err := pluginConfig.MarshalJSON()
		if err != nil {
			return nil, fmt.Errorf("failed to marshal plugin config: %w", err)
		}
		config["configuration"] = map[string]any{
			"@type": "type.googleapis.com/google.protobuf.StringValue",
			"value": string(configJSON),
		}
	}

	// Add image pull secret if provided
	if imagePullSecret != "" {
		if vmConfig, ok := config["vm_config"].(map[string]any); ok {
			if code, ok := vmConfig["code"].(map[string]any); ok {
				if remote, ok := code["remote"].(map[string]any); ok {
					remote["image_pull_secret"] = imagePullSecret
				}
			}
		}
	}

	return map[string]any{
		"config": config,
	}, nil
}

func EqualEnvoyFilters(a, b *istioclientgonetworkingv1alpha3.EnvoyFilter) bool {
	if a.Spec.Priority != b.Spec.Priority || !maps.Equal(a.Spec.WorkloadSelector.GetLabels(), b.Spec.WorkloadSelector.GetLabels()) {
		return false
	}

	aConfigPatches := a.Spec.ConfigPatches
	bConfigPatches := b.Spec.ConfigPatches
	if len(aConfigPatches) != len(bConfigPatches) {
		return false
	}
	return lo.EveryBy(aConfigPatches, func(aConfigPatch *istioapinetworkingv1alpha3.EnvoyFilter_EnvoyConfigObjectPatch) bool {
		return lo.SomeBy(bConfigPatches, func(bConfigPatch *istioapinetworkingv1alpha3.EnvoyFilter_EnvoyConfigObjectPatch) bool {
			if aConfigPatch == nil && bConfigPatch == nil {
				return true
			}
			if (aConfigPatch == nil && bConfigPatch != nil) || (aConfigPatch != nil && bConfigPatch == nil) {
				return false
			}

			// apply_to
			if aConfigPatch.ApplyTo != bConfigPatch.ApplyTo {
				return false
			}

			// match comparison depends on patch type
			switch aConfigPatch.ApplyTo {
			case istioapinetworkingv1alpha3.EnvoyFilter_HTTP_FILTER:
				// HTTP_FILTER uses listener match
				aListener := aConfigPatch.Match.GetListener()
				bListener := bConfigPatch.Match.GetListener()
				if (aListener == nil) != (bListener == nil) {
					return false
				}
				// For HTTP_FILTER patches, compare the match structure using protobuf equality
				if aListener != nil && bListener != nil {
					if !proto.Equal(aConfigPatch.Match, bConfigPatch.Match) {
						return false
					}
				}
			case istioapinetworkingv1alpha3.EnvoyFilter_CLUSTER:
				// CLUSTER uses cluster match
				aCluster := aConfigPatch.Match.GetCluster()
				bCluster := bConfigPatch.Match.GetCluster()
				if (aCluster == nil) != (bCluster == nil) {
					return false
				}
				if aCluster.Service != bCluster.Service || aCluster.PortNumber != bCluster.PortNumber || aCluster.Subset != bCluster.Subset {
					return false
				}
			default:
				// For other patch types, compare the match structure using protobuf equality
				if !proto.Equal(aConfigPatch.Match, bConfigPatch.Match) {
					return false
				}
			}

			// patch
			aPatch := aConfigPatch.Patch
			bPatch := bConfigPatch.Patch

			if aPatch.Operation != bPatch.Operation || aPatch.FilterClass != bPatch.FilterClass {
				return false
			}

			// Use protobuf equality for patch values to handle non-deterministic map ordering.
			return proto.Equal(aPatch.Value, bPatch.Value)
		})
	})
}

func ConditionToProperConditionFunc(istioCondition *istioapimetav1alpha1.IstioCondition, _ int) metav1.Condition {
	return metav1.Condition{
		Type:    istioCondition.GetType(),
		Status:  metav1.ConditionStatus(istioCondition.GetStatus()),
		Reason:  istioCondition.GetReason(),
		Message: istioCondition.GetMessage(),
	}
}

func IsEnvoyFilterInstalled(restMapper meta.RESTMapper) (bool, error) {
	return utils.IsCRDInstalled(
		restMapper,
		istioclientgonetworkingv1alpha3.GroupName,
		"EnvoyFilter",
		istioclientgonetworkingv1alpha3.SchemeGroupVersion.Version)
}

func IsWASMPluginInstalled(restMapper meta.RESTMapper) (bool, error) {
	return utils.IsCRDInstalled(
		restMapper,
		istioclientgoextensionv1alpha1.GroupName,
		"WasmPlugin",
		istioclientgoextensionv1alpha1.SchemeGroupVersion.Version)
}

func IsPeerAuthenticationInstalled(restMapper meta.RESTMapper) (bool, error) {
	return utils.IsCRDInstalled(
		restMapper,
		istiosecurityv1.GroupName,
		"PeerAuthentication",
		istiosecurityv1.SchemeGroupVersion.Version)
}

func IsIstioInstalled(restMapper meta.RESTMapper) (bool, error) {
	ok, err := IsWASMPluginInstalled(restMapper)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}

	ok, err = IsEnvoyFilterInstalled(restMapper)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}

	ok, err = IsPeerAuthenticationInstalled(restMapper)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}

	// Istio found
	return true, nil
}

func LinkGatewayToEnvoyFilter(objs controller.Store) machinery.LinkFunc {
	gateways := lo.Map(objs.FilterByGroupKind(machinery.GatewayGroupKind), func(obj controller.Object, _ int) machinery.Object {
		return &machinery.Gateway{Gateway: obj.(*gatewayapiv1.Gateway)}
	})

	return machinery.LinkFunc{
		From: machinery.GatewayGroupKind,
		To:   EnvoyFilterGroupKind,
		Func: func(child machinery.Object) []machinery.Object {
			envoyFilter := child.(*controller.RuntimeObject).Object.(*istioclientgonetworkingv1alpha3.EnvoyFilter)
			gatewayName := envoyFilter.Spec.WorkloadSelector.GetLabels()[GatewayNameLabel]
			if gatewayName == "" {
				// todo(remove): fallback migration of targetRefs to workloadSelector
				for _, ref := range envoyFilter.Spec.TargetRefs {
					group := ref.GetGroup()
					if group == "" {
						group = machinery.GatewayGroupKind.Group
					}
					kind := ref.GetKind()
					if kind == "" {
						kind = machinery.GatewayGroupKind.Kind
					}
					if group == machinery.GatewayGroupKind.Group && kind == machinery.GatewayGroupKind.Kind {
						gatewayName = ref.GetName()
						break
					}
				}
			}
			return lo.Filter(gateways, func(obj machinery.Object, _ int) bool {
				return obj.GetName() == gatewayName && obj.GetNamespace() == envoyFilter.GetNamespace()
			})
		},
	}
}

func LinkKuadrantToPeerAuthentication(objs controller.Store) machinery.LinkFunc {
	kuadrants := lo.Map(objs.FilterByGroupKind(kuadrantv1beta1.KuadrantGroupKind), controller.ObjectAs[machinery.Object])

	return machinery.LinkFunc{
		From: kuadrantv1beta1.KuadrantGroupKind,
		To:   PeerAuthenticationGroupKind,
		Func: func(child machinery.Object) []machinery.Object {
			return lo.Filter(kuadrants, func(k machinery.Object, _ int) bool {
				return k.GetNamespace() == child.GetNamespace() && child.GetName() == "default"
			})
		},
	}
}
