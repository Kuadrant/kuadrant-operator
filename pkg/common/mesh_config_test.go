//go:build unit
// +build unit

package common

import (
	"testing"

	"google.golang.org/protobuf/types/known/structpb"
	"gotest.tools/assert"
	istiomeshv1alpha1 "istio.io/api/mesh/v1alpha1"
	istioapiv1alpha1 "istio.io/api/operator/v1alpha1"
	iopv1alpha1 "istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	corev1 "k8s.io/api/core/v1"
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

func TestMeshConfigFromStruct(t *testing.T) {
	expectedConfig := getStubbedMeshConfig()

	config := &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"extensionProviders": {
				Kind: &structpb.Value_ListValue{
					ListValue: &structpb.ListValue{
						Values: []*structpb.Value{
							{
								Kind: &structpb.Value_StructValue{
									StructValue: &structpb.Struct{
										Fields: map[string]*structpb.Value{
											"name": {
												Kind: &structpb.Value_StringValue{
													StringValue: "custom-authorizer",
												},
											},
											"envoyExtAuthzGrpc": {
												Kind: &structpb.Value_StructValue{
													StructValue: &structpb.Struct{
														Fields: map[string]*structpb.Value{
															"port": {
																Kind: &structpb.Value_NumberValue{
																	NumberValue: 50051,
																},
															},
															"service": {
																Kind: &structpb.Value_StringValue{
																	StringValue: "custom-authorizer.default.svc.cluster.local",
																},
															},
														},
													},
												},
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	meshConfig, _ := MeshConfigFromStruct(config)

	assert.Equal(t, meshConfig.ExtensionProviders[0].Name, expectedConfig.ExtensionProviders[0].Name)
	assert.Equal(t, meshConfig.ExtensionProviders[0].GetEnvoyExtAuthzGrpc().Service, expectedConfig.ExtensionProviders[0].GetEnvoyExtAuthzGrpc().Service)
	assert.Equal(t, meshConfig.ExtensionProviders[0].GetEnvoyExtAuthzGrpc().Port, expectedConfig.ExtensionProviders[0].GetEnvoyExtAuthzGrpc().Port)
}

func TestMeshConfigFromString(t *testing.T) {
	expectedConfig := getStubbedMeshConfig()

	config := `
extensionProviders:
- name: "custom-authorizer"
  envoyExtAuthzGrpc:
    service: "custom-authorizer.default.svc.cluster.local"
    port: "50051"
`

	meshConfig, _ := MeshConfigFromString(config)

	assert.Equal(t, meshConfig.ExtensionProviders[0].Name, expectedConfig.ExtensionProviders[0].Name)
	assert.Equal(t, meshConfig.ExtensionProviders[0].GetEnvoyExtAuthzGrpc().Service, expectedConfig.ExtensionProviders[0].GetEnvoyExtAuthzGrpc().Service)
	assert.Equal(t, meshConfig.ExtensionProviders[0].GetEnvoyExtAuthzGrpc().Port, expectedConfig.ExtensionProviders[0].GetEnvoyExtAuthzGrpc().Port)

}

func TestMeshConfigToStruct(t *testing.T) {
	config := getStubbedMeshConfig()

	meshConfig, _ := MeshConfigToStruct(config)

	assert.Equal(t, meshConfig.Fields["extensionProviders"].GetListValue().Values[0].GetStructValue().Fields["name"].GetStringValue(), "custom-authorizer")
	assert.Equal(t, meshConfig.Fields["extensionProviders"].GetListValue().Values[0].GetStructValue().Fields["envoyExtAuthzGrpc"].GetStructValue().Fields["service"].GetStringValue(), "custom-authorizer.default.svc.cluster.local")
	assert.Equal(t, meshConfig.Fields["extensionProviders"].GetListValue().Values[0].GetStructValue().Fields["envoyExtAuthzGrpc"].GetStructValue().Fields["port"].GetNumberValue(), float64(50051))
}

func TestExtensionProvidersFromMeshConfig(t *testing.T) {
	config := getStubbedMeshConfig()

	extensionProviders := ExtensionProvidersFromMeshConfig(config)

	assert.Equal(t, extensionProviders[0].Name, "custom-authorizer")
	assert.Equal(t, extensionProviders[0].GetEnvoyExtAuthzGrpc().Service, "custom-authorizer.default.svc.cluster.local")
	assert.Equal(t, extensionProviders[0].GetEnvoyExtAuthzGrpc().Port, uint32(50051))
}

func TestExtensionProvidersFromMeshConfigWithEmptyConfig(t *testing.T) {
	config := &istiomeshv1alpha1.MeshConfig{}

	extensionProviders := ExtensionProvidersFromMeshConfig(config)

	assert.Equal(t, len(extensionProviders), 0)
}

func TestHasKuadrantAuthorizer(t *testing.T) {
	configWithNoKuadrantAuth := getStubbedMeshConfig().ExtensionProviders

	configWithKuadrantAuth := make([]*istiomeshv1alpha1.MeshConfig_ExtensionProvider, 0)
	kuadrantAuthProvider := CreateKuadrantAuthorizer("kuadrant-system")
	configWithKuadrantAuth = append(configWithKuadrantAuth, kuadrantAuthProvider)

	assert.Equal(t, HasKuadrantAuthorizer(configWithNoKuadrantAuth), false)
	assert.Equal(t, HasKuadrantAuthorizer(configWithKuadrantAuth), true)
}

func TestCreateKuadrantAuthorizer(t *testing.T) {
	authorizer := CreateKuadrantAuthorizer("kuadrant-system")

	assert.Equal(t, authorizer.Name, "kuadrant-authorization")
	assert.Equal(t, authorizer.GetEnvoyExtAuthzGrpc().Service, "authorino-authorino-authorization.kuadrant-system.svc.cluster.local")
	assert.Equal(t, authorizer.GetEnvoyExtAuthzGrpc().Port, uint32(50051))
}

func TestRemoveKuadrantAuthorizerFromList(t *testing.T) {
	providers := getStubbedMeshConfig().ExtensionProviders
	providers = append(providers, CreateKuadrantAuthorizer("kuadrant-system"))
	providers = RemoveKuadrantAuthorizerFromList(providers)

	assert.Equal(t, len(providers), 1)
	assert.Equal(t, providers[0].Name, "custom-authorizer")
}

func TestUpdateMeshConfig(t *testing.T) {
	t.Run("TestUpdateMeshConfigWithConfigMap", func(t *testing.T) {
		configMap := &corev1.ConfigMap{
			Data: map[string]string{
				"mesh": `
extensionProviders:
- name: "custom-authorizer"
  envoyExtAuthzGrpc:
    service: "custom-authorizer.default.svc.cluster.local"
    port: "50051"
`,
			},
		}
		updated, err := UpdateMeshConfig(configMap, func(meshConfig *istiomeshv1alpha1.MeshConfig) bool {
			meshConfig.ExtensionProviders = append(meshConfig.ExtensionProviders, CreateKuadrantAuthorizer("kuadrant-system"))
			return true
		})

		meshConfig, _ := MeshConfigFromString(configMap.Data["mesh"])
		assert.NilError(t, err)
		assert.Equal(t, updated, true)
		assert.Equal(t, len(meshConfig.ExtensionProviders), 2)
	})
	t.Run("TestUpdateMeshConfigWithIstioOperator", func(t *testing.T) {
		meshConfig, _ := MeshConfigToStruct(getStubbedMeshConfig())
		istioOperator := &iopv1alpha1.IstioOperator{
			Spec: &istioapiv1alpha1.IstioOperatorSpec{
				MeshConfig: meshConfig,
			},
		}
		updated, err := UpdateMeshConfig(istioOperator, func(meshConfig *istiomeshv1alpha1.MeshConfig) bool {
			meshConfig.ExtensionProviders = append(meshConfig.ExtensionProviders, CreateKuadrantAuthorizer("kuadrant-system"))
			return true
		})
		config := istioOperator.Spec.MeshConfig
		assert.NilError(t, err)
		assert.Equal(t, updated, true)
		assert.Equal(t, len(config.Fields["extensionProviders"].GetListValue().Values), 2)
	})
}
