//go:build unit
// +build unit

package istio

import (
	"testing"

	maistrav1 "github.com/kuadrant/kuadrant-operator/api/external/maistra/v1"
	maistrav2 "github.com/kuadrant/kuadrant-operator/api/external/maistra/v2"
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
	ossmControlPlane.Spec.TechPreview = maistrav1.NewHelmValues(nil)
	ossmControlPlane.Spec.TechPreview.SetField("meshConfig", getStubbedMeshConfigStruct().AsMap())

	wrapper := NewOSSMControlPlaneWrapper(ossmControlPlane)
	meshConfig, _ := wrapper.GetMeshConfig()

	assert.Equal(t, meshConfig.ExtensionProviders[0].Name, "custom-authorizer")
	assert.Equal(t, meshConfig.ExtensionProviders[0].GetEnvoyExtAuthzGrpc().GetPort(), uint32(50051))
}

func TestOSSMControlPlaneWrapper_SetMeshConfig(t *testing.T) {
	ossmControlPlane := &maistrav2.ServiceMeshControlPlane{}
	ossmControlPlane.Spec.TechPreview = maistrav1.NewHelmValues(nil)
	emptyConfig := &structpb.Struct{}
	ossmControlPlane.Spec.TechPreview.SetField("meshConfig", emptyConfig.AsMap())

	wrapper := NewOSSMControlPlaneWrapper(ossmControlPlane)

	stubbedMeshConfig := getStubbedMeshConfig()
	err := wrapper.SetMeshConfig(stubbedMeshConfig)
	assert.NilError(t, err)

	meshConfig, _ := wrapper.GetMeshConfig()

	assert.Equal(t, meshConfig.ExtensionProviders[0].Name, "custom-authorizer")
	assert.Equal(t, meshConfig.ExtensionProviders[0].GetEnvoyExtAuthzGrpc().GetPort(), uint32(50051))
}
