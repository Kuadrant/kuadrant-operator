package istio

import (
	"encoding/json"

	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"github.com/samber/lo"
	istioapimetav1alpha1 "istio.io/api/meta/v1alpha1"
	istioapinetworkingv1alpha3 "istio.io/api/networking/v1alpha3"
	istioapiv1beta1 "istio.io/api/type/v1beta1"
	istioclientgoextensionv1alpha1 "istio.io/client-go/pkg/apis/extensions/v1alpha1"
	istioclientgonetworkingv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kuadrant/kuadrant-operator/internal/utils"
)

var (
	EnvoyFiltersResource = istioclientgonetworkingv1alpha3.SchemeGroupVersion.WithResource("envoyfilters")
	WasmPluginsResource  = istioclientgoextensionv1alpha1.SchemeGroupVersion.WithResource("wasmplugins")

	EnvoyFilterGroupKind = schema.GroupKind{Group: istioclientgonetworkingv1alpha3.GroupName, Kind: "EnvoyFilter"}
	WasmPluginGroupKind  = schema.GroupKind{Group: istioclientgoextensionv1alpha1.GroupName, Kind: "WasmPlugin"}
)

func EqualTargetRefs(a, b []*istioapiv1beta1.PolicyTargetReference) bool {
	return len(a) == len(b) && lo.EveryBy(a, func(aTargetRef *istioapiv1beta1.PolicyTargetReference) bool {
		return lo.SomeBy(b, func(bTargetRef *istioapiv1beta1.PolicyTargetReference) bool {
			return aTargetRef.Group == bTargetRef.Group && aTargetRef.Kind == bTargetRef.Kind && aTargetRef.Name == bTargetRef.Name && aTargetRef.Namespace == bTargetRef.Namespace
		})
	})
}

// BuildEnvoyFilterClusterPatch returns an envoy config patch that adds a cluster to the gateway.
func BuildEnvoyFilterClusterPatch(host string, port int, clusterPatchBuilder func(string, int) map[string]any) ([]*istioapinetworkingv1alpha3.EnvoyFilter_EnvoyConfigObjectPatch, error) {
	patchRaw, _ := json.Marshal(map[string]any{"operation": "ADD", "value": clusterPatchBuilder(host, port)})
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

			// cluster match
			aCluster := aConfigPatch.Match.GetCluster()
			bCluster := bConfigPatch.Match.GetCluster()
			if aCluster == nil || bCluster == nil {
				return false
			}
			if aCluster.Service != bCluster.Service || aCluster.PortNumber != bCluster.PortNumber || aCluster.Subset != bCluster.Subset {
				return false
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
