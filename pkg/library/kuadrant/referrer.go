package kuadrant

import (
	"encoding/json"
	"fmt"
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
)

type Referrer interface {
	// Kind returns the kind of the referrer object, typically a Kuadrant Policy kind.
	Kind() string
	// BackReferenceAnnotationName returns the name of the annotation in a target reference object that contains the back references to the referrer objects.
	BackReferenceAnnotationName() string
	// DirectReferenceAnnotationName return the name of the annotation for direct reference
	DirectReferenceAnnotationName() string
}

// BackReferencesFromObject returns the names of the policies listed in the annotations of a target ref object.
func BackReferencesFromObject(obj client.Object, referrer Referrer) []client.ObjectKey {
	backRefs, found := utils.ReadAnnotationsFromObject(obj)[referrer.BackReferenceAnnotationName()]
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

func DirectReferencesFromObject(obj client.Object, referrer Referrer) (client.ObjectKey, error) {
	annotations := utils.ReadAnnotationsFromObject(obj)
	key := referrer.DirectReferenceAnnotationName()
	directRefs, found := annotations[key]
	if !found {
		return client.ObjectKey{}, fmt.Errorf("annotation %s not found", key)
	}

	parts := strings.Split(directRefs, "/")
	ref := client.ObjectKey{Namespace: parts[0], Name: parts[1]}

	return ref, nil
}
