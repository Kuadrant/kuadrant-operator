package istio

import (
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"github.com/samber/lo"
	istioapiv1beta1 "istio.io/api/type/v1beta1"
	istioclientgoextensionv1alpha1 "istio.io/client-go/pkg/apis/extensions/v1alpha1"
	istioclientgonetworkingv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	istioclientgosecurityv1beta1 "istio.io/client-go/pkg/apis/security/v1beta1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
)

var (
	EnvoyFiltersResource          = istioclientgonetworkingv1alpha3.SchemeGroupVersion.WithResource("envoyfilters")
	WasmPluginsResource           = istioclientgoextensionv1alpha1.SchemeGroupVersion.WithResource("wasmplugins")
	AuthorizationPoliciesResource = istioclientgosecurityv1beta1.SchemeGroupVersion.WithResource("authorizationpolicies")

	EnvoyFilterGroupKind         = schema.GroupKind{Group: istioclientgonetworkingv1alpha3.GroupName, Kind: "EnvoyFilter"}
	WasmPluginGroupKind          = schema.GroupKind{Group: istioclientgoextensionv1alpha1.GroupName, Kind: "WasmPlugin"}
	AuthorizationPolicyGroupKind = schema.GroupKind{Group: istioclientgosecurityv1beta1.GroupName, Kind: "AuthorizationPolicy"}
)

func PolicyTargetRefFromGateway(gateway *gatewayapiv1.Gateway) *istioapiv1beta1.PolicyTargetReference {
	return &istioapiv1beta1.PolicyTargetReference{
		Group: gatewayapiv1.GroupName,
		Kind:  "Gateway",
		Name:  gateway.Name,
	}
}

func EqualTargetRefs(a, b []*istioapiv1beta1.PolicyTargetReference) bool {
	return len(a) == len(b) && lo.EveryBy(a, func(sTargetRef *istioapiv1beta1.PolicyTargetReference) bool {
		return lo.SomeBy(b, func(tTargetRef *istioapiv1beta1.PolicyTargetReference) bool {
			return sTargetRef.Group == tTargetRef.Group && sTargetRef.Kind == tTargetRef.Kind && sTargetRef.Name == tTargetRef.Name && sTargetRef.Namespace == tTargetRef.Namespace
		})
	})
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

func IsAuthorizationPolicyInstalled(restMapper meta.RESTMapper) (bool, error) {
	return utils.IsCRDInstalled(
		restMapper,
		istioclientgosecurityv1beta1.GroupName,
		"AuthorizationPolicy",
		istioclientgosecurityv1beta1.SchemeGroupVersion.Version)
}

func IsIstioInstalled(restMapper meta.RESTMapper) (bool, error) {
	ok, err := IsWASMPluginInstalled(restMapper)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}

	ok, err = IsAuthorizationPolicyInstalled(restMapper)
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
				group = machinery.GatewayClassGroupKind.Group
			}
			kind := targetRef.GetKind()
			if kind == "" {
				kind = machinery.GatewayClassGroupKind.Kind
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
