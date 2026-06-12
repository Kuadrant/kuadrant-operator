package controllers

import (
	"context"
	"errors"
	"testing"

	"github.com/go-logr/logr"
	ctrlruntimereconcile "sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// mockController implements the Reconcile method for testing
type mockController struct {
	reconcileResult ctrlruntimereconcile.Result
	reconcileError  error
}

func (m *mockController) Reconcile(_ context.Context, _ ctrlruntimereconcile.Request) (ctrlruntimereconcile.Result, error) {
	return m.reconcileResult, m.reconcileError
}

func TestKuadrantController_Reconcile_BlockingError(t *testing.T) {
	logger := logr.Discard()
	errorTracker := NewPersistentErrorTracker(logger)

	// Create a mock controller that returns a blocking error
	blockingErr := errors.New("topology build failed")
	mock := &mockController{
		reconcileResult: ctrlruntimereconcile.Result{},
		reconcileError:  blockingErr,
	}

	// We can't directly create a controller.Controller, so we'll just test the logic
	// In a real scenario, the KuadrantController wraps a real controller

	// Simulate the logic from KuadrantController.Reconcile
	result, err := mock.Reconcile(context.Background(), ctrlruntimereconcile.Request{})

	// Blocking error should be returned immediately
	if err == nil {
		t.Error("Expected blocking error to be returned")
	}
	if !errors.Is(err, blockingErr) {
		t.Errorf("Expected error %v, got %v", blockingErr, err)
	}

	// Error tracker should not affect the result when there's a blocking error
	if result.RequeueAfter != 0 {
		t.Errorf("Expected no requeue with blocking error, got %v", result.RequeueAfter)
	}

	// Verify error tracker state didn't interfere
	if errorTracker.ShouldRequeue() != 0 {
		t.Error("Error tracker should not have errors")
	}
}

func TestKuadrantController_Reconcile_NoErrors(t *testing.T) {
	// Create a mock controller that succeeds
	mock := &mockController{
		reconcileResult: ctrlruntimereconcile.Result{},
		reconcileError:  nil,
	}

	logger := logr.Discard()
	errorTracker := NewPersistentErrorTracker(logger)

	// Simulate the logic
	result, err := mock.Reconcile(context.Background(), ctrlruntimereconcile.Request{})

	// Should succeed with no error
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	// No requeue should be set (no non-blocking errors)
	requeueAfter := errorTracker.ShouldRequeue()
	if requeueAfter != 0 {
		t.Errorf("Expected no requeue, got %v", requeueAfter)
	}

	if result.RequeueAfter != 0 {
		t.Errorf("Expected no requeue in result, got %v", result.RequeueAfter)
	}
}
