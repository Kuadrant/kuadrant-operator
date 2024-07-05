package envoygateway

import (
	"fmt"
	"reflect"

	egv1alpha1 "github.com/envoyproxy/gateway/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

	if !reflect.DeepEqual(existing.Spec.TargetRef, desired.Spec.TargetRef) {
		update = true
		existing.Spec.TargetRef = desired.Spec.TargetRef
	}

	if !reflect.DeepEqual(existing.Annotations, desired.Annotations) {
		update = true
		existing.Annotations = desired.Annotations
	}

	return update, nil
}
