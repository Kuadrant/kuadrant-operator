//go:build unit

package utils

import (
	"context"
	"reflect"
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	dfake "k8s.io/client-go/dynamic/fake"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kuadrant/limitador-operator/api/v1alpha1"
)

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

func TestIsOwnedBy(t *testing.T) {
	testCases := []struct {
		name     string
		owned    client.Object
		owner    client.Object
		expected bool
	}{
		{
			name: "when owned object has owner reference then return true",
			owned: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "v1",
							Kind:       "Deployment",
							Name:       "my-deployment",
						},
					},
				},
			},
			owner: &appsv1.Deployment{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Deployment",
					APIVersion: "v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-deployment",
				},
			},
			expected: true,
		},
		{
			name: "when owned object does not have owner reference then return false",
			owned: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{},
			},
			owner: &appsv1.Deployment{
				TypeMeta: metav1.TypeMeta{
					Kind: "Deployment",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-deployment",
				},
			},
			expected: false,
		},
		{
			name: "when owned object has owner reference with different group then return false",
			owned: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "apps/v1",
							Kind:       "Deployment",
							Name:       "my-deployment",
						},
					},
				},
			},
			owner: &appsv1.Deployment{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Deployment",
					APIVersion: "v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-deployment",
				},
			},
			expected: false,
		},
		{
			name: "when owned object has owner reference with different kind then return false",
			owned: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "v1",
							Kind:       "StatefulSet",
							Name:       "my-deployment",
						},
					},
				},
			},
			owner: &appsv1.Deployment{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Deployment",
					APIVersion: "v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-deployment",
				},
			},
			expected: false,
		},
		{
			name: "when owned object has owner reference with different name then return false",
			owned: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "v1",
							Kind:       "Deployment",
							Name:       "other-deployment",
						},
					},
				},
			},
			owner: &appsv1.Deployment{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Deployment",
					APIVersion: "v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: "my-deployment",
				},
			},
			expected: false,
		},
		{
			name: "when owned object has owner reference and in same namespace then return true",
			owned: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "ns1",
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "v1",
							Kind:       "Deployment",
							Name:       "my-deployment",
						},
					},
				},
			},
			owner: &appsv1.Deployment{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Deployment",
					APIVersion: "v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-deployment",
					Namespace: "ns1",
				},
			},
			expected: true,
		},
		{
			name: "when owned object has owner reference but in different namespace then return false",
			owned: &corev1.Pod{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "ns1",
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion: "v1",
							Kind:       "Deployment",
							Name:       "my-deployment",
						},
					},
				},
			},
			owner: &appsv1.Deployment{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Deployment",
					APIVersion: "v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-deployment",
					Namespace: "ns2",
				},
			},
			expected: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual := IsOwnedBy(tc.owned, tc.owner)
			if actual != tc.expected {
				t.Errorf("unexpected result: got %v, want %v", actual, tc.expected)
			}
		})
	}
}

func TestGetClusterUID(t *testing.T) {
	var tScheme = runtime.NewScheme()
	utilruntime.Must(corev1.AddToScheme(tScheme))

	var testCases = []struct {
		Name       string
		Objects    []runtime.Object
		Validation func(t *testing.T, e error, id string)
	}{
		{
			Name:    "an absent namespace generates an error",
			Objects: []runtime.Object{},
			Validation: func(t *testing.T, e error, id string) {
				if !errors.IsNotFound(e) {
					t.Errorf("expected not found error, got '%v'", e)
				}
			},
		},
		{
			Name: "a UID generates a valid deterministic cluster ID",
			Objects: []runtime.Object{
				&corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{
						Name: clusterIDNamespace,
						UID:  "random-uid",
					},
				},
			},
			Validation: func(t *testing.T, e error, id string) {
				if e != nil {
					t.Errorf("unexpected error, got '%v', expected nil", e)
				}

				if id != "random-uid" {
					t.Errorf("unexpected cluster ID got '%s', expected 'random-uid'", id)
				}
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.Name, func(t *testing.T) {
			fc := dfake.NewSimpleDynamicClient(tScheme, testCase.Objects...)
			id, err := GetClusterUID(context.Background(), fc)
			testCase.Validation(t, err, id)
		})
	}
}
