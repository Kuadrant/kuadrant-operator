package common

import (
	"encoding/json"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Referrer interface {
	// Kind returns the kind of the referrer object, typically a Kuadrant Policy kind.
	Kind() string
	// BackReferenceAnnotationName returns the name of the annotation in a target reference object that contains the back references to the referrer objects.
	BackReferenceAnnotationName() string
}

// BackReferencesFromObject returns the names of the policies listed in the annotations of a target ref object.
func BackReferencesFromObject(obj client.Object, referrer Referrer) []client.ObjectKey {
	backRefs, found := ReadAnnotationsFromObject(obj)[referrer.BackReferenceAnnotationName()]
	if !found {
		return make([]client.ObjectKey, 0)
	}

	var refs []client.ObjectKey

	err := json.Unmarshal([]byte(backRefs), &refs)
	if err != nil {
		return make([]client.ObjectKey, 0)
	}

	return refs
}
