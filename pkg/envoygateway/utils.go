package envoygateway

import (
	egv1alpha1 "github.com/envoyproxy/gateway/api/v1alpha1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func IsEnvoyExtensionPolicyInstalled(restMapper meta.RESTMapper) (bool, error) {
	_, err := restMapper.RESTMapping(
		schema.GroupKind{Group: egv1alpha1.GroupName, Kind: egv1alpha1.KindEnvoyExtensionPolicy},
		egv1alpha1.GroupVersion.Version)

	if err == nil {
		return true, nil
	}

	if meta.IsNoMatchError(err) {
		return false, nil
	}

	return false, err
}

func IsEnvoyGatewaySecurityPolicyInstalled(restMapper meta.RESTMapper) (bool, error) {
	_, err := restMapper.RESTMapping(
		schema.GroupKind{Group: egv1alpha1.GroupName, Kind: egv1alpha1.KindSecurityPolicy},
		egv1alpha1.GroupVersion.Version)

	if err == nil {
		return true, nil
	}

	if meta.IsNoMatchError(err) {
		return false, nil
	}

	return false, err
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

	// EnvoyGateway found
	return true, nil
}
