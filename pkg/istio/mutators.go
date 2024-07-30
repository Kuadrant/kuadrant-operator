package istio

import (
	"fmt"
	"reflect"

	istioclientgoextensionv1alpha1 "istio.io/client-go/pkg/apis/extensions/v1alpha1"
	istiov1beta1 "istio.io/client-go/pkg/apis/security/v1beta1"
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

func AuthorizationPolicyMutator(existingObj, desiredObj client.Object) (bool, error) {
	existing, ok := existingObj.(*istiov1beta1.AuthorizationPolicy)
	if !ok {
		return false, fmt.Errorf("%T is not an *istiov1beta1.AuthorizationPolicy", existingObj)
	}
	desired, ok := desiredObj.(*istiov1beta1.AuthorizationPolicy)
	if !ok {
		return false, fmt.Errorf("%T is not an *istiov1beta1.AuthorizationPolicy", desiredObj)
	}

	var update bool

	if !reflect.DeepEqual(existing.Spec.Action, desired.Spec.Action) {
		update = true
		existing.Spec.Action = desired.Spec.Action
	}

	if !reflect.DeepEqual(existing.Spec.ActionDetail, desired.Spec.ActionDetail) {
		update = true
		existing.Spec.ActionDetail = desired.Spec.ActionDetail
	}

	if !reflect.DeepEqual(existing.Spec.Rules, desired.Spec.Rules) {
		update = true
		existing.Spec.Rules = desired.Spec.Rules
	}

	if !reflect.DeepEqual(existing.Spec.Selector, desired.Spec.Selector) {
		update = true
		existing.Spec.Selector = desired.Spec.Selector
	}

	if !reflect.DeepEqual(existing.Annotations, desired.Annotations) {
		update = true
		existing.Annotations = desired.Annotations
	}

	return update, nil
}
