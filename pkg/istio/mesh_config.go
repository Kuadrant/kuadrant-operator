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
	istiov1alpha1 "maistra.io/istio-operator/api/v1alpha1"
	"maistra.io/istio-operator/pkg/helm"
	"sigs.k8s.io/controller-runtime/pkg/client"

	maistrav2 "github.com/kuadrant/kuadrant-operator/api/external/maistra/v2"
)

const (
	ExtAuthorizerName = "kuadrant-authorization"
)

type authorizer interface {
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
func RegisterKuadrantAuthorizer(configWrapper ConfigWrapper, authorizer authorizer) error {
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
func UnregisterKuadrantAuthorizer(configWrapper ConfigWrapper, authorizer authorizer) error {
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

// GetMeshConfig returns the IstioOperator MeshConfig
func (w *OperatorWrapper) GetMeshConfig() (*istiomeshv1alpha1.MeshConfig, error) {
	if w.config.Spec == nil {
		w.config.Spec = &istioapiv1alpha1.IstioOperatorSpec{}
	}
	return meshConfigFromStruct(w.config.Spec.MeshConfig)
}

// SetMeshConfig sets the IstioOperator MeshConfig
func (w *OperatorWrapper) SetMeshConfig(config *istiomeshv1alpha1.MeshConfig) error {
	meshConfigStruct, err := meshConfigToStruct(config)
	if err != nil {
		return err
	}
	w.config.Spec.MeshConfig = meshConfigStruct
	return nil
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

// GetMeshConfig returns the MeshConfig from the ConfigMap
func (w *ConfigMapWrapper) GetMeshConfig() (*istiomeshv1alpha1.MeshConfig, error) {
	meshConfigString, ok := w.config.Data["mesh"]
	if !ok {
		return nil, fmt.Errorf("mesh config not found in ConfigMap")
	}
	return meshConfigFromString(meshConfigString)
}

// SetMeshConfig sets the MeshConfig in the ConfigMap
func (w *ConfigMapWrapper) SetMeshConfig(config *istiomeshv1alpha1.MeshConfig) error {
	meshConfigString, err := meshConfigToString(config)
	if err != nil {
		return err
	}
	w.config.Data["mesh"] = meshConfigString
	return nil
}

// OSSMControlPlaneWrapper wraps the OSSM ServiceMeshControlPlane
type OSSMControlPlaneWrapper struct {
	config *maistrav2.ServiceMeshControlPlane
}

// NewOSSMControlPlaneWrapper creates a new OSSMControlPlaneWrapper
func NewOSSMControlPlaneWrapper(config *maistrav2.ServiceMeshControlPlane) *OSSMControlPlaneWrapper {
	return &OSSMControlPlaneWrapper{config: config}
}

// GetConfigObject returns the OSSM ServiceMeshControlPlane
func (w *OSSMControlPlaneWrapper) GetConfigObject() client.Object {
	return w.config
}

// SailWrapper wraps the IstioCR
type SailWrapper struct {
	config *istiov1alpha1.Istio
}

// NewSailWrapper creates a new SailWrapper
func NewSailWrapper(config *istiov1alpha1.Istio) *SailWrapper {
	return &SailWrapper{config: config}
}

// GetConfigObject returns the IstioCR
func (w *SailWrapper) GetConfigObject() client.Object {
	return w.config
}

// GetMeshConfig returns the Istio MeshConfig
func (w *SailWrapper) GetMeshConfig() (*istiomeshv1alpha1.MeshConfig, error) {
	values := w.config.Spec.GetValues()
	config, ok := values["meshConfig"].(map[string]any)
	if !ok {
		return &istiomeshv1alpha1.MeshConfig{}, nil
	}
	meshConfigStruct, err := structpb.NewStruct(config)
	if err != nil {
		return nil, err
	}
	meshConfig, err := meshConfigFromStruct(meshConfigStruct)
	if err != nil {
		return nil, err
	}
	return meshConfig, nil
}

// SetMeshConfig sets the Istio MeshConfig
func (w *SailWrapper) SetMeshConfig(config *istiomeshv1alpha1.MeshConfig) error {
	meshConfigStruct, err := meshConfigToStruct(config)
	if err != nil {
		return err
	}
	values := w.config.Spec.GetValues()
	if values == nil {
		values = helm.HelmValues{}
	}
	if err := values.Set("meshConfig", meshConfigStruct.AsMap()); err != nil {
		return err
	}
	return w.config.Spec.SetValues(values)
}

// GetMeshConfig returns the MeshConfig from the OSSM ServiceMeshControlPlane
func (w *OSSMControlPlaneWrapper) GetMeshConfig() (*istiomeshv1alpha1.MeshConfig, error) {
	if config, found, err := w.config.Spec.TechPreview.GetMap("meshConfig"); err != nil {
		return nil, err
	} else if found {
		meshConfigStruct, err := structpb.NewStruct(config)
		if err != nil {
			return nil, err
		}
		meshConfig, err := meshConfigFromStruct(meshConfigStruct)
		if err != nil {
			return nil, err
		}
		return meshConfig, nil
	}
	return &istiomeshv1alpha1.MeshConfig{}, nil
}

// SetMeshConfig sets the MeshConfig in the OSSM ServiceMeshControlPlane
func (w *OSSMControlPlaneWrapper) SetMeshConfig(config *istiomeshv1alpha1.MeshConfig) error {
	meshConfigStruct, err := meshConfigToStruct(config)
	if err != nil {
		return err
	}

	return w.config.Spec.TechPreview.SetField("meshConfig", meshConfigStruct.AsMap())
}

// meshConfigFromStruct Builds the Istio/OSSM MeshConfig from a compatible structure:
//
//	meshConfig:
//	  extensionProviders:
//	    - envoyExtAuthzGrpc:
//	        port: <port>
//	        service: <authorino-service>
//	      name: kuadrant-authorization
func meshConfigFromStruct(structure *structpb.Struct) (*istiomeshv1alpha1.MeshConfig, error) {
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

// meshConfigToStruct Marshals the Istio MeshConfig into a struct
func meshConfigToStruct(config *istiomeshv1alpha1.MeshConfig) (*structpb.Struct, error) {
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

// meshConfigFromString returns the Istio MeshConfig from a ConfigMap
func meshConfigFromString(config string) (*istiomeshv1alpha1.MeshConfig, error) {
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
