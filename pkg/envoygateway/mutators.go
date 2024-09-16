package envoygateway

import (
	"fmt"
	"reflect"

	egv1alpha1 "github.com/envoyproxy/gateway/api/v1alpha1"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/kuadrant/kuadrant-operator/pkg/rlptools/wasm"
)

func EnvoyExtensionPolicyMutator(existingObj, desiredObj client.Object) (bool, error) {
	existing, ok := existingObj.(*egv1alpha1.EnvoyExtensionPolicy)
	if !ok {
		return false, fmt.Errorf("%T is not an *egapi.EnvoyExtensionPolicy", existingObj)
	}
	desired, ok := desiredObj.(*egv1alpha1.EnvoyExtensionPolicy)
	if !ok {
		return false, fmt.Errorf("%T is not an *egapi.EnvoyExtensionPolicy", desiredObj)
	}

	var update bool

	if len(existing.Spec.Wasm) != len(desired.Spec.Wasm) {
		update = true
		existing.Spec.Wasm = desired.Spec.Wasm
	}

	for idx := range desired.Spec.Wasm {
		opts := cmp.Options{
			cmpopts.IgnoreFields(egv1alpha1.Wasm{}, "Config"),
			cmpopts.IgnoreMapEntries(func(k string, _ any) bool {
				return k == "config"
			}),
		}

		// Compare all except config (which is serialized into bytes)
		if cmp.Equal(desired.Spec.Wasm[idx], existing.Spec.Wasm[idx], opts) {
			update = true
			existing.Spec.Wasm[idx] = desired.Spec.Wasm[idx]
		}

		existingWasmConfig, err := wasm.ConfigFromJSON(existing.Spec.Wasm[idx].Config)
		if err != nil {
			return false, err
		}

		desiredWasmConfig, err := wasm.ConfigFromJSON(desired.Spec.Wasm[idx].Config)
		if err != nil {
			return false, err
		}

		// TODO(eastizle): reflect.DeepEqual does not work well with lists without order
		if !reflect.DeepEqual(desiredWasmConfig, existingWasmConfig) {
			update = true
			existing.Spec.Wasm[idx].Config = desired.Spec.Wasm[idx].Config
		}
	}

	if !reflect.DeepEqual(existing.Spec.PolicyTargetReferences.TargetRefs, desired.Spec.PolicyTargetReferences.TargetRefs) {
		update = true
		existing.Spec.PolicyTargetReferences.TargetRefs = desired.Spec.PolicyTargetReferences.TargetRefs
	}

	if !reflect.DeepEqual(existing.Annotations, desired.Annotations) {
		update = true
		existing.Annotations = desired.Annotations
	}

	return update, nil
}

func EnvoySecurityPolicyMutator(existingObj, desiredObj client.Object) (bool, error) {
	existing, ok := existingObj.(*egv1alpha1.SecurityPolicy)
	if !ok {
		return false, fmt.Errorf("%T is not an *egapi.SecurityPolicy", existingObj)
	}
	desired, ok := desiredObj.(*egv1alpha1.SecurityPolicy)
	if !ok {
		return false, fmt.Errorf("%T is not an *egapi.SecurityPolicy", desiredObj)
	}

	var update bool

	if !reflect.DeepEqual(existing.Spec.ExtAuth, desired.Spec.ExtAuth) {
		update = true
		existing.Spec.ExtAuth = desired.Spec.ExtAuth
	}

	if !reflect.DeepEqual(existing.Spec.PolicyTargetReferences.TargetRefs, desired.Spec.PolicyTargetReferences.TargetRefs) {
		update = true
		existing.Spec.PolicyTargetReferences.TargetRefs = desired.Spec.PolicyTargetReferences.TargetRefs
	}

	if !reflect.DeepEqual(existing.Annotations, desired.Annotations) {
		update = true
		existing.Annotations = desired.Annotations
	}

	return update, nil
}

func SecurityPolicyReferenceGrantMutator(existingObj, desiredObj client.Object) (bool, error) {
	existing, ok := existingObj.(*gatewayapiv1beta1.ReferenceGrant)
	if !ok {
		return false, fmt.Errorf("%T is not an *gatewayapiv1beta1.ReferenceGrant", existingObj)
	}
	desired, ok := desiredObj.(*gatewayapiv1beta1.ReferenceGrant)
	if !ok {
		return false, fmt.Errorf("%T is not an *gatewayapiv1beta1.ReferenceGrant", desiredObj)
	}

	var update bool
	if !reflect.DeepEqual(existing.Spec.From, desired.Spec.From) {
		update = true
		existing.Spec.From = desired.Spec.From
	}

	if !reflect.DeepEqual(existing.Spec.To, desired.Spec.To) {
		update = true
		existing.Spec.To = desired.Spec.To
	}

	if !reflect.DeepEqual(existing.Annotations, desired.Annotations) {
		update = true
		existing.Annotations = desired.Annotations
	}

	return update, nil
}
