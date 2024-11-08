package reconcilers

import (
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// TypedMutateFn is a function which mutates the existing T into it's desired state.
type TypedMutateFn[T client.Object] func(desired, existing T) bool

func Mutator[T client.Object](opts ...TypedMutateFn[T]) MutateFn {
	return func(existingObj, desiredObj client.Object) (bool, error) {
		existing, ok := existingObj.(T)
		if !ok {
			return false, fmt.Errorf("existing %T is not %T", existingObj, *new(T))
		}
		desired, ok := desiredObj.(T)
		if !ok {
			return false, fmt.Errorf("desired %T is not %T", desiredObj, *new(T))
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
