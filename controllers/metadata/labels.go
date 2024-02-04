package metadata

import (
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func HasLabel(obj metav1.Object, key string) bool {
	labels := obj.GetLabels()
	if labels == nil {
		return false
	}
	_, ok := labels[key]
	return ok
}

func GetLabel(obj metav1.Object, key string) string {
	if !HasLabel(obj, key) {
		return ""
	}
	return obj.GetLabels()[key]
}

func HasLabelsContaining(obj metav1.Object, key string) (bool, map[string]string) {
	matches := map[string]string{}
	labels := obj.GetLabels()
	if labels == nil {
		return false, matches
	}

	for k, label := range labels {
		if strings.Contains(k, key) {
			matches[k] = label
		}
	}
	return len(matches) > 0, matches
}

func AddLabel(obj metav1.Object, key, value string) {
	labels := obj.GetLabels()
	if labels == nil {
		labels = map[string]string{}
	}
	for k, v := range labels {
		if k == key {
			if v == value {
				return
			}
		}
	}
	labels[key] = value
	obj.SetLabels(labels)
}

func RemoveLabel(obj metav1.Object, key string) {
	labels := obj.GetLabels()
	for k := range labels {
		if k == key {
			delete(labels, key)
			obj.SetLabels(labels)
			return
		}
	}
}
