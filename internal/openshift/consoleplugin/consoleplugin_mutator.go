package consoleplugin

import (
	"reflect"

	consolev1 "github.com/openshift/api/console/v1"
)

func SpecMutator(desired, existing *consolev1.ConsolePlugin) bool {
	if desired.Spec.Backend.Service == nil {
		panic("coded ConsolePlugin does not specify service")
	}

	update := false

	if !reflect.DeepEqual(existing.Spec.Backend.Service, desired.Spec.Backend.Service) {
		existing.Spec.Backend.Service = desired.Spec.Backend.Service
		update = true
	}
	if !reflect.DeepEqual(existing.Spec.Proxy, desired.Spec.Proxy) {
		existing.Spec.Proxy = desired.Spec.Proxy
		update = true
	}

	return update
}
