//go:build unit

package utils

import (
	"context"
	"testing"

	"gotest.tools/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/utils/ptr"

	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

func TestDynamicClientFromContext_Success(t *testing.T) {
	expectedClient := &dynamic.DynamicClient{}
	ctx := context.WithValue(context.Background(), (*dynamic.DynamicClient)(nil), expectedClient)

	client, err := DynamicClientFromContext(ctx)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if client != expectedClient {
		t.Errorf("expected client %v, got %v", expectedClient, client)
	}
}

func TestDynamicClientFromContext_Missing(t *testing.T) {
	ctx := context.Background()

	client, err := DynamicClientFromContext(ctx)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if client != nil {
		t.Errorf("expected nil client, got %v", client)
	}
}

type TestPolicy struct {
	metav1.ObjectMeta
	GroupVersionKind schema.GroupVersionKind
	TargetRefs       []gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName
}

type mockObjectKind struct {
	gvk schema.GroupVersionKind
}

func (m *mockObjectKind) SetGroupVersionKind(gvk schema.GroupVersionKind) {
	m.gvk = gvk
}

func (m *mockObjectKind) GroupVersionKind() schema.GroupVersionKind {
	return m.gvk
}

func (tp *TestPolicy) GetObjectKind() schema.ObjectKind {
	return &mockObjectKind{gvk: tp.GroupVersionKind}
}

func (tp *TestPolicy) GetTargetRefs() []gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName {
	return tp.TargetRefs
}

func (tp *TestPolicy) DeepCopyObject() runtime.Object {
	copy := *tp
	return &copy
}

func TestMapToExtPolicy(t *testing.T) {

	p := &TestPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: "default",
			Name:      "test-policy",
		},
		GroupVersionKind: schema.GroupVersionKind{Group: "example.group", Kind: "ExamplePolicy"},
		TargetRefs: []gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
			gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
				LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
					Group: "my.group",
					Kind:  "MyKind",
					Name:  "example-name",
				},
				SectionName: ptr.To[gatewayapiv1alpha2.SectionName]("some-section"),
			},
		},
	}
	result := MapToExtPolicy(p)

	assert.Assert(t, result != nil, "expected non-nil result")
	assert.Assert(t, result.Metadata != nil, "expected metadata to be populated")

	assert.Equal(t, "default", result.Metadata.Namespace)
	assert.Equal(t, "test-policy", result.Metadata.Name)
	assert.Equal(t, "example.group", result.Metadata.Group)
	assert.Equal(t, "ExamplePolicy", result.Metadata.Kind)

	assert.Equal(t, len(result.TargetRefs), 1)
	assert.Equal(t, "my.group", result.TargetRefs[0].Group)
	assert.Equal(t, "MyKind", result.TargetRefs[0].Kind)
	assert.Equal(t, "example-name", result.TargetRefs[0].Name)
	assert.Equal(t, "some-section", result.TargetRefs[0].SectionName)
}
