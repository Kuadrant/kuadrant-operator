package common

import (
	"fmt"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/structpb"
	istiomeshv1alpha1 "istio.io/api/mesh/v1alpha1"
	istioapiv1alpha1 "istio.io/api/operator/v1alpha1"
	iopv1alpha1 "istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/pkg/util/protomarshal"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	ExtAuthorizerName = "kuadrant-authorization"
)

// MeshConfigFromStruct Builds the Istio/OSSM MeshConfig from a compatible structure:
//
//	meshConfig:
//	  extensionProviders:
//	    - envoyExtAuthzGrpc:
//	        port: <port>
//	        service: <authorino-service>
//	      name: kuadrant-authorization
func MeshConfigFromStruct(structure *structpb.Struct) (*istiomeshv1alpha1.MeshConfig, error) {
	if structure == nil {
		return &istiomeshv1alpha1.MeshConfig{}, nil
	}

	meshConfigJSON, err := structure.MarshalJSON()
	if err != nil {
		return nil, err
	}
	meshConfig := &istiomeshv1alpha1.MeshConfig{}
	// istiomeshv1alpha1.MeshConfig doesn't implement JSON/Yaml marshalling, only protobuf
	if err = protojson.Unmarshal(meshConfigJSON, meshConfig); err != nil {
		return nil, err
	}

	return meshConfig, nil
}

// MeshConfigFromString returns the Istio MeshConfig from a ConfigMap
func MeshConfigFromString(config string) (*istiomeshv1alpha1.MeshConfig, error) {
	meshConfig := &istiomeshv1alpha1.MeshConfig{}
	err := protomarshal.ApplyYAML(config, meshConfig)
	if err != nil {
		return nil, err
	}
	return meshConfig, nil
}

// meshConfigToString returns the Istio MeshConfig as a string
func meshConfigToString(config *istiomeshv1alpha1.MeshConfig) (string, error) {
	configString, err := protomarshal.ToYAML(config)
	if err != nil {
		return "", err
	}
	return configString, nil
}

func MeshConfigToStruct(config *istiomeshv1alpha1.MeshConfig) (*structpb.Struct, error) {
	configJSON, err := protojson.Marshal(config)
	if err != nil {
		return nil, err
	}
	configStruct := &structpb.Struct{}

	if err = configStruct.UnmarshalJSON(configJSON); err != nil {
		return nil, err
	}
	return configStruct, nil
}

func ExtensionProvidersFromMeshConfig(config *istiomeshv1alpha1.MeshConfig) (extensionProviders []*istiomeshv1alpha1.MeshConfig_ExtensionProvider) {
	extensionProviders = config.ExtensionProviders
	if len(extensionProviders) == 0 {
		extensionProviders = make([]*istiomeshv1alpha1.MeshConfig_ExtensionProvider, 0)
	}
	return
}

func UpdateMeshConfig(configObject client.Object, updateFunc func(meshConfig *istiomeshv1alpha1.MeshConfig) bool) (bool, error) {
	meshConfig, err := getMeshConfig(configObject)
	if err != nil {
		return false, err
	}
	if updateFunc(meshConfig) {
		switch config := configObject.(type) {
		case *iopv1alpha1.IstioOperator:
			meshConfigStruct, err := MeshConfigToStruct(meshConfig)
			if err != nil {
				return false, err
			}
			config.Spec.MeshConfig = meshConfigStruct
			return true, nil
		case *corev1.ConfigMap:
			var err error
			config.Data["mesh"], err = meshConfigToString(meshConfig)
			if err != nil {
				return false, err
			}
			return true, nil
		default:
			return false, fmt.Errorf("unsupported configObject type: %T", config)
		}
	}
	return false, nil
}

func getMeshConfig(configObject client.Object) (*istiomeshv1alpha1.MeshConfig, error) {
	switch config := configObject.(type) {
	case *iopv1alpha1.IstioOperator:
		if config.Spec == nil {
			config.Spec = &istioapiv1alpha1.IstioOperatorSpec{}
		}
		return MeshConfigFromStruct(config.Spec.MeshConfig)
	case *corev1.ConfigMap:
		return MeshConfigFromString(config.Data["mesh"])
	default:
		return nil, fmt.Errorf("unsupported configObject type: %T", config)
	}
}

func RemoveKuadrantAuthorizerFromList(extensionProviders []*istiomeshv1alpha1.MeshConfig_ExtensionProvider) []*istiomeshv1alpha1.MeshConfig_ExtensionProvider {
	for i, extProvider := range extensionProviders {
		if extProvider.Name == ExtAuthorizerName {
			extensionProviders = append(extensionProviders[:i], extensionProviders[i+1:]...)
			break
		}
	}
	return extensionProviders
}

func HasKuadrantAuthorizer(extensionProviders []*istiomeshv1alpha1.MeshConfig_ExtensionProvider) bool {
	for _, extensionProvider := range extensionProviders {
		if extensionProvider.Name == ExtAuthorizerName {
			return true
		}
	}

	return false
}

func CreateKuadrantAuthorizer(namespace string) *istiomeshv1alpha1.MeshConfig_ExtensionProvider {
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
