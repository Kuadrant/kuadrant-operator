package envoygateway

import (
	egv1alpha1 "github.com/envoyproxy/gateway/api/v1alpha1"
	"github.com/samber/lo"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"

	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
)

var (
	EnvoyPatchPoliciesResource     = egv1alpha1.SchemeBuilder.GroupVersion.WithResource("envoypatchpolicies")
	EnvoyExtensionPoliciesResource = egv1alpha1.SchemeBuilder.GroupVersion.WithResource("envoyextensionpolicies")
	SecurityPoliciesResource       = egv1alpha1.SchemeBuilder.GroupVersion.WithResource("securitypolicies")

	EnvoyPatchPolicyGroupKind     = schema.GroupKind{Group: egv1alpha1.GroupName, Kind: egv1alpha1.KindEnvoyPatchPolicy}
	EnvoyExtensionPolicyGroupKind = schema.GroupKind{Group: egv1alpha1.GroupName, Kind: egv1alpha1.KindEnvoyExtensionPolicy}
	SecurityPolicyGroupKind       = schema.GroupKind{Group: egv1alpha1.GroupName, Kind: egv1alpha1.KindSecurityPolicy}
)

func IsEnvoyPatchPolicyInstalled(restMapper meta.RESTMapper) (bool, error) {
	return utils.IsCRDInstalled(
		restMapper,
		egv1alpha1.GroupName,
		egv1alpha1.KindEnvoyPatchPolicy,
		egv1alpha1.GroupVersion.Version)
}

func IsEnvoyExtensionPolicyInstalled(restMapper meta.RESTMapper) (bool, error) {
	return utils.IsCRDInstalled(
		restMapper,
		egv1alpha1.GroupName,
		egv1alpha1.KindEnvoyExtensionPolicy,
		egv1alpha1.GroupVersion.Version)
}

func IsEnvoyGatewaySecurityPolicyInstalled(restMapper meta.RESTMapper) (bool, error) {
	return utils.IsCRDInstalled(
		restMapper,
		egv1alpha1.GroupName,
		egv1alpha1.KindSecurityPolicy,
		egv1alpha1.GroupVersion.Version)
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
			envoyPatchPolicy := child.(*controller.RuntimeObject).Object.(*egv1alpha1.EnvoyPatchPolicy)
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
