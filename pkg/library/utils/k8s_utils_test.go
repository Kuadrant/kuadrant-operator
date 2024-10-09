//go:build unit

package utils

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kuadrant/limitador-operator/api/v1alpha1"
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
			name:     "when input is empty then return an empty JSON array",
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
							TargetPort: intstr.FromInt32(8080),
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
							TargetPort: intstr.FromInt32(8080),
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
							TargetPort: intstr.FromInt32(8080),
						},
						{
							Name:       "http2",
							Port:       8090,
							TargetPort: intstr.FromInt32(8090),
						},
						{
							Name:       "http3",
							Port:       8100,
							TargetPort: intstr.FromInt32(8100),
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
							TargetPort: intstr.FromInt32(8080),
						},
						{
							Name:       "http2",
							Port:       8090,
							TargetPort: intstr.FromInt32(8090),
						},
						{
							Name:       "http3",
							Port:       8100,
							TargetPort: intstr.FromInt32(8100),
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

func TestFindObjectKey(t *testing.T) {
	key1 := client.ObjectKey{Namespace: "ns1", Name: "obj1"}
	key2 := client.ObjectKey{Namespace: "ns2", Name: "obj2"}
	key3 := client.ObjectKey{Namespace: "ns3", Name: "obj3"}

	testCases := []struct {
		name     string
		list     []client.ObjectKey
		key      client.ObjectKey
		expected int
	}{
		{
			name:     "when input slice has one search ObjectKey then return its index",
			list:     []client.ObjectKey{key1, key2, key3},
			key:      key2,
			expected: 1,
		},
		{
			name:     "when input slice has no search ObjectKey then return length of input slice",
			list:     []client.ObjectKey{key1, key3},
			key:      key2,
			expected: 2,
		},
		{
			name:     "when input slice is empty then return 0",
			list:     []client.ObjectKey{},
			key:      key1,
			expected: 0,
		},
		{
			name:     "when there are multiple occurrences of the search ObjectKey then return the index of first occurrence",
			list:     []client.ObjectKey{key1, key2, key1, key3},
			key:      key2,
			expected: 1,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if output := FindObjectKey(tc.list, tc.key); output != tc.expected {
				t.Errorf("expected %d but got %d", tc.expected, output)
			}
		})
	}
}

func TestFindDeploymentStatusCondition(t *testing.T) {
	tests := []struct {
		name          string
		conditions    []appsv1.DeploymentCondition
		conditionType string
		expected      *appsv1.DeploymentCondition
	}{
		{
			name: "when search condition exists then return the condition",
			conditions: []appsv1.DeploymentCondition{
				{
					Type:   appsv1.DeploymentConditionType("Ready"),
					Status: corev1.ConditionTrue,
				},
				{
					Type:   appsv1.DeploymentConditionType("Progressing"),
					Status: corev1.ConditionFalse,
				},
			},
			conditionType: "Ready",
			expected: &appsv1.DeploymentCondition{
				Type:   appsv1.DeploymentConditionType("Ready"),
				Status: corev1.ConditionTrue,
			},
		},
		{
			name: "when search condition does not exist then return nil",
			conditions: []appsv1.DeploymentCondition{
				{
					Type:   appsv1.DeploymentConditionType("Progressing"),
					Status: corev1.ConditionFalse,
				},
			},
			conditionType: "Ready",
			expected:      nil,
		},
		{
			name:          "when conditions slice is empty then return nil",
			conditions:    []appsv1.DeploymentCondition{},
			conditionType: "Ready",
			expected:      nil,
		},
		{
			name: "when multiple conditions have the same type then return the first occurrence",
			conditions: []appsv1.DeploymentCondition{
				{
					Type:   appsv1.DeploymentConditionType("Ready"),
					Status: corev1.ConditionTrue,
				},
				{
					Type:   appsv1.DeploymentConditionType("Progressing"),
					Status: corev1.ConditionTrue,
				},
				{
					Type:   appsv1.DeploymentConditionType("Ready"),
					Status: corev1.ConditionFalse,
				},
			},
			conditionType: "Ready",
			expected: &appsv1.DeploymentCondition{
				Type:   appsv1.DeploymentConditionType("Ready"),
				Status: corev1.ConditionTrue,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			actual := FindDeploymentStatusCondition(tc.conditions, tc.conditionType)
			if !reflect.DeepEqual(actual, tc.expected) {
				t.Errorf("unexpected result: got %s, want %s", actual.String(), tc.expected.String())
			}
		})
	}
}

func TestHasLabel(t *testing.T) {
	testCases := []struct {
		name   string
		obj    metav1.Object
		label  string
		expect bool
	}{
		{
			name: "existing label found",
			obj: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-object",
					Labels: map[string]string{
						"test-key": "value",
					},
				},
			},
			label:  "test-key",
			expect: true,
		},
		{
			name: "existing label not found",
			obj: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-object",
					Labels: map[string]string{
						"test-fail": "value",
					},
				},
			},
			label:  "test-key",
			expect: false,
		},
		{
			name: "no labels",
			obj: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "test-object",
					Labels: nil,
				},
			},
			label:  "test-key",
			expect: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := HasLabel(tc.obj, tc.label)
			if got != tc.expect {
				t.Errorf("expected '%v' got '%v'", tc.expect, got)
			}
		})
	}
}

func TestGetLabel(t *testing.T) {
	testCases := []struct {
		name   string
		obj    metav1.Object
		label  string
		expect string
	}{
		{
			name: "existing label found",
			obj: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-object",
					Labels: map[string]string{
						"test-key": "value",
					},
				},
			},
			label:  "test-key",
			expect: "value",
		},
		{
			name: "existing label not found",
			obj: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-object",
					Labels: map[string]string{
						"test-fail": "value",
					},
				},
			},
			label:  "test-key",
			expect: "",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := GetLabel(tc.obj, tc.label)
			if got != tc.expect {
				t.Errorf("expected '%v' got '%v'", tc.expect, got)
			}
		})
	}
}

func TestGetClusterUID(t *testing.T) {
	var testCases = []struct {
		Name       string
		Objects    []client.Object
		Validation func(t *testing.T, e error, id string)
	}{
		{
			Name:    "an absent namespace generates an error",
			Objects: []client.Object{},
			Validation: func(t *testing.T, e error, id string) {
				if !errors.IsNotFound(e) {
					t.Errorf("expected not found error, got '%v'", e)
				}
			},
		},
		{
			Name: "a UID generates a valid deterministic cluster ID",
			Objects: []client.Object{
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
			fc := fake.NewClientBuilder().WithObjects(testCase.Objects...).Build()
			id, err := GetClusterUID(context.Background(), fc)
			testCase.Validation(t, err, id)
		})
	}
}
