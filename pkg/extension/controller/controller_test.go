//go:build unit

package controller

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/client"

	"reflect"

	celtypes "github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"gotest.tools/assert"
	"gotest.tools/assert/cmp"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	extpb "github.com/kuadrant/kuadrant-operator/pkg/extension/grpc/v1"
	exttypes "github.com/kuadrant/kuadrant-operator/pkg/extension/types"
)

type mockKuadrantCtx struct {
	resolveFn       func(ctx context.Context, policy exttypes.Policy, expression string, subscribe bool) (ref.Val, error)
	resolvePolicyFn func(ctx context.Context, policy exttypes.Policy, expression string, subscribe bool) (exttypes.Policy, error)
	addDataToFn     func(ctx context.Context, requester exttypes.Policy, target exttypes.Policy, binding string, expression string) error
}

type mockPolicy struct {
	name      string
	namespace string
}

func (m *mockPolicy) GetName() string                  { return m.name }
func (m *mockPolicy) GetNamespace() string             { return m.namespace }
func (m *mockPolicy) GetObjectKind() schema.ObjectKind { return &mockObjectKind{} }
func (m *mockPolicy) GetTargetRefs() []gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName {
	return nil
}

type mockObjectKind struct{}

func (m *mockObjectKind) SetGroupVersionKind(gvk schema.GroupVersionKind) {}
func (m *mockObjectKind) GroupVersionKind() schema.GroupVersionKind {
	return schema.GroupVersionKind{Group: "kuadrant.io", Kind: "TestPolicy"}
}

type mockCelValue struct {
	pbPolicy *extpb.Policy
}

func (m *mockCelValue) ConvertToNative(typeDesc reflect.Type) (interface{}, error) {
	if typeDesc == reflect.TypeOf((*extpb.Policy)(nil)).Elem() {
		return m.pbPolicy, nil
	}
	return nil, fmt.Errorf("unsupported conversion to %v", typeDesc)
}

func (m *mockCelValue) ConvertToType(typeVal ref.Type) ref.Val { return m }
func (m *mockCelValue) Equal(other ref.Val) ref.Val            { return celtypes.Bool(false) }
func (m *mockCelValue) Type() ref.Type                         { return celtypes.TypeType }
func (m *mockCelValue) Value() interface{}                     { return m.pbPolicy }

func (m *mockKuadrantCtx) Resolve(ctx context.Context, policy exttypes.Policy, expression string, subscribe bool) (ref.Val, error) {
	return m.resolveFn(ctx, policy, expression, subscribe)
}

func (m *mockKuadrantCtx) ResolvePolicy(ctx context.Context, policy exttypes.Policy, expression string, subscribe bool) (exttypes.Policy, error) {
	if m.resolvePolicyFn != nil {
		return m.resolvePolicyFn(ctx, policy, expression, subscribe)
	}
	return nil, fmt.Errorf("resolvePolicyFn not implemented in mock")
}

func (m *mockKuadrantCtx) AddDataTo(ctx context.Context, requester exttypes.Policy, target exttypes.Policy, binding string, expression string) error {
	return m.addDataToFn(ctx, requester, target, binding, expression)
}

func (m *mockKuadrantCtx) GetClient() client.Client {
	return nil
}

func (m *mockKuadrantCtx) GetScheme() *runtime.Scheme {
	return &runtime.Scheme{}
}

func (m *mockKuadrantCtx) ReconcileObject(ctx context.Context, obj, desired client.Object, mutateFn exttypes.MutateFn) error {
	return nil
}

func TestGenericResolveSuccess(t *testing.T) {
	mockCtx := &mockKuadrantCtx{
		resolveFn: func(ctx context.Context, policy exttypes.Policy, expression string, subscribe bool) (ref.Val, error) {
			return celtypes.Int(42), nil
		},
	}

	result, err := Resolve[int](context.Background(), mockCtx, nil, "some.expression", false)
	assert.NilError(t, err)
	assert.Equal(t, 42, result)
}

func TestGenericResolveTypeMismatch(t *testing.T) {
	mockCtx := &mockKuadrantCtx{
		resolveFn: func(ctx context.Context, policy exttypes.Policy, expression string, subscribe bool) (ref.Val, error) {
			return celtypes.String("not-an-int"), nil
		},
	}

	_, err := Resolve[int](context.Background(), mockCtx, nil, "some.expression", false)
	assert.ErrorContains(t, err, "unsupported native conversion")
}

func TestGenericResolveError(t *testing.T) {
	expectedErr := errors.New("resolve error")
	mockCtx := &mockKuadrantCtx{
		resolveFn: func(ctx context.Context, policy exttypes.Policy, expression string, subscribe bool) (ref.Val, error) {
			return nil, expectedErr
		},
	}

	_, err := Resolve[int](context.Background(), mockCtx, &mockPolicy{}, "expression", false)
	assert.Assert(t, err != nil)
	assert.Assert(t, cmp.Contains(err.Error(), expectedErr.Error()))
}

