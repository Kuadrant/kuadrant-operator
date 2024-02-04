package metadata

import (
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func AddFinalizer(obj metav1.Object, finalizer string) {
	finalizers := obj.GetFinalizers()
	if HasFinalizer(obj, finalizer) {
		return
	}
	finalizers = append(finalizers, finalizer)
	obj.SetFinalizers(finalizers)
}

func RemoveFinalizer(obj metav1.Object, finalizer string) {
	finalizers := obj.GetFinalizers()
	for i, v := range finalizers {
		if v == finalizer {
			finalizers[i] = finalizers[len(finalizers)-1]
			finalizers = finalizers[:len(finalizers)-1]
			obj.SetFinalizers(finalizers)
			return
		}
	}
}

func HasFinalizer(obj metav1.Object, finalizer string) bool {
	finalizers := obj.GetFinalizers()
	for _, v := range finalizers {
		if v == finalizer {
			return true
		}
	}

	return false
}

func HasFinalizersContaining(obj metav1.Object, key string) (bool, []string) {
	finalizers := obj.GetFinalizers()
	if finalizers == nil {
		return false, []string{}
	}
	var matches []string
	for _, finalizer := range finalizers {
		if strings.Contains(finalizer, key) {
			matches = append(matches, finalizer)
		}
	}
	return len(matches) > 0, matches
}
