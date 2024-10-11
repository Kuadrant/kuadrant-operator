package consoleplugin

import (
	"reflect"

	consolev1 "github.com/openshift/api/console/v1"
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
