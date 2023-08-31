//go:build unit

package common

import (
	"testing"

	"gotest.tools/assert"
	istiomeshv1alpha1 "istio.io/api/mesh/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func getStubbedMeshConfig() *istiomeshv1alpha1.MeshConfig {
	providers := make([]*istiomeshv1alpha1.MeshConfig_ExtensionProvider, 0)
	provider := &istiomeshv1alpha1.MeshConfig_ExtensionProvider{
		Name: "custom-authorizer",
		Provider: &istiomeshv1alpha1.MeshConfig_ExtensionProvider_EnvoyExtAuthzGrpc{
			EnvoyExtAuthzGrpc: &istiomeshv1alpha1.MeshConfig_ExtensionProvider_EnvoyExternalAuthorizationGrpcProvider{
				Port:    50051,
				Service: "custom-authorizer.default.svc.cluster.local",
			},
		},
	}
	providers = append(providers, provider)
	return &istiomeshv1alpha1.MeshConfig{
		ExtensionProviders: providers,
	}
}

type stubbedConfigWrapper struct {
	istioMeshConfig *istiomeshv1alpha1.MeshConfig
}

func (c *stubbedConfigWrapper) SetMeshConfig(config *istiomeshv1alpha1.MeshConfig) error {
	c.istioMeshConfig = config
	return nil
}

func (c *stubbedConfigWrapper) GetMeshConfig() (*istiomeshv1alpha1.MeshConfig, error) {
	return c.istioMeshConfig, nil
}

func (c *stubbedConfigWrapper) GetConfigObject() client.Object {
	return nil
}

func TestKuadrantAuthorizer_GetExtensionProvider(t *testing.T) {
	authorizer := NewKuadrantAuthorizer("default")
	provider := authorizer.GetExtensionProvider()

	assert.Equal(t, provider.Name, ExtAuthorizerName)
	assert.Equal(t, provider.GetEnvoyExtAuthzGrpc().Service, "authorino-authorino-authorization.default.svc.cluster.local")
}

func TestHasKuadrantAuthorizer(t *testing.T) {
	authorizer := NewKuadrantAuthorizer("default")
	configWrapper := &stubbedConfigWrapper{getStubbedMeshConfig()}

	hasAuthorizer, err := HasKuadrantAuthorizer(configWrapper, *authorizer)

	assert.NilError(t, err)
	assert.Equal(t, hasAuthorizer, false)

	configWrapper.istioMeshConfig.ExtensionProviders = append(configWrapper.istioMeshConfig.ExtensionProviders, authorizer.GetExtensionProvider())
	hasAuthorizer, err = HasKuadrantAuthorizer(configWrapper, *authorizer)
	assert.NilError(t, err)
	assert.Equal(t, hasAuthorizer, true)
}

func TestRegisterKuadrantAuthorizer(t *testing.T) {
	authorizer := NewKuadrantAuthorizer("default")
	configWrapper := &stubbedConfigWrapper{getStubbedMeshConfig()}

	err := RegisterKuadrantAuthorizer(configWrapper, authorizer)
	assert.NilError(t, err)

	meshConfig, _ := configWrapper.GetMeshConfig()
	assert.Equal(t, meshConfig.ExtensionProviders[1].Name, "kuadrant-authorization")
}

func TestUnregisterKuadrantAuthorizer(t *testing.T) {
	authorizer := NewKuadrantAuthorizer("default")
	configWrapper := &stubbedConfigWrapper{getStubbedMeshConfig()}

	err := RegisterKuadrantAuthorizer(configWrapper, authorizer)
	assert.NilError(t, err)
	assert.Equal(t, len(configWrapper.istioMeshConfig.ExtensionProviders), 2)

	err = UnregisterKuadrantAuthorizer(configWrapper, authorizer)
	assert.NilError(t, err)
	assert.Equal(t, len(configWrapper.istioMeshConfig.ExtensionProviders), 1)

	meshConfig, _ := configWrapper.GetMeshConfig()
	assert.Equal(t, meshConfig.GetExtensionProviders()[0].Name, "custom-authorizer")
}
