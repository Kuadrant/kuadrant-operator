package istio

import (
	"encoding/json"
	"fmt"

	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"github.com/samber/lo"
	"google.golang.org/protobuf/types/known/structpb"
	istioapimetav1alpha1 "istio.io/api/meta/v1alpha1"
	istioapinetworkingv1alpha3 "istio.io/api/networking/v1alpha3"
	istioapiv1beta1 "istio.io/api/type/v1beta1"
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

var (
	EnvoyFiltersResource       = istioclientgonetworkingv1alpha3.SchemeGroupVersion.WithResource("envoyfilters")
	WasmPluginsResource        = istioclientgoextensionv1alpha1.SchemeGroupVersion.WithResource("wasmplugins")
	PeerAuthenticationResource = istiosecurityv1.SchemeGroupVersion.WithResource("peerauthentications")

	EnvoyFilterGroupKind        = schema.GroupKind{Group: istioclientgonetworkingv1alpha3.GroupName, Kind: "EnvoyFilter"}
	WasmPluginGroupKind         = schema.GroupKind{Group: istioclientgoextensionv1alpha1.GroupName, Kind: "WasmPlugin"}
	PeerAuthenticationGroupKind = schema.GroupKind{Group: istiosecurityv1.GroupName, Kind: "PeerAuthentication"}
)

func EqualTargetRefs(a, b []*istioapiv1beta1.PolicyTargetReference) bool {
	return len(a) == len(b) && lo.EveryBy(a, func(aTargetRef *istioapiv1beta1.PolicyTargetReference) bool {
		return lo.SomeBy(b, func(bTargetRef *istioapiv1beta1.PolicyTargetReference) bool {
			return aTargetRef.Group == bTargetRef.Group && aTargetRef.Kind == bTargetRef.Kind && aTargetRef.Name == bTargetRef.Name && aTargetRef.Namespace == bTargetRef.Namespace
		})
	})
}

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
		"allow_on_headers_stop_iteration": true,
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
	if a.Spec.Priority != b.Spec.Priority || !EqualTargetRefs(a.Spec.TargetRefs, b.Spec.TargetRefs) {
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
				// For HTTP_FILTER patches, we compare the listener match structure if present
				// Since the structure is complex, we'll compare the JSON representation
				if aListener != nil && bListener != nil {
					aMatchJSON, aErr := json.Marshal(aConfigPatch.Match)
					bMatchJSON, _ := json.Marshal(bConfigPatch.Match)
					if string(aMatchJSON) != string(bMatchJSON) || aErr != nil {
						return false
					}
				}
			case istioapinetworkingv1alpha3.EnvoyFilter_CLUSTER:
				// CLUSTER uses cluster match
				aCluster := aConfigPatch.Match.GetCluster()
				bCluster := bConfigPatch.Match.GetCluster()
				if aCluster == nil || bCluster == nil {
					return false
				}
				if aCluster.Service != bCluster.Service || aCluster.PortNumber != bCluster.PortNumber || aCluster.Subset != bCluster.Subset {
					return false
				}
			default:
				// For other patch types, compare the match structure via JSON
				aMatchJSON, aErr := json.Marshal(aConfigPatch.Match)
				bMatchJSON, _ := json.Marshal(bConfigPatch.Match)
				if string(aMatchJSON) != string(bMatchJSON) || aErr != nil {
					return false
				}
			}

			// patch
			aPatch := aConfigPatch.Patch
			bPatch := bConfigPatch.Patch

			if aPatch.Operation != bPatch.Operation || aPatch.FilterClass != bPatch.FilterClass {
				return false
			}
			aPatchJSON, _ := aPatch.Value.MarshalJSON()
			bPatchJSON, _ := bPatch.Value.MarshalJSON()
			return string(aPatchJSON) == string(bPatchJSON)
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

func LinkGatewayToWasmPlugin(objs controller.Store) machinery.LinkFunc {
	gateways := lo.Map(objs.FilterByGroupKind(machinery.GatewayGroupKind), func(obj controller.Object, _ int) machinery.Object {
		return &machinery.Gateway{Gateway: obj.(*gatewayapiv1.Gateway)}
	})

	return machinery.LinkFunc{
		From: machinery.GatewayGroupKind,
		To:   WasmPluginGroupKind,
		Func: func(child machinery.Object) []machinery.Object {
			wasmPlugin := child.(*controller.RuntimeObject).Object.(*istioclientgoextensionv1alpha1.WasmPlugin)
			return lo.Filter(gateways, istioTargetRefsIncludeObjectFunc(wasmPlugin.Spec.TargetRefs, wasmPlugin.GetNamespace()))
		},
	}
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
			return lo.Filter(gateways, istioTargetRefsIncludeObjectFunc(envoyFilter.Spec.TargetRefs, envoyFilter.GetNamespace()))
		},
	}
}

func istioTargetRefsIncludeObjectFunc(targetRefs []*istioapiv1beta1.PolicyTargetReference, defaultNamespace string) func(machinery.Object, int) bool {
	return func(obj machinery.Object, _ int) bool {
		groupKind := obj.GroupVersionKind().GroupKind()
		return lo.SomeBy(targetRefs, func(targetRef *istioapiv1beta1.PolicyTargetReference) bool {
			if targetRef == nil {
				return false
			}
			group := targetRef.GetGroup()
			if group == "" {
				group = machinery.GatewayGroupKind.Group
			}
			kind := targetRef.GetKind()
			if kind == "" {
				kind = machinery.GatewayGroupKind.Kind
			}
			name := targetRef.GetName()
			if name == "" {
				return false
			}
			namespace := targetRef.GetNamespace()
			if namespace == "" {
				namespace = defaultNamespace
			}
			return group == groupKind.Group &&
				kind == groupKind.Kind &&
				name == obj.GetName() &&
				namespace == obj.GetNamespace()
		})
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
