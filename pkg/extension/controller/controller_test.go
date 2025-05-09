//go:build unit

package controller

import (
	"context"
	"errors"
	"os"
	"testing"

	celtypes "github.com/google/cel-go/common/types"
	"github.com/google/cel-go/common/types/ref"
	"gotest.tools/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	exttypes "github.com/kuadrant/kuadrant-operator/pkg/extension/types"
)

type mockKuadrantCtx struct {
	resolveFn func(ctx context.Context, policy exttypes.Policy, expression string, subscribe bool) (ref.Val, error)
}

func (m *mockKuadrantCtx) Resolve(ctx context.Context, policy exttypes.Policy, expression string, subscribe bool) (ref.Val, error) {
	return m.resolveFn(ctx, policy, expression, subscribe)
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
	expectedErr := errors.New("resolve failure")
	mockCtx := &mockKuadrantCtx{
		resolveFn: func(ctx context.Context, policy exttypes.Policy, expression string, subscribe bool) (ref.Val, error) {
			return nil, expectedErr
		},
	}

	_, err := Resolve[int](context.Background(), mockCtx, nil, "some.expression", false)
	assert.Error(t, err, expectedErr.Error())
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

func TestBuilderMissingWatchTypes(t *testing.T) {
	builder, _ := NewBuilder("test-controller")
	_, err := builder.
		WithScheme(runtime.NewScheme()).
		WithReconciler(mockReconcile).
		Build()
	assert.ErrorContains(t, err, "watch sources must be set")
}

func TestBuilderMissingSocketPath(t *testing.T) {
	originalArgs := os.Args
	defer func() { os.Args = originalArgs }()
	os.Args = []string{"my-extension"} // no socket path

	builder, _ := NewBuilder("test-controller")
	_, err := builder.
		WithScheme(runtime.NewScheme()).
		WithReconciler(mockReconcile).
		Watches(&corev1.Pod{}).
		Build()

	assert.ErrorContains(t, err, "missing socket path")
}
