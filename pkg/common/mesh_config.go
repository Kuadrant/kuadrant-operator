package common

import (
	"fmt"

	istiomeshv1alpha1 "istio.io/api/mesh/v1alpha1"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	ExtAuthorizerName = "kuadrant-authorization"
)

type Authorizer interface {
	GetExtensionProvider() *istiomeshv1alpha1.MeshConfig_ExtensionProvider
}

type ConfigWrapper interface {
	GetConfigObject() client.Object
	GetMeshConfig() (*istiomeshv1alpha1.MeshConfig, error)
	SetMeshConfig(*istiomeshv1alpha1.MeshConfig) error
}

type KuadrantAuthorizer struct {
	extensionProvider *istiomeshv1alpha1.MeshConfig_ExtensionProvider
}

// NewKuadrantAuthorizer Creates a new KuadrantAuthorizer
func NewKuadrantAuthorizer(namespace string) *KuadrantAuthorizer {
	return &KuadrantAuthorizer{
		extensionProvider: createKuadrantAuthorizer(namespace),
	}
}

// GetExtensionProvider Returns the Istio MeshConfig ExtensionProvider for Kuadrant
func (k *KuadrantAuthorizer) GetExtensionProvider() *istiomeshv1alpha1.MeshConfig_ExtensionProvider {
	return k.extensionProvider
}

// createKuadrantAuthorizer Creates the Istio MeshConfig ExtensionProvider for Kuadrant
func createKuadrantAuthorizer(namespace string) *istiomeshv1alpha1.MeshConfig_ExtensionProvider {
	envoyExtAuthGRPC := &istiomeshv1alpha1.MeshConfig_ExtensionProvider_EnvoyExtAuthzGrpc{
		EnvoyExtAuthzGrpc: &istiomeshv1alpha1.MeshConfig_ExtensionProvider_EnvoyExternalAuthorizationGrpcProvider{
			Port:    50051,
			Service: fmt.Sprintf("authorino-authorino-authorization.%s.svc.cluster.local", namespace),
		},
	}
	return &istiomeshv1alpha1.MeshConfig_ExtensionProvider{
		Name:     ExtAuthorizerName,
		Provider: envoyExtAuthGRPC,
	}
}

// HasKuadrantAuthorizer returns true if the IstioOperator has the Kuadrant ExtensionProvider
func HasKuadrantAuthorizer(configWrapper ConfigWrapper, authorizer KuadrantAuthorizer) (bool, error) {
	config, err := configWrapper.GetMeshConfig()
	if err != nil {
		return false, err
	}
	return hasExtensionProvider(authorizer.GetExtensionProvider(), extensionProvidersFromMeshConfig(config)), nil
}

// RegisterKuadrantAuthorizer adds the Kuadrant ExtensionProvider to the IstioOperator
func RegisterKuadrantAuthorizer(configWrapper ConfigWrapper, authorizer Authorizer) error {
	config, err := configWrapper.GetMeshConfig()
	if err != nil {
		return err
	}
	if !hasExtensionProvider(authorizer.GetExtensionProvider(), extensionProvidersFromMeshConfig(config)) {
		config.ExtensionProviders = append(config.ExtensionProviders, authorizer.GetExtensionProvider())
		if err = configWrapper.SetMeshConfig(config); err != nil {
			return err
		}
	}
	return nil
}

// UnregisterKuadrantAuthorizer removes the Kuadrant ExtensionProvider from the IstioOperator
func UnregisterKuadrantAuthorizer(configWrapper ConfigWrapper, authorizer Authorizer) error {
	config, err := configWrapper.GetMeshConfig()
	if err != nil {
		return err
	}
	if hasExtensionProvider(authorizer.GetExtensionProvider(), extensionProvidersFromMeshConfig(config)) {
		config.ExtensionProviders = removeExtensionProvider(authorizer.GetExtensionProvider(), extensionProvidersFromMeshConfig(config))
		if err = configWrapper.SetMeshConfig(config); err != nil {
			return err
		}
	}
	return nil
}

func extensionProvidersFromMeshConfig(config *istiomeshv1alpha1.MeshConfig) (extensionProviders []*istiomeshv1alpha1.MeshConfig_ExtensionProvider) {
	extensionProviders = config.ExtensionProviders
	if len(extensionProviders) == 0 {
		extensionProviders = make([]*istiomeshv1alpha1.MeshConfig_ExtensionProvider, 0)
	}
	return
}

// hasExtensionProvider returns true if the MeshConfig has an ExtensionProvider with the given name
func hasExtensionProvider(provider *istiomeshv1alpha1.MeshConfig_ExtensionProvider, extensionProviders []*istiomeshv1alpha1.MeshConfig_ExtensionProvider) bool {
	for _, extensionProvider := range extensionProviders {
		if extensionProvider.Name == provider.Name {
			return true
		}
	}
	return false
}

func removeExtensionProvider(provider *istiomeshv1alpha1.MeshConfig_ExtensionProvider, providers []*istiomeshv1alpha1.MeshConfig_ExtensionProvider) []*istiomeshv1alpha1.MeshConfig_ExtensionProvider {
	for i, extensionProvider := range providers {
		if extensionProvider.Name == provider.Name {
			return append(providers[:i], providers[i+1:]...)
		}
	}
	return providers
}
