package istio

import (
	"context"

	"github.com/go-logr/logr"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"github.com/samber/lo"
	istiocommon "istio.io/api/type/v1beta1"
	istioclientgoextensionv1alpha1 "istio.io/client-go/pkg/apis/extensions/v1alpha1"
	istioclientnetworkingv1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	istioclientgosecurityv1beta1 "istio.io/client-go/pkg/apis/security/v1beta1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
)

var (
	EnvoyFiltersResource          = istioclientnetworkingv1alpha3.SchemeGroupVersion.WithResource("envoyfilters")
	WasmPluginsResource           = istioclientgoextensionv1alpha1.SchemeGroupVersion.WithResource("wasmplugins")
	AuthorizationPoliciesResource = istioclientgosecurityv1beta1.SchemeGroupVersion.WithResource("authorizationpolicies")

	EnvoyFilterGroupKind         = schema.GroupKind{Group: istioclientnetworkingv1alpha3.GroupName, Kind: "EnvoyFilter"}
	WasmPluginGroupKind          = schema.GroupKind{Group: istioclientgoextensionv1alpha1.GroupName, Kind: "WasmPlugin"}
	AuthorizationPolicyGroupKind = schema.GroupKind{Group: istioclientgosecurityv1beta1.GroupName, Kind: "AuthorizationPolicy"}
)

func WorkloadSelectorFromGateway(ctx context.Context, k8sClient client.Client, gateway *gatewayapiv1.Gateway) *istiocommon.WorkloadSelector {
	logger, _ := logr.FromContext(ctx)
	gatewayWorkloadSelector, err := kuadrantgatewayapi.GetGatewayWorkloadSelector(ctx, k8sClient, gateway)
	if err != nil {
		logger.V(1).Info("failed to build Istio WorkloadSelector from Gateway service - falling back to Gateway labels")
		gatewayWorkloadSelector = gateway.Labels
	}
	return &istiocommon.WorkloadSelector{
		MatchLabels: gatewayWorkloadSelector,
	}
}

func PolicyTargetRefFromGateway(gateway *gatewayapiv1.Gateway) *istiocommon.PolicyTargetReference {
	return &istiocommon.PolicyTargetReference{
		Group: gatewayapiv1.GroupName,
		Kind:  "Gateway",
		Name:  gateway.Name,
	}
}

func IsEnvoyFilterInstalled(restMapper meta.RESTMapper) (bool, error) {
	return utils.IsCRDInstalled(
		restMapper,
		istioclientnetworkingv1alpha3.GroupName,
		"EnvoyFilter",
		istioclientnetworkingv1alpha3.SchemeGroupVersion.Version)
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
	gateways := lo.Map(objs.FilterByGroupKind(machinery.GatewayGroupKind), controller.ObjectAs[*gatewayapiv1.Gateway])

	return machinery.LinkFunc{
		From: machinery.GatewayGroupKind,
		To:   WasmPluginGroupKind,
		Func: func(child machinery.Object) []machinery.Object {
			wasmPlugin := child.(*controller.RuntimeObject).Object.(*istioclientgoextensionv1alpha1.WasmPlugin)
			return lo.FilterMap(gateways, func(g *gatewayapiv1.Gateway, _ int) (machinery.Object, bool) {
				gateway := &machinery.Gateway{Gateway: g}
				targetRef := wasmPlugin.Spec.TargetRef
				if targetRef == nil {
					return gateway, false
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
					return gateway, false
				}
				namespace := targetRef.GetNamespace()
				if namespace == "" {
					namespace = wasmPlugin.GetNamespace()
				}
				return gateway, group == machinery.GatewayGroupKind.Group &&
					kind == machinery.GatewayGroupKind.Kind &&
					name == gateway.GetName() &&
					namespace == gateway.GetNamespace()
			})
		},
	}
}
