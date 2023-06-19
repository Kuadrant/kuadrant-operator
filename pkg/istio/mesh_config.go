package istio

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

// ConfigWrapper wraps the IstioOperator CRD or ConfigMap
type ConfigWrapper interface {
	GetConfigObject() client.Object
	GetConfig() (*istiomeshv1alpha1.MeshConfig, error)
	UpdateConfig(updateFunc func(meshConfig *istiomeshv1alpha1.MeshConfig) bool) (bool, error)
}

// OperatorWrapper wraps the IstioOperator CRD
type OperatorWrapper struct {
	config *iopv1alpha1.IstioOperator
}

// NewOperatorWrapper creates a new IstioOperatorWrapper
func NewOperatorWrapper(config *iopv1alpha1.IstioOperator) *OperatorWrapper {
	return &OperatorWrapper{config: config}
}

// GetConfigObject returns the IstioOperator CRD
func (w *OperatorWrapper) GetConfigObject() client.Object {
	return w.config
}

// GetConfig returns the IstioOperator MeshConfig
func (w *OperatorWrapper) GetConfig() (*istiomeshv1alpha1.MeshConfig, error) {
	if w.config.Spec == nil {
		w.config.Spec = &istioapiv1alpha1.IstioOperatorSpec{}
	}
	return MeshConfigFromStruct(w.config.Spec.MeshConfig)
}

// UpdateConfig updates the IstioOperator with the new MeshConfig and returns true if the MeshConfig was updated
func (w *OperatorWrapper) UpdateConfig(updateFunc func(meshConfig *istiomeshv1alpha1.MeshConfig) bool) (bool, error) {
	config, err := w.GetConfig()
	if err != nil {
		return false, err
	}
	if updateFunc(config) {
		meshConfigStruct, err := MeshConfigToStruct(config)
		if err != nil {
			return false, err
		}
		w.config.Spec.MeshConfig = meshConfigStruct
		return true, nil
	}
	return false, nil
}

// ConfigMapWrapper wraps the ConfigMap holding the Istio MeshConfig
type ConfigMapWrapper struct {
	config *corev1.ConfigMap
}

// NewConfigMapWrapper creates a new ConfigMapWrapper
func NewConfigMapWrapper(config *corev1.ConfigMap) *ConfigMapWrapper {
	return &ConfigMapWrapper{config: config}
}

// GetConfigObject returns the ConfigMap
func (w *ConfigMapWrapper) GetConfigObject() client.Object {
	return w.config
}

// GetConfig returns the MeshConfig from the ConfigMap
func (w *ConfigMapWrapper) GetConfig() (*istiomeshv1alpha1.MeshConfig, error) {
	meshConfigString, ok := w.config.Data["mesh"]
	if !ok {
		return nil, fmt.Errorf("mesh config not found in ConfigMap")
	}
	return MeshConfigFromString(meshConfigString)
}

// UpdateConfig updates the ConfigMap with the new MeshConfig and returns true if the ConfigMap was updated
func (w *ConfigMapWrapper) UpdateConfig(updateFunc func(meshConfig *istiomeshv1alpha1.MeshConfig) bool) (bool, error) {
	config, err := w.GetConfig()
	if err != nil {
		return false, err
	}

	if updateFunc(config) {
		meshString, err := meshConfigToString(config)
		if err != nil {
			return false, err
		}
		w.config.Data["mesh"] = meshString
		return true, nil
	}
	return false, nil
}

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

// MeshConfigToStruct Marshals the Istio MeshConfig into a struct
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

// ExtensionProvidersFromMeshConfig Returns the Istio MeshConfig ExtensionProviders
func ExtensionProvidersFromMeshConfig(config *istiomeshv1alpha1.MeshConfig) (extensionProviders []*istiomeshv1alpha1.MeshConfig_ExtensionProvider) {
	extensionProviders = config.ExtensionProviders
	if len(extensionProviders) == 0 {
		extensionProviders = make([]*istiomeshv1alpha1.MeshConfig_ExtensionProvider, 0)
	}
	return
}

// HasKuadrantAuthorizer Checks if the Istio MeshConfig has the ExtensionProvider for Kuadrant
func HasKuadrantAuthorizer(extensionProviders []*istiomeshv1alpha1.MeshConfig_ExtensionProvider) bool {
	for _, extensionProvider := range extensionProviders {
		if extensionProvider.Name == ExtAuthorizerName {
			return true
		}
	}

	return false
}

// RemoveKuadrantAuthorizerFromConfig Removes the Istio MeshConfig ExtensionProvider for Kuadrant
func RemoveKuadrantAuthorizerFromConfig(config *istiomeshv1alpha1.MeshConfig) bool {
	for i, extProvider := range config.ExtensionProviders {
		if extProvider.Name == ExtAuthorizerName {
			fmt.Println("Removing Kuadrant Authorizer from MeshConfig", config.ExtensionProviders)
			config.ExtensionProviders = append(config.ExtensionProviders[:i], config.ExtensionProviders[i+1:]...)
			fmt.Println("Removing Kuadrant Authorizer from MeshConfig", config.ExtensionProviders)
			return true
		}
	}
	return false
}

// AddKuadrantAuthorizerToConfig Adds the Istio MeshConfig ExtensionProvider for Kuadrant
func AddKuadrantAuthorizerToConfig(namespace string) func(config *istiomeshv1alpha1.MeshConfig) bool {
	return func(config *istiomeshv1alpha1.MeshConfig) bool {
		if HasKuadrantAuthorizer(config.ExtensionProviders) {
			return false
		}
		config.ExtensionProviders = append(config.ExtensionProviders, CreateKuadrantAuthorizer(namespace))
		return true
	}
}

// CreateKuadrantAuthorizer Creates the Istio MeshConfig ExtensionProvider for Kuadrant
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

// meshConfigToString returns the Istio MeshConfig as a string
func meshConfigToString(config *istiomeshv1alpha1.MeshConfig) (string, error) {
	configString, err := protomarshal.ToYAML(config)
	if err != nil {
		return "", err
	}
	return configString, nil
}
