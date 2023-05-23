//go:build unit
// +build unit

package common

import (
	"context"
	"reflect"
	"testing"

	"github.com/kuadrant/limitador-operator/api/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	fake "sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestObjectKeyListDifference(t *testing.T) {

	key1 := client.ObjectKey{Namespace: "ns1", Name: "obj1"}
	key2 := client.ObjectKey{Namespace: "ns2", Name: "obj2"}
	key3 := client.ObjectKey{Namespace: "ns3", Name: "obj3"}

	testCases := []struct {
		name     string
		a        []client.ObjectKey
		b        []client.ObjectKey
		expected []client.ObjectKey
	}{
		{
			"empty",
			[]client.ObjectKey{},
			[]client.ObjectKey{},
			[]client.ObjectKey{},
		},
		{
			"a empty",
			[]client.ObjectKey{},
			[]client.ObjectKey{key1},
			[]client.ObjectKey{},
		},
		{
			"b empty",
			[]client.ObjectKey{key1, key2},
			[]client.ObjectKey{},
			[]client.ObjectKey{key1, key2},
		},
		{
			"equal",
			[]client.ObjectKey{key1, key2, key3},
			[]client.ObjectKey{key1, key2, key3},
			[]client.ObjectKey{},
		},
		{
			"missing key2",
			[]client.ObjectKey{key1, key2, key3},
			[]client.ObjectKey{key1, key3},
			[]client.ObjectKey{key2},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(subT *testing.T) {
			res := ObjectKeyListDifference(tc.a, tc.b)
			if len(res) != len(tc.expected) {
				subT.Errorf("expected len (%d), got (%d)", len(tc.expected), len(res))
			}

			for idx := range res {
				if res[idx] != tc.expected[idx] {
					subT.Errorf("expected object (index %d) does not match. Expected (%s), got (%s)", idx, tc.expected[idx], res[idx])
				}
			}
		})
	}
}

func TestGetService(t *testing.T) {
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "svc-ns",
			Name:      "my-svc",
			Labels: map[string]string{
				"a-label": "irrelevant",
			},
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"a-selector": "what-we-are-looking-for",
			},
		},
	}

	k8sClient := fake.NewFakeClient(service)

	var svc *corev1.Service
	var err error

	svc, err = GetService(context.TODO(), k8sClient, client.ObjectKey{Namespace: "svc-ns", Name: "my-svc"})
	if err != nil || svc == nil || svc.GetNamespace() != service.GetNamespace() || svc.GetName() != service.GetName() {
		t.Error("should have gotten Service svc-ns/my-svc")
	}

	svc, err = GetService(context.TODO(), k8sClient, client.ObjectKey{Namespace: "svc-ns", Name: "unknown"})
	if err == nil || !apierrors.IsNotFound(err) || svc != nil {
		t.Error("should have gotten no Service")
	}
}

func TestGetServiceWorkloadSelector(t *testing.T) {
	service := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "svc-ns",
			Name:      "my-svc",
			Labels: map[string]string{
				"a-label": "irrelevant",
			},
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"a-selector": "what-we-are-looking-for",
			},
		},
	}

	k8sClient := fake.NewFakeClient(service)

	var selector map[string]string
	var err error

	selector, err = GetServiceWorkloadSelector(context.TODO(), k8sClient, client.ObjectKey{Namespace: "svc-ns", Name: "my-svc"})
	if err != nil || len(selector) != 1 || selector["a-selector"] != "what-we-are-looking-for" {
		t.Error("should not have failed to get the service workload selector")
	}

	selector, err = GetServiceWorkloadSelector(context.TODO(), k8sClient, client.ObjectKey{Namespace: "svc-ns", Name: "unknown-svc"})
	if err == nil || !apierrors.IsNotFound(err) || selector != nil {
		t.Error("should have failed to get the service workload selector")
	}
}

func TestObjectInfo(t *testing.T) {
	testCases := []struct {
		name     string
		input    client.Object
		expected string
	}{
		{
			name: "when given a Kubernetes object then return formatted string",
			input: &v1alpha1.Limitador{
				TypeMeta: metav1.TypeMeta{
					Kind: "Limitador",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-limitador",
				},
			},
			expected: "Limitador/test-limitador",
		},
		{
			name: "when given a Kubernetes object with empty Kind then return formatted string",
			input: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-service",
				},
			},
			expected: "/test-service",
		},
		{
			name: "when given a Kubernetes object with empty Name then return formatted string",
			input: &corev1.Namespace{
				TypeMeta: metav1.TypeMeta{
					Kind: "Namespace",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "",
				},
			},
			expected: "Namespace/",
		},
		{
			name:     "when given empty Kubernetes object then return formatted string (separator only)",
			input:    &corev1.Pod{},
			expected: "/",
		},
	}

	for _, c := range testCases {
		t.Run(c.name, func(t *testing.T) {
			if actual := ObjectInfo(c.input); actual != c.expected {
				t.Errorf("Expected %q, got %q", c.expected, actual)
			}
		})
	}
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

func TestTagObjectToDelete(t *testing.T) {
	testCases := []struct {
		name         string
		input        client.Object
		expectedTags map[string]string
	}{
		{
			name: "when object has no annotations (nil) then initialize them with empty map and add 'delete' tag",
			input: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-pod",
					Annotations: nil,
				},
			},
			expectedTags: map[string]string{
				DeleteTagAnnotation: "true",
			},
		},
		{
			name: "when object has empty annotations (empty map) then add 'delete' tag",
			input: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{
					Name:        "test-service",
					Annotations: map[string]string{},
				},
			},
			expectedTags: map[string]string{
				DeleteTagAnnotation: "true",
			},
		},
		{
			name: "when object already has annotations then add 'delete' tag",
			input: &v1alpha1.Limitador{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-service",
					Annotations: map[string]string{
						"key1": "value1",
						"key2": "value2",
					},
				},
			},
			expectedTags: map[string]string{
				DeleteTagAnnotation: "true",
				"key1":              "value1",
				"key2":              "value2",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			TagObjectToDelete(tc.input)

			annotations := tc.input.GetAnnotations()
			if annotations == nil {
				t.Fatal("Expected annotations to be not nil, but got nil")
			}

			if !reflect.DeepEqual(annotations, tc.expectedTags) {
				t.Errorf("Expected annotations to be '%v', but got '%v'", tc.expectedTags, annotations)
			}
		})
	}
}

func TestIsObjectTaggedToDelete(t *testing.T) {
	testCases := []struct {
		name        string
		input       client.Object
		annotations map[string]string
		expected    bool
	}{
		{
			name: "when object has delete tag annotation set to true then return true",
			input: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						DeleteTagAnnotation: "true",
					},
				},
			},
			expected: true,
		},
		{
			name: "when object has delete tag annotation set to false then return false",
			input: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{
						DeleteTagAnnotation: "false",
					},
				},
			},
			expected: false,
		},
		{
			name: "when object has no delete tag annotation then return false",
			input: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{},
				},
			},
			expected: false,
		},
		{
			name: "when object annotations are nil then return false",
			input: &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: nil,
				},
			},
			expected: false,
		},
	}

	for _, c := range testCases {
		t.Run(c.name, func(t *testing.T) {
			actual := IsObjectTaggedToDelete(c.input)
			if actual != c.expected {
				t.Errorf("Expected %v, but got %v", c.expected, actual)
			}
		})
	}
}
