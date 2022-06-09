package istio

import (
	"encoding/json"
	"fmt"
	"strings"

	_struct "github.com/golang/protobuf/ptypes/struct"
	apimv1alpha1 "github.com/kuadrant/kuadrant-controller/apis/apim/v1alpha1"
	"github.com/kuadrant/kuadrant-controller/pkg/common"
	istioapiv1alpha1 "istio.io/api/extensions/v1alpha1"
	"istio.io/api/type/v1beta1"
	istioextensionv1alpha3 "istio.io/client-go/pkg/apis/extensions/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

var shimURL = common.FetchEnv("SHIM_IMAGE", "oci://quay.io/rahanand/wasm-shim:latest")

type WasmPluginFactory struct {
	ObjectName string
	Namespace  string
	Phase      istioapiv1alpha1.PluginPhase
	Config     *_struct.Struct
	Labels     map[string]string
}

func (v *WasmPluginFactory) WasmPlugin() *istioextensionv1alpha3.WasmPlugin {
	return &istioextensionv1alpha3.WasmPlugin{
		TypeMeta: metav1.TypeMeta{
			Kind:       "WasmPlugin",
			APIVersion: "extensions.istio.io/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      v.ObjectName,
			Namespace: v.Namespace,
		},
		Spec: istioapiv1alpha1.WasmPlugin{
			Selector: &v1beta1.WorkloadSelector{
				MatchLabels: v.Labels,
			},
			Url:          shimURL, // TODO: take this from Environment.
			PluginConfig: v.Config,
			Phase:        v.Phase,
		},
	}
}

func WASMPluginKey(gwKey client.ObjectKey, stage apimv1alpha1.RateLimitStage) client.ObjectKey {
	stageStr := strings.ToLower(string(stage))
	return client.ObjectKey{
		Name:      fmt.Sprintf("kuadrant-%s-wasm-%s", gwKey.Name, stageStr),
		Namespace: gwKey.Namespace,
	}
}

func PluginConfigToWasmPluginStruct(config *PluginConfig) (*_struct.Struct, error) {
	pluginConfigJSON, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}

	pluginConfigStruct := &_struct.Struct{}
	if err := pluginConfigStruct.UnmarshalJSON(pluginConfigJSON); err != nil {
		return nil, err
	}
	return pluginConfigStruct, nil
}

func WasmPlugins(rlp *apimv1alpha1.RateLimitPolicy, gwKey client.ObjectKey, gwLabels map[string]string, hosts []gatewayapiv1alpha2.Hostname) ([]*istioextensionv1alpha3.WasmPlugin, error) {
	rlpKey := client.ObjectKeyFromObject(rlp)

	stagePhaseMapping := map[apimv1alpha1.RateLimitStage]istioapiv1alpha1.PluginPhase{
		apimv1alpha1.RateLimitStagePREAUTH:  istioapiv1alpha1.PluginPhase_AUTHN,
		apimv1alpha1.RateLimitStagePOSTAUTH: istioapiv1alpha1.PluginPhase_STATS,
	}

	wasmPlugins := []*istioextensionv1alpha3.WasmPlugin{}
	for stage, phase := range stagePhaseMapping {
		pluginPolicy := PluginPolicyFromRateLimitPolicy(rlp, stage, hosts)
		pluginConfig := &PluginConfig{
			FailureModeDeny: true,
			PluginPolicies: map[string]PluginPolicy{
				rlpKey.String(): *pluginPolicy,
			},
		}

		pluginConfigStruct, err := PluginConfigToWasmPluginStruct(pluginConfig)
		if err != nil {
			return nil, err
		}

		pluginKey := WASMPluginKey(gwKey, stage)
		pluginFactory := WasmPluginFactory{
			ObjectName: pluginKey.Name,
			Namespace:  pluginKey.Namespace,
			Phase:      phase,
			Config:     pluginConfigStruct,
			Labels:     gwLabels,
		}

		wasmPlugins = append(wasmPlugins, pluginFactory.WasmPlugin())
	}

	return wasmPlugins, nil
}

func WASMPluginMutator(existingObj, desiredObj client.Object) (bool, error) {
	update := false
	existing, ok := existingObj.(*istioextensionv1alpha3.WasmPlugin)
	if !ok {
		return false, fmt.Errorf("%T is not a *istioextensionv1alpha3.WasmPlugin", existingObj)
	}
	desired, ok := desiredObj.(*istioextensionv1alpha3.WasmPlugin)
	if !ok {
		return false, fmt.Errorf("%T is not a *istioextensionv1alpha3.WasmPlugin", desiredObj)
	}

	// Deserialize config into PluginConfig struct
	existingConfigJSON, err := existing.Spec.PluginConfig.MarshalJSON()
	if err != nil {
		return false, err
	}
	existingPluginConfig := &PluginConfig{}
	if err := json.Unmarshal(existingConfigJSON, existingPluginConfig); err != nil {
		return false, err
	}

	desiredConfigJSON, err := desired.Spec.PluginConfig.MarshalJSON()
	if err != nil {
		return false, err
	}
	desiredPluginConfig := &PluginConfig{}
	if err := json.Unmarshal(desiredConfigJSON, desiredPluginConfig); err != nil {
		return false, err
	}

	patchUpdate := false
	MergeMapStringPluginPolicy(&patchUpdate, &existingPluginConfig.PluginPolicies, desiredPluginConfig.PluginPolicies)

	if patchUpdate {
		update = true
		finalPluginConfig, err := PluginConfigToWasmPluginStruct(existingPluginConfig)
		if err != nil {
			return false, err
		}
		existing.Spec.PluginConfig = finalPluginConfig
	}
	return update, nil
}
