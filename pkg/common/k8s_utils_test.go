//go:build unit
// +build unit

package common

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/kuadrant/limitador-operator/api/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestObjectKeyListDifference(t *testing.T) {

	key1 := client.ObjectKey{Namespace: "ns1", Name: "obj1"}
	key2 := client.ObjectKey{Namespace: "ns2", Name: "obj2"}
	key3 := client.ObjectKey{Namespace: "ns3", Name: "obj3"}
	key4 := client.ObjectKey{Namespace: "ns4", Name: "obj4"}

	testCases := []struct {
		name     string
		a        []client.ObjectKey
		b        []client.ObjectKey
		expected []client.ObjectKey
	}{
		{
			"when both input slices are empty then return an empty slice",
			[]client.ObjectKey{},
			[]client.ObjectKey{},
			[]client.ObjectKey{},
		},
		{
			"when inputA is empty and inputB has elements then return an empty slice",
			[]client.ObjectKey{},
			[]client.ObjectKey{key1},
			[]client.ObjectKey{},
		},
		{
			"when inputA has elements and inputB is empty then return inputA as the result",
			[]client.ObjectKey{key1, key2},
			[]client.ObjectKey{},
			[]client.ObjectKey{key1, key2},
		},
		{
			"when inputA and inputB are equal then return an empty slice",
			[]client.ObjectKey{key1, key2, key3},
			[]client.ObjectKey{key1, key2, key3},
			[]client.ObjectKey{},
		},
		{
			"when inputA and inputB have common elements then return the difference",
			[]client.ObjectKey{key1, key2, key3},
			[]client.ObjectKey{key1, key3},
			[]client.ObjectKey{key2},
		},
		{
			"when inputA and inputB have no common elements then return inputA as the result",
			[]client.ObjectKey{key1, key2},
			[]client.ObjectKey{key3, key4},
			[]client.ObjectKey{key1, key2},
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

	k8sClient := fake.NewClientBuilder().WithRuntimeObjects(service).Build()

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

	k8sClient := fake.NewClientBuilder().WithRuntimeObjects(service).Build()

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

func TestStatusConditionsMarshalJSON(t *testing.T) {
	now := time.Now()
	nowFmt := now.UTC().Format("2006-01-02T15:04:05Z")

	testCases := []struct {
		name     string
		input    []metav1.Condition
		expected []byte
		err      error
	}{
		{
			name:     "when input is empty then return an empty JSON (empty byte array)",
			input:    []metav1.Condition{},
			expected: []byte("[]"),
			err:      nil,
		},
		{
			name: "when input contains multiple conditions then return the sorted JSON array",
			input: []metav1.Condition{
				{
					Type:               "ConditionB",
					Status:             metav1.ConditionFalse,
					LastTransitionTime: metav1.Time{Time: now},
					Reason:             "ReasonB",
					Message:            "MessageB",
				},
				{
					Type:               "ConditionC",
					Status:             metav1.ConditionTrue,
					LastTransitionTime: metav1.Time{Time: now},
					Reason:             "ReasonC",
					Message:            "MessageC",
				},
				{
					Type:               "ConditionA",
					Status:             metav1.ConditionTrue,
					LastTransitionTime: metav1.Time{Time: now},
					Reason:             "ReasonA",
					Message:            "MessageA",
				},
			},
			expected: []byte(`[{"type":"ConditionA","status":"True","lastTransitionTime":"` + nowFmt + `","reason":"ReasonA","message":"MessageA"},{"type":"ConditionB","status":"False","lastTransitionTime":"` + nowFmt + `","reason":"ReasonB","message":"MessageB"},{"type":"ConditionC","status":"True","lastTransitionTime":"` + nowFmt + `","reason":"ReasonC","message":"MessageC"}]`),
			err:      nil,
		},
		{
			name: "when input contains condition with empty LastTransitionTime then return JSON with according null value",
			input: []metav1.Condition{
				{
					Type:    "ConditionA",
					Status:  metav1.ConditionTrue,
					Reason:  "ReasonA",
					Message: "MessageA",
				},
			},
			expected: []byte(`[{"type":"ConditionA","status":"True","lastTransitionTime":null,"reason":"ReasonA","message":"MessageA"}]`),
			err:      nil,
		},
		{
			name: "when input contains condition with empty Type, Status, Reason and Message then return JSON with according empty values",
			input: []metav1.Condition{
				{
					LastTransitionTime: metav1.Time{Time: now},
				},
			},
			expected: []byte(`[{"type":"","status":"","lastTransitionTime":"` + nowFmt + `","reason":"","message":""}]`),
			err:      nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			actual, err := StatusConditionsMarshalJSON(tc.input)
			if err != tc.err {
				t.Errorf("unexpected error: got %v, want %v", err, tc.err)
			}

			if !reflect.DeepEqual(actual, tc.expected) {
				t.Errorf("unexpected result: got %s, want %s", string(actual), string(tc.expected))
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

func TestGetServicePortNumber(t *testing.T) {
	ctx := context.TODO()
	k8sClient := fake.NewClientBuilder().Build()

	tests := []struct {
		name        string
		servicePort string
		expected    int32
		service     *corev1.Service
		serviceKey  client.ObjectKey
		expectedErr error
	}{
		{
			name:        "when port is already a number then return the number",
			servicePort: "8080",
			expected:    8080,
			serviceKey:  client.ObjectKey{Name: "my-service1", Namespace: "default"},
		},
		{
			name:        "when port is a named existing port then return the target port",
			servicePort: "http",
			expected:    8080,
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: "my-service2", Namespace: "default"},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{
							Name:       "http",
							Port:       80,
							TargetPort: intstr.FromInt(8080),
						},
					},
				},
			},
			serviceKey: client.ObjectKey{Name: "my-service2", Namespace: "default"},
		},
		{
			name:        "when port not found then return an error",
			servicePort: "unknown",
			expectedErr: fmt.Errorf("service port unknown was not found in default/my-service3"),
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: "my-service3", Namespace: "default"},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{
							Name:       "http",
							Port:       80,
							TargetPort: intstr.FromInt(8080),
						},
					},
				},
			},
			serviceKey: client.ObjectKey{Name: "my-service3", Namespace: "default"},
		},
		{
			name:        "when service not found then return an error",
			servicePort: "http",
			expectedErr: fmt.Errorf("services \"my-service4\" not found"),
			expected:    0,
			serviceKey:  client.ObjectKey{Name: "my-service4", Namespace: "default"},
		},
		{
			name:        "when multiple ports exist and the port is found then return the target port",
			servicePort: "http2",
			expected:    8090,
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: "my-service5", Namespace: "default"},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{
							Name:       "http1",
							Port:       8080,
							TargetPort: intstr.FromInt(8080),
						},
						{
							Name:       "http2",
							Port:       8090,
							TargetPort: intstr.FromInt(8090),
						},
						{
							Name:       "http3",
							Port:       8100,
							TargetPort: intstr.FromInt(8100),
						},
					},
				},
			},
			serviceKey: client.ObjectKey{Name: "my-service5", Namespace: "default"},
		},
		{
			name:        "when multiple ports exist and the port is not found then return an error",
			servicePort: "https2",
			expectedErr: fmt.Errorf("service port https2 was not found in default/my-service6"),
			service: &corev1.Service{
				ObjectMeta: metav1.ObjectMeta{Name: "my-service6", Namespace: "default"},
				Spec: corev1.ServiceSpec{
					Ports: []corev1.ServicePort{
						{
							Name:       "http1",
							Port:       8080,
							TargetPort: intstr.FromInt(8080),
						},
						{
							Name:       "http2",
							Port:       8090,
							TargetPort: intstr.FromInt(8090),
						},
						{
							Name:       "http3",
							Port:       8100,
							TargetPort: intstr.FromInt(8100),
						},
					},
				},
			},
			serviceKey: client.ObjectKey{Name: "my-service6", Namespace: "default"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.service != nil {
				err := k8sClient.Create(ctx, tt.service)
				if err != nil {
					t.Fatalf("failed to create service: %v", err)
				}
			}

			portNumber, err := GetServicePortNumber(ctx, k8sClient, tt.serviceKey, tt.servicePort)

			if err != nil && tt.expectedErr == nil {
				t.Errorf("unexpected error: %v", err)
			}
			if err == nil && tt.expectedErr != nil {
				t.Error("expected an error, but got nil")
			}
			if err != nil && tt.expectedErr != nil && err.Error() != tt.expectedErr.Error() {
				t.Errorf("unexpected error: got %v, want %v", err, tt.expectedErr)
			}

			if portNumber != tt.expected {
				t.Errorf("unexpected port number: got %d, want %d", portNumber, tt.expected)
			}
		})
	}
}
