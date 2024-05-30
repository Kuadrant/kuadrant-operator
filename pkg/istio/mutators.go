package istio

import (
	"fmt"
	"reflect"

	istioclientgoextensionv1alpha1 "istio.io/client-go/pkg/apis/extensions/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kuadrant/kuadrant-operator/pkg/rlptools/wasm"
)

func WASMPluginMutator(existingObj, desiredObj client.Object) (bool, error) {
	update := false
	existing, ok := existingObj.(*istioclientgoextensionv1alpha1.WasmPlugin)
	if !ok {
		return false, fmt.Errorf("%T is not a *istioclientgoextensionv1alpha1.WasmPlugin", existingObj)
	}
	desired, ok := desiredObj.(*istioclientgoextensionv1alpha1.WasmPlugin)
	if !ok {
		return false, fmt.Errorf("%T is not a *istioclientgoextensionv1alpha1.WasmPlugin", desiredObj)
	}

	existingWasmConfig, err := wasm.ConfigFromStruct(existing.Spec.PluginConfig)
	if err != nil {
		return false, err
	}

	desiredWasmConfig, err := wasm.ConfigFromStruct(desired.Spec.PluginConfig)
	if err != nil {
		return false, err
	}

	// TODO(eastizle): reflect.DeepEqual does not work well with lists without order
	if !reflect.DeepEqual(desiredWasmConfig, existingWasmConfig) {
		update = true
		existing.Spec.PluginConfig = desired.Spec.PluginConfig
	}

	return update, nil
}
