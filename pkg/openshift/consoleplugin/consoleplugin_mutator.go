package consoleplugin

import (
	"fmt"
	"reflect"

	consolev1 "github.com/openshift/api/console/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kuadrant/kuadrant-operator/pkg/library/reconcilers"
)

func ServiceMutator(desired, existing *consolev1.ConsolePlugin) bool {
	if desired.Spec.Backend.Service == nil {
		panic("coded ConsolePlugin does not specify service")
	}

	update := false

	if !reflect.DeepEqual(existing.Spec.Backend.Service, desired.Spec.Backend.Service) {
		existing.Spec.Backend.Service = desired.Spec.Backend.Service
		update = true
	}

	return update
}

// ConsolePluginMutateFn is a function which mutates the existing ConsolePlugin into it's desired state.
type MutateFn func(desired, existing *consolev1.ConsolePlugin) bool

func Mutator(opts ...MutateFn) reconcilers.MutateFn {
	return func(existingObj, desiredObj client.Object) (bool, error) {
		existing, ok := existingObj.(*consolev1.ConsolePlugin)
		if !ok {
			return false, fmt.Errorf("%T is not a *consolev1.ConsolePlugin", existingObj)
		}
		desired, ok := desiredObj.(*consolev1.ConsolePlugin)
		if !ok {
			return false, fmt.Errorf("%T is not a *consolev1.ConsolePlugin", desiredObj)
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
