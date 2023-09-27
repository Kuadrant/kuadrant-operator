package common

import (
	"strings"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// NamespacedNameToObjectKey converts <namespace/name> format string to k8s object key.
// It's common for K8s to reference an object using this format. For e.g. gateways in VirtualService.
func NamespacedNameToObjectKey(namespacedName, defaultNamespace string) client.ObjectKey {
	if i := strings.IndexRune(namespacedName, '/'); i >= 0 {
		return client.ObjectKey{Namespace: namespacedName[:i], Name: namespacedName[i+1:]}
	}
	return client.ObjectKey{Namespace: defaultNamespace, Name: namespacedName}
}

// ReadAnnotationsFromObject reads the annotations from a Kubernetes object
// and returns them as a map. If the object has no annotations, it returns an empty map.
func ReadAnnotationsFromObject(obj client.Object) map[string]string {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	return annotations
}
