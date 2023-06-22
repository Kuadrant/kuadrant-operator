package istio

import (
	"fmt"

	maistrav2 "github.com/kuadrant/kuadrant-operator/api/external/maistra/v2"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/structpb"
	istiomeshv1alpha1 "istio.io/api/mesh/v1alpha1"
	istioapiv1alpha1 "istio.io/api/operator/v1alpha1"
	iopv1alpha1 "istio.io/istio/operator/pkg/apis/istio/v1alpha1"
	"istio.io/istio/pkg/util/protomarshal"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// The structs below implement the interface defined in pkg/common/mesh_config.go `ConfigWrapper`
// type ConfigWrapper interface {
//		GetConfigObject() client.Object
//		GetMeshConfig() (*istiomeshv1alpha1.MeshConfig, error)
//		SetMeshConfig(*istiomeshv1alpha1.MeshConfig) error
// }

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
	if err := w.config.Spec.TechPreview.SetField("meshConfig", meshConfigStruct.AsMap()); err != nil {
		return err
	}
	return nil
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
