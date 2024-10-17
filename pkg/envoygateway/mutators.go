package envoygateway

import (
	"fmt"
	"reflect"

	egv1alpha1 "github.com/envoyproxy/gateway/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

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
