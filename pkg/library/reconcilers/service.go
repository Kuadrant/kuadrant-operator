package reconcilers

import (
	"fmt"
	"reflect"

	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ServiceMutateFn is a function which mutates the existing Service into it's desired state.
type ServiceMutateFn func(desired, existing *v1.Service) bool

func ServiceMutator(opts ...ServiceMutateFn) MutateFn {
	return func(existingObj, desiredObj client.Object) (bool, error) {
		existing, ok := existingObj.(*v1.Service)
		if !ok {
			return false, fmt.Errorf("%T is not a *v1.Service", existingObj)
		}
		desired, ok := desiredObj.(*v1.Service)
		if !ok {
			return false, fmt.Errorf("%T is not a *v1.Service", desiredObj)
		}

		update := false

		// Loop through each option
		for _, opt := range opts {
			tmpUpdate := opt(desired, existing)
			update = update || tmpUpdate
		}

		return update, nil
	}
}

func ServicePortsMutator(desired, existing *v1.Service) bool {
	update := false

	if !reflect.DeepEqual(existing.Spec.Ports, desired.Spec.Ports) {
		existing.Spec.Ports = desired.Spec.Ports
		update = true
	}

	return update
}
