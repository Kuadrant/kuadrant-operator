package common

import (
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestNamespacedNameToObjectKey(t *testing.T) {
	t.Run("when a namespaced name is provided then return an ObjectKey with corresponding namespace and name", func(t *testing.T) {
		namespacedName := "test-namespace/test-name"
		defaultNamespace := "default"

		result := NamespacedNameToObjectKey(namespacedName, defaultNamespace)

		expected := client.ObjectKey{Name: "test-name", Namespace: "test-namespace"}
		if !reflect.DeepEqual(result, expected) {
			t.Errorf("Expected %v, but got %v", expected, result)
		}
	})

	t.Run("when only a name is provided then return an ObjectKey with the default namespace and provided name", func(t *testing.T) {
		namespacedName := "test-name"
		defaultNamespace := "default"

		result := NamespacedNameToObjectKey(namespacedName, defaultNamespace)

		expected := client.ObjectKey{Name: "test-name", Namespace: "default"}
		if !reflect.DeepEqual(result, expected) {
			t.Errorf("Expected %v, but got %v", expected, result)
		}
	})

	t.Run("when an empty string is provided, then return an ObjectKey with default namespace and empty name", func(t *testing.T) {
		namespacedName := ""
		defaultNamespace := "default"

		result := NamespacedNameToObjectKey(namespacedName, defaultNamespace)

		expected := client.ObjectKey{Name: "", Namespace: "default"}
		if !reflect.DeepEqual(result, expected) {
			t.Errorf("Expected %v, but got %v", expected, result)
		}
	})
}

func TestReadAnnotationsFromObject(t *testing.T) {
	testCases := []struct {
		name     string
		input    client.Object
		expected map[string]string
	}{
		{
			name: "when object has annotations then return the annotations",
			input: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						"key1": "value1",
						"key2": "value2",
					},
				},
			},
			expected: map[string]string{
				"key1": "value1",
				"key2": "value2",
			},
		},
		{
			name: "when object has no annotations then return an empty map",
			input: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{},
			},
			expected: map[string]string{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual := ReadAnnotationsFromObject(tc.input)
			if !reflect.DeepEqual(actual, tc.expected) {
				t.Errorf("Expected annotations %v, but got %v", tc.expected, actual)
			}
		})
	}
}
