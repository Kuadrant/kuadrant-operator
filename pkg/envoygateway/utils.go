package envoygateway

import (
	"encoding/json"
	"reflect"

	envoygatewayv1alpha1 "github.com/envoyproxy/gateway/api/v1alpha1"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"github.com/samber/lo"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	"github.com/kuadrant/kuadrant-operator/pkg/common"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
)

var (
	EnvoyPatchPoliciesResource     = envoygatewayv1alpha1.SchemeBuilder.GroupVersion.WithResource("envoypatchpolicies")
	EnvoyExtensionPoliciesResource = envoygatewayv1alpha1.SchemeBuilder.GroupVersion.WithResource("envoyextensionpolicies")

	EnvoyPatchPolicyGroupKind     = schema.GroupKind{Group: envoygatewayv1alpha1.GroupName, Kind: envoygatewayv1alpha1.KindEnvoyPatchPolicy}
	EnvoyExtensionPolicyGroupKind = schema.GroupKind{Group: envoygatewayv1alpha1.GroupName, Kind: envoygatewayv1alpha1.KindEnvoyExtensionPolicy}
)

func IsEnvoyPatchPolicyInstalled(restMapper meta.RESTMapper) (bool, error) {
	return utils.IsCRDInstalled(
		restMapper,
		envoygatewayv1alpha1.GroupName,
		envoygatewayv1alpha1.KindEnvoyPatchPolicy,
		envoygatewayv1alpha1.GroupVersion.Version)
}

func IsEnvoyExtensionPolicyInstalled(restMapper meta.RESTMapper) (bool, error) {
	return utils.IsCRDInstalled(
		restMapper,
		envoygatewayv1alpha1.GroupName,
		envoygatewayv1alpha1.KindEnvoyExtensionPolicy,
		envoygatewayv1alpha1.GroupVersion.Version)
}

func IsEnvoyGatewaySecurityPolicyInstalled(restMapper meta.RESTMapper) (bool, error) {
	return utils.IsCRDInstalled(
		restMapper,
		envoygatewayv1alpha1.GroupName,
		envoygatewayv1alpha1.KindSecurityPolicy,
		envoygatewayv1alpha1.GroupVersion.Version)
}

func IsEnvoyGatewayInstalled(restMapper meta.RESTMapper) (bool, error) {
	ok, err := IsEnvoyGatewaySecurityPolicyInstalled(restMapper)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}

	ok, err = IsEnvoyExtensionPolicyInstalled(restMapper)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}

	ok, err = IsEnvoyPatchPolicyInstalled(restMapper)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, nil
	}

	// EnvoyGateway found
	return true, nil
}

func LinkGatewayToEnvoyPatchPolicy(objs controller.Store) machinery.LinkFunc {
	gateways := lo.Map(objs.FilterByGroupKind(machinery.GatewayGroupKind), func(obj controller.Object, _ int) machinery.Object {
		return &machinery.Gateway{Gateway: obj.(*gatewayapiv1.Gateway)}
	})

	return machinery.LinkFunc{
		From: machinery.GatewayGroupKind,
		To:   EnvoyPatchPolicyGroupKind,
		Func: func(child machinery.Object) []machinery.Object {
			envoyPatchPolicy := child.(*controller.RuntimeObject).Object.(*envoygatewayv1alpha1.EnvoyPatchPolicy)
			namespace := envoyPatchPolicy.GetNamespace()
			targetRef := envoyPatchPolicy.Spec.TargetRef
			group := string(targetRef.Group)
			if group == "" {
				group = machinery.GatewayGroupKind.Group
			}
			kind := string(targetRef.Kind)
			if kind == "" {
				kind = machinery.GatewayGroupKind.Kind
			}
			name := string(targetRef.Name)
			if group != machinery.GatewayGroupKind.Group || kind != machinery.GatewayGroupKind.Kind || name == "" {
				return []machinery.Object{}
			}
			return lo.Filter(gateways, func(gateway machinery.Object, _ int) bool {
				return gateway.GetName() == name && gateway.GetNamespace() == namespace
			})
		},
	}
}

func LinkGatewayToEnvoyExtensionPolicy(objs controller.Store) machinery.LinkFunc {
	gateways := lo.Map(objs.FilterByGroupKind(machinery.GatewayGroupKind), func(obj controller.Object, _ int) machinery.Object {
		return &machinery.Gateway{Gateway: obj.(*gatewayapiv1.Gateway)}
	})

	return machinery.LinkFunc{
		From: machinery.GatewayGroupKind,
		To:   EnvoyExtensionPolicyGroupKind,
		Func: func(child machinery.Object) []machinery.Object {
			envoyExtensionPolicy := child.(*controller.RuntimeObject).Object.(*envoygatewayv1alpha1.EnvoyExtensionPolicy)
			return lo.Filter(gateways, func(gateway machinery.Object, _ int) bool {
				if gateway.GetNamespace() != envoyExtensionPolicy.GetNamespace() {
					return false
				}
				return lo.SomeBy(envoyExtensionPolicy.Spec.TargetRefs, func(targetRef gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName) bool {
					group := string(targetRef.Group)
					if group == "" {
						group = machinery.GatewayGroupKind.Group
					}
					kind := string(targetRef.Kind)
					if kind == "" {
						kind = machinery.GatewayGroupKind.Kind
					}
					name := string(targetRef.Name)
					if name == "" {
						return false
					}
					return group == machinery.GatewayGroupKind.Group &&
						kind == machinery.GatewayGroupKind.Kind &&
						name == gateway.GetName()
				})
			})
		},
	}
}

// BuildEnvoyPatchPolicyClusterPatch returns an envoy config patch that adds a cluster to the gateway.
func BuildEnvoyPatchPolicyClusterPatch(host string, port int, clusterPatchBuilder func(string, int) map[string]any) ([]envoygatewayv1alpha1.EnvoyJSONPatchConfig, error) {
	patchRaw, _ := json.Marshal(clusterPatchBuilder(host, port))
	patch := &apiextensionsv1.JSON{}
	if err := patch.UnmarshalJSON(patchRaw); err != nil {
		return nil, err
	}

	return []envoygatewayv1alpha1.EnvoyJSONPatchConfig{
		{
			Type: envoygatewayv1alpha1.ClusterEnvoyResourceType,
			Name: common.KuadrantRateLimitClusterName,
			Operation: envoygatewayv1alpha1.JSONPatchOperation{
				Op:    envoygatewayv1alpha1.JSONPatchOperationType("add"),
				Path:  "",
				Value: patch,
			},
		},
	}, nil
}

func EqualEnvoyPatchPolicies(a, b *envoygatewayv1alpha1.EnvoyPatchPolicy) bool {
	if a.Spec.Type != b.Spec.Type || a.Spec.Priority != b.Spec.Priority || !reflect.DeepEqual(a.Spec.TargetRef, b.Spec.TargetRef) {
		return false
	}

	aJSONPatches := a.Spec.JSONPatches
	bJSONPatches := b.Spec.JSONPatches
	if len(aJSONPatches) != len(bJSONPatches) {
		return false
	}
	return lo.EveryBy(aJSONPatches, func(aJSONPatch envoygatewayv1alpha1.EnvoyJSONPatchConfig) bool {
		return lo.SomeBy(bJSONPatches, func(bJSONPatch envoygatewayv1alpha1.EnvoyJSONPatchConfig) bool {
			return aJSONPatch.Type == bJSONPatch.Type && aJSONPatch.Name == bJSONPatch.Name && reflect.DeepEqual(aJSONPatch.Operation, bJSONPatch.Operation)
		})
	})
}
