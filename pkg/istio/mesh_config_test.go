//go:build unit

package istio

import (
	"fmt"
	"testing"

	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
	"google.golang.org/protobuf/types/known/structpb"
	"gotest.tools/assert"
	istiomeshv1alpha1 "istio.io/api/mesh/v1alpha1"
	istioapiv1alpha1 "istio.io/api/operator/v1alpha1"
	iopv1alpha1 "istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	istiov1alpha1 "maistra.io/istio-operator/api/v1alpha1"
	"maistra.io/istio-operator/pkg/helm"
	"sigs.k8s.io/controller-runtime/pkg/client"

	maistrav2 "github.com/kuadrant/kuadrant-operator/api/external/maistra/v2"
)

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
	assert.Equal(t, provider.GetEnvoyExtAuthzGrpc().Service, fmt.Sprintf("%s.default.svc.cluster.local", kuadrant.AuthorinoServiceName))
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

func getStubbedMeshConfigStruct() *structpb.Struct {
	return &structpb.Struct{
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
}

func TestOperatorWrapper_GetConfigObject(t *testing.T) {
	config := &iopv1alpha1.IstioOperator{}
	wrapper := NewOperatorWrapper(config)

	assert.Equal(t, wrapper.GetConfigObject(), config)
}

func TestOperatorWrapper_GetMeshConfig(t *testing.T) {
	structConfig := getStubbedMeshConfigStruct()

	config := &iopv1alpha1.IstioOperator{
		Spec: &istioapiv1alpha1.IstioOperatorSpec{
			MeshConfig: structConfig,
		},
	}
	wrapper := NewOperatorWrapper(config)

	meshConfig, err := wrapper.GetMeshConfig()
	assert.NilError(t, err)
	assert.Equal(t, meshConfig.ExtensionProviders[0].Name, "custom-authorizer")
	assert.Equal(t, meshConfig.ExtensionProviders[0].GetEnvoyExtAuthzGrpc().GetPort(), uint32(50051))
}

func TestOperatorWrapper_SetMeshConfig(t *testing.T) {
	config := &iopv1alpha1.IstioOperator{
		Spec: &istioapiv1alpha1.IstioOperatorSpec{},
	}
	wrapper := NewOperatorWrapper(config)

	stubbedMeshConfig := getStubbedMeshConfig()
	err := wrapper.SetMeshConfig(stubbedMeshConfig)
	assert.NilError(t, err)

	meshConfig, _ := wrapper.GetMeshConfig()

	assert.Equal(t, meshConfig.ExtensionProviders[0].Name, stubbedMeshConfig.ExtensionProviders[0].Name)
	assert.Equal(t, meshConfig.ExtensionProviders[0].GetEnvoyExtAuthzGrpc().GetPort(), uint32(50051))
}

func TestConfigMapWrapper_GetConfigObject(t *testing.T) {
	configMap := &corev1.ConfigMap{}
	wrapper := NewConfigMapWrapper(configMap)

	assert.Equal(t, wrapper.GetConfigObject(), configMap)
}

func TestConfigMapWrapper_GetMeshConfig(t *testing.T) {
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
	wrapper := NewConfigMapWrapper(configMap)

	meshConfig, _ := wrapper.GetMeshConfig()
	assert.Equal(t, meshConfig.ExtensionProviders[0].Name, "custom-authorizer")
	assert.Equal(t, meshConfig.ExtensionProviders[0].GetEnvoyExtAuthzGrpc().GetPort(), uint32(50051))
}

func TestConfigMapWrapper_SetMeshConfig(t *testing.T) {
	configMap := &corev1.ConfigMap{
		Data: map[string]string{
			"mesh": "",
		},
	}
	wrapper := NewConfigMapWrapper(configMap)

	stubbedMeshConfig := getStubbedMeshConfig()
	err := wrapper.SetMeshConfig(stubbedMeshConfig)
	assert.NilError(t, err)

	meshConfig, _ := wrapper.GetMeshConfig()

	assert.Equal(t, meshConfig.ExtensionProviders[0].Name, "custom-authorizer")
	assert.Equal(t, meshConfig.ExtensionProviders[0].GetEnvoyExtAuthzGrpc().GetPort(), uint32(50051))
}

func TestOSSMControlPlaneWrapper_GetConfigObject(t *testing.T) {
	ossmControlPlane := &maistrav2.ServiceMeshControlPlane{}
	wrapper := NewOSSMControlPlaneWrapper(ossmControlPlane)
	assert.Equal(t, wrapper.GetConfigObject(), ossmControlPlane)
}

func TestOSSMControlPlaneWrapper_GetMeshConfig(t *testing.T) {
	ossmControlPlane := &maistrav2.ServiceMeshControlPlane{}
	ossmMeshConfig, err := ossmMeshConfigFromStruct(getStubbedMeshConfigStruct())
	ossmControlPlane.Spec.MeshConfig = ossmMeshConfig
	assert.NilError(t, err)

	wrapper := NewOSSMControlPlaneWrapper(ossmControlPlane)
	meshConfig, _ := wrapper.GetMeshConfig()

	assert.Equal(t, meshConfig.ExtensionProviders[0].Name, "custom-authorizer")
	assert.Equal(t, meshConfig.ExtensionProviders[0].GetEnvoyExtAuthzGrpc().GetPort(), uint32(50051))

	// additional test branches for ossmMeshConfigFromStruct
	ossmMeshConfig, err = ossmMeshConfigFromStruct(nil)
	assert.NilError(t, err)
	assert.DeepEqual(t, ossmMeshConfig, &maistrav2.MeshConfig{})

	invalidStruct := &structpb.Struct{
		Fields: map[string]*structpb.Value{
			"invalid": {},
		},
	}

	ossmMeshConfig, err = ossmMeshConfigFromStruct(invalidStruct)
	assert.Check(t, err != nil)
	assert.Check(t, ossmMeshConfig == nil)
}

func TestOSSMControlPlaneWrapper_SetMeshConfig(t *testing.T) {
	ossmControlPlane := &maistrav2.ServiceMeshControlPlane{}
	wrapper := NewOSSMControlPlaneWrapper(ossmControlPlane)

	stubbedMeshConfig := getStubbedMeshConfig()
	err := wrapper.SetMeshConfig(stubbedMeshConfig)
	assert.NilError(t, err)

	meshConfig, _ := wrapper.GetMeshConfig()

	assert.Equal(t, meshConfig.ExtensionProviders[0].Name, "custom-authorizer")
	assert.Equal(t, meshConfig.ExtensionProviders[0].GetEnvoyExtAuthzGrpc().GetPort(), uint32(50051))
}

func TestSailWrapper_GetConfigObject(t *testing.T) {
	ist := &istiov1alpha1.Istio{}
	wrapper := NewSailWrapper(ist)

	assert.Equal(t, wrapper.GetConfigObject(), ist)
}

func TestSailWrapper_GetMeshConfig(t *testing.T) {
	structConfig := getStubbedMeshConfigStruct()
	values := helm.HelmValues{}
	if err := values.Set("meshConfig", structConfig.AsMap()); err != nil {
		assert.NilError(t, err)
	}
	config := &istiov1alpha1.Istio{}
	if err := config.Spec.SetValues(values); err != nil {
		assert.NilError(t, err)
	}
	wrapper := NewSailWrapper(config)

	meshConfig, err := wrapper.GetMeshConfig()
	assert.NilError(t, err)
	assert.Equal(t, meshConfig.ExtensionProviders[0].Name, "custom-authorizer")
	assert.Equal(t, meshConfig.ExtensionProviders[0].GetEnvoyExtAuthzGrpc().GetPort(), uint32(50051))
}

func TestSailWrapper_SetMeshConfig(t *testing.T) {
	config := &istiov1alpha1.Istio{}
	wrapper := NewSailWrapper(config)

	stubbedMeshConfig := getStubbedMeshConfig()
	err := wrapper.SetMeshConfig(stubbedMeshConfig)
	assert.NilError(t, err)

	meshConfig, _ := wrapper.GetMeshConfig()

	assert.Equal(t, meshConfig.ExtensionProviders[0].Name, stubbedMeshConfig.ExtensionProviders[0].Name)
	assert.Equal(t, meshConfig.ExtensionProviders[0].GetEnvoyExtAuthzGrpc().GetPort(), uint32(50051))
}