func TestResolvePolicy(t *testing.T) {
	expectedPbPolicy := &extpb.Policy{
		Metadata: &extpb.Metadata{
			Name:      "test-policy",
			Namespace: "test-namespace",
			Group:     "kuadrant.io",
			Kind:      "AuthPolicy",
		},
		TargetRefs: []*extpb.TargetRef{
			{
				Group:       "gateway.networking.k8s.io",
				Kind:        "Gateway",
				Name:        "test-gateway",
				Namespace:   "test-namespace",
				SectionName: "http",
			},
		},
	}

	mockCtx := &mockKuadrantCtx{
		resolveFn: func(ctx context.Context, policy exttypes.Policy, expression string, subscribe bool) (ref.Val, error) {
			return &mockCelValue{pbPolicy: expectedPbPolicy}, nil
		},
		resolvePolicyFn: func(ctx context.Context, policy exttypes.Policy, expression string, subscribe bool) (exttypes.Policy, error) {
			return extpb.NewPolicyAdapter(expectedPbPolicy), nil
		},
	}

	// returns types.Policy interface
	policy, err := mockCtx.ResolvePolicy(context.Background(), &mockPolicy{}, "self.findAuthPolicies()[0]", true)
	if err != nil {
		t.Logf("ResolvePolicy error: %v", err)
	}
	assert.Assert(t, err == nil)
	assert.Assert(t, policy != nil)

	assert.Equal(t, policy.GetName(), "test-policy")
	assert.Equal(t, policy.GetNamespace(), "test-namespace")
	assert.Equal(t, policy.GetObjectKind().GroupVersionKind().Group, "kuadrant.io")
	assert.Equal(t, policy.GetObjectKind().GroupVersionKind().Kind, "AuthPolicy")

	targetRefs := policy.GetTargetRefs()
	assert.Assert(t, cmp.Len(targetRefs, 1))
	assert.Equal(t, string(targetRefs[0].Group), "gateway.networking.k8s.io")
	assert.Equal(t, string(targetRefs[0].Kind), "Gateway")
	assert.Equal(t, string(targetRefs[0].Name), "test-gateway")
	assert.Equal(t, string(*targetRefs[0].SectionName), "http")
}

func TestPolicyAdapterMethods(t *testing.T) {
	pbPolicy := &extpb.Policy{
		Metadata: &extpb.Metadata{
			Name:      "auth-policy",
			Namespace: "default",
			Group:     "kuadrant.io",
			Kind:      "AuthPolicy",
		},
		TargetRefs: []*extpb.TargetRef{
			{
				Group:     "gateway.networking.k8s.io",
				Kind:      "HTTPRoute",
				Name:      "test-route",
				Namespace: "default",
			},
		},
	}

	adapter := extpb.NewPolicyAdapter(pbPolicy)

	assert.Equal(t, adapter.GetName(), "auth-policy")
	assert.Equal(t, adapter.GetNamespace(), "default")
	gvk := adapter.GetObjectKind().GroupVersionKind()
	assert.Equal(t, gvk.Group, "kuadrant.io")
	assert.Equal(t, gvk.Kind, "AuthPolicy")

	targetRefs := adapter.GetTargetRefs()
	assert.Assert(t, cmp.Len(targetRefs, 1))
	assert.Equal(t, string(targetRefs[0].Group), "gateway.networking.k8s.io")
	assert.Equal(t, string(targetRefs[0].Kind), "HTTPRoute")
	assert.Equal(t, string(targetRefs[0].Name), "test-route")
	assert.Assert(t, targetRefs[0].SectionName == nil)
}

func TestPolicyAdapterNilSafety(t *testing.T) {
	adapter := extpb.NewPolicyAdapter(nil)
	assert.Equal(t, adapter.GetName(), "")
	assert.Equal(t, adapter.GetNamespace(), "")
	assert.Assert(t, adapter.GetTargetRefs() == nil)

	pbPolicy := &extpb.Policy{
		Metadata: nil,
	}
	adapter = extpb.NewPolicyAdapter(pbPolicy)
	assert.Equal(t, adapter.GetName(), "")
	assert.Equal(t, adapter.GetNamespace(), "")
}

func mockReconcile(_ context.Context, _ reconcile.Request, _ exttypes.KuadrantCtx) (reconcile.Result, error) {
	return reconcile.Result{}, nil
}

func setupFakeArgs(t *testing.T, socketPath string) func() {
	t.Helper()
	originalArgs := os.Args
	os.Args = []string{"my-extension", socketPath}
	return func() { os.Args = originalArgs }
}

func TestBuilderMissingName(t *testing.T) {
	builder := &Builder{}
	_, err := builder.Build()
	assert.ErrorContains(t, err, "controller name must be set")
}

func TestBuilderMissingScheme(t *testing.T) {
	builder, _ := NewBuilder("test-controller")
	_, err := builder.Build()
	assert.ErrorContains(t, err, "scheme must be set")
}

func TestBuilderBuildMissingReconcile(t *testing.T) {
	builder, _ := NewBuilder("test-controller")
	_, err := builder.
		WithScheme(runtime.NewScheme()).
		Build()
	assert.ErrorContains(t, err, "reconcile function must be set")
}

func TestBuilderMissingForType(t *testing.T) {
	builder, _ := NewBuilder("test-controller")
	_, err := builder.
		WithScheme(runtime.NewScheme()).
		WithReconciler(mockReconcile).
		Build()
	assert.ErrorContains(t, err, "for type must be set")
}

func TestBuilderMissingSocketPath(t *testing.T) {
	originalArgs := os.Args
	defer func() { os.Args = originalArgs }()
	os.Args = []string{"my-extension"} // no socket path

	builder, _ := NewBuilder("test-controller")
	_, err := builder.
		WithScheme(runtime.NewScheme()).
		WithReconciler(mockReconcile).
		For(&corev1.Pod{}).
		Build()

	assert.ErrorContains(t, err, "missing socket path")
}
