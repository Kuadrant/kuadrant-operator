package reconcilers

import (
	"fmt"

	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// ConfigMapMutateFn is a function which mutates the existing ConfigMap into it's desired state.
type ConfigMapMutateFn func(desired, existing *v1.ConfigMap) bool

func ConfigMapMutator(opts ...ConfigMapMutateFn) MutateFn {
	return func(existingObj, desiredObj client.Object) (bool, error) {
		existing, ok := existingObj.(*v1.ConfigMap)
		if !ok {
			return false, fmt.Errorf("%T is not a *v1.ConfigMap", existingObj)
		}
		desired, ok := desiredObj.(*v1.ConfigMap)
		if !ok {
			return false, fmt.Errorf("%T is not a *v1.ConfigMap", desiredObj)
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

func ConfigMapReconcileField(desired, existing *v1.ConfigMap, fieldName string) bool {
	updated := false

	if existingVal, ok := existing.Data[fieldName]; !ok {
		existing.Data[fieldName] = desired.Data[fieldName]
		updated = true
	} else {
		if desired.Data[fieldName] != existingVal {
			existing.Data[fieldName] = desired.Data[fieldName]
			updated = true
		}
	}
	return updated
}
