package envoygateway

import (
	egv1alpha1 "github.com/envoyproxy/gateway/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"

	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
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
	return kuadrantgatewayapi.IsCRDInstalled(
		restMapper,
		egv1alpha1.GroupName,
		egv1alpha1.KindEnvoyPatchPolicy,
		egv1alpha1.GroupVersion.Version)
}

func IsEnvoyExtensionPolicyInstalled(restMapper meta.RESTMapper) (bool, error) {
	return kuadrantgatewayapi.IsCRDInstalled(
		restMapper,
		egv1alpha1.GroupName,
		egv1alpha1.KindEnvoyExtensionPolicy,
		egv1alpha1.GroupVersion.Version)
}

func IsEnvoyGatewaySecurityPolicyInstalled(restMapper meta.RESTMapper) (bool, error) {
	return kuadrantgatewayapi.IsCRDInstalled(
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
