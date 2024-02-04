package metadata

import (
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func GetAnnotationsByPrefix(obj metav1.Object, prefix string) map[string]string {
	annotations := map[string]string{}

	//get any annotations that contain the prefix
	exists, keys := HasAnnotationsContaining(obj, prefix)
	if !exists {
		return annotations
	}
	for k, v := range keys {
		//check the annotation starts with the prefix
		if strings.HasPrefix(k, prefix) {
			annotations[k] = v
		}
	}
	return annotations
}

func GetAnnotation(obj metav1.Object, key string) string {
	if !HasAnnotation(obj, key) {
		return ""
	}
	return obj.GetAnnotations()[key]
}

func HasAnnotation(obj metav1.Object, key string) bool {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		return false
	}
	_, ok := annotations[strings.TrimSpace(key)]
	return ok
}

func HasAnnotationsContaining(obj metav1.Object, key string) (bool, map[string]string) {
	matches := map[string]string{}
	Annotations := obj.GetAnnotations()
	if Annotations == nil {
		return false, matches
	}

	for k, annotation := range Annotations {
		if strings.Contains(k, key) {
			matches[k] = annotation
		}
	}
	return len(matches) > 0, matches
}

func AddAnnotation(obj metav1.Object, key, value string) {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	for k, v := range annotations {
		if k == key {
			if v == value {
				return
			}
			break
		}
	}
	annotations[key] = value
	obj.SetAnnotations(annotations)
}

func RemoveAnnotationsByPrefix(obj metav1.Object, prefix string) int {
	count := 0
	for annotation := range GetAnnotationsByPrefix(obj, prefix) {
		RemoveAnnotation(obj, annotation)
		count++
	}
	return count
}

func RemoveAnnotation(obj metav1.Object, key string) {
	annotations := obj.GetAnnotations()
	for k := range annotations {
		if k == key {
			delete(annotations, key)
			obj.SetAnnotations(annotations)
			return
		}
	}
}

type AnnotationPredicate func(key, value string) bool

func KeyPredicate(predicate func(key string) bool) AnnotationPredicate {
	return func(key, _ string) bool {
		return predicate(key)
	}
}

// CopyAnnotation copies an annotation with key `key` from `fromObj` into `toObj`
// Returns `true` if the annotation was found and copied, `false` otherwise
func CopyAnnotation(fromObj, toObj metav1.Object, key string) bool {
	return CopyAnnotationsPredicate(fromObj, toObj, func(eachKey, value string) bool {
		return eachKey == key
	})
}

// CopyAnnotationsPredicate copies any annotation from fromObj into toObj annotations
// that fullfils the given predicate. Returns true if at least one annotation was
// copied
func CopyAnnotationsPredicate(fromObj, toObj metav1.Object, predicate AnnotationPredicate) bool {
	fromObjAnnotations := fromObj.GetAnnotations()
	if fromObjAnnotations == nil {
		return false
	}

	toObjAnnotations := toObj.GetAnnotations()
	if toObjAnnotations == nil {
		toObjAnnotations = map[string]string{}
		toObj.SetAnnotations(toObjAnnotations)
	}

	copied := false
	for key, value := range fromObjAnnotations {
		if !predicate(key, value) {
			continue
		}

		toObjAnnotations[key] = value
		copied = true
	}

	return copied
}
