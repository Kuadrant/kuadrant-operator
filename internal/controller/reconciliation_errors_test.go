package controllers

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/kuadrant/policy-machinery/controller"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8stypes "k8s.io/apimachinery/pkg/types"
)

func TestErrorRegistry_Record(t *testing.T) {
	registry := NewErrorRegistry()

	resource := k8stypes.NamespacedName{Namespace: "test-ns", Name: "test-resource"}
	kind := schema.GroupKind{Group: "test.io", Kind: "TestKind"}
	err := errors.New("test error")

	registry.Record("TestReconciler", "delete", resource, kind, err)

	if !registry.HasErrors() {
		t.Error("Expected registry to have errors")
	}

	errors := registry.GetErrors()
	if len(errors) != 1 {
		t.Errorf("Expected 1 error, got %d", len(errors))
	}

	recErr := errors[0]
	if recErr.Source != "TestReconciler" {
		t.Errorf("Expected source 'TestReconciler', got '%s'", recErr.Source)
	}
	if recErr.Operation != "delete" {
		t.Errorf("Expected operation 'delete', got '%s'", recErr.Operation)
	}
	if recErr.Resource != resource {
		t.Errorf("Expected resource %v, got %v", resource, recErr.Resource)
	}
	if recErr.ResourceKind != kind {
		t.Errorf("Expected kind %v, got %v", kind, recErr.ResourceKind)
	}
}

func TestReconciliationError_Key(t *testing.T) {
	err := &ReconciliationError{
		Resource:     k8stypes.NamespacedName{Namespace: "ns", Name: "name"},
		ResourceKind: schema.GroupKind{Group: "test.io", Kind: "TestKind"},
		Operation:    "delete",
	}

	expected := "TestKind.test.io/ns/name/delete"
	if err.Key() != expected {
		t.Errorf("Expected key '%s', got '%s'", expected, err.Key())
	}
}

func TestReconciliationError_NextRetryDelay(t *testing.T) {
	tests := []struct {
		name       string
		retryCount int
		expected   time.Duration
	}{
		{"first retry", 0, 2 * time.Second},
		{"second retry", 1, 4 * time.Second},
		{"third retry", 2, 8 * time.Second},
		{"fourth retry", 3, 16 * time.Second},
		{"fifth retry", 4, 32 * time.Second},
		{"capped at max", 5, 1 * time.Minute}, // Should be capped at maxRetryDelay
		{"capped at max", 10, 1 * time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := &ReconciliationError{RetryCount: tt.retryCount}
			delay := err.NextRetryDelay()
			if delay != tt.expected {
				t.Errorf("Expected delay %v, got %v", tt.expected, delay)
			}
		})
	}
}

func TestReconciliationError_ShouldRetry(t *testing.T) {
	tests := []struct {
		name       string
		retryCount int
		expected   bool
	}{
		{"under limit", 0, true},
		{"under limit", 4, true},
		{"at limit", 5, false},
		{"over limit", 6, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := &ReconciliationError{RetryCount: tt.retryCount}
			if err.ShouldRetry() != tt.expected {
				t.Errorf("Expected ShouldRetry() = %v, got %v", tt.expected, err.ShouldRetry())
			}
		})
	}
}

func TestPersistentErrorTracker_UpdateFromRegistry(t *testing.T) {
	logger := logr.Discard()
	tracker := NewPersistentErrorTracker(logger)

	resource1 := k8stypes.NamespacedName{Namespace: "ns", Name: "resource1"}
	resource2 := k8stypes.NamespacedName{Namespace: "ns", Name: "resource2"}
	kind := schema.GroupKind{Group: "test.io", Kind: "TestKind"}

	// First reconciliation - record two errors
	registry1 := NewErrorRegistry()
	registry1.Record("TestReconciler", "delete", resource1, kind, errors.New("error1"))
	registry1.Record("TestReconciler", "delete", resource2, kind, errors.New("error2"))

	tracker.UpdateFromRegistry(registry1)

	if tracker.GetErrorCount() != 2 {
		t.Errorf("Expected 2 errors, got %d", tracker.GetErrorCount())
	}

	// Second reconciliation - resource1 still fails, resource2 succeeds
	registry2 := NewErrorRegistry()
	registry2.Record("TestReconciler", "delete", resource1, kind, errors.New("error1 still failing"))

	tracker.UpdateFromRegistry(registry2)

	// Should have 1 error (resource2 was resolved)
	if tracker.GetErrorCount() != 1 {
		t.Errorf("Expected 1 error after resolution, got %d", tracker.GetErrorCount())
	}

	// Check that retry count was incremented for resource1
	tracker.mu.RLock()
	key1 := "TestKind.test.io/ns/resource1/delete"
	if err, found := tracker.errors[key1]; !found {
		t.Error("Expected resource1 error to still be tracked")
	} else if err.RetryCount != 1 {
		t.Errorf("Expected retry count 1, got %d", err.RetryCount)
	}
	tracker.mu.RUnlock()
}

func TestPersistentErrorTracker_ShouldRequeue(t *testing.T) {
	logger := logr.Discard()
	tracker := NewPersistentErrorTracker(logger)

	// No errors - should not requeue
	if delay := tracker.ShouldRequeue(); delay != 0 {
		t.Errorf("Expected no requeue with no errors, got %v", delay)
	}

	// Add error with retry count 0 - should requeue after 2s
	registry := NewErrorRegistry()
	resource := k8stypes.NamespacedName{Namespace: "ns", Name: "resource"}
	kind := schema.GroupKind{Group: "test.io", Kind: "TestKind"}
	registry.Record("TestReconciler", "delete", resource, kind, errors.New("error"))

	tracker.UpdateFromRegistry(registry)

	delay := tracker.ShouldRequeue()
	if delay != 2*time.Second {
		t.Errorf("Expected 2s requeue delay, got %v", delay)
	}

	// Simulate another failure - retry count should be 1, delay 4s
	registry2 := NewErrorRegistry()
	registry2.Record("TestReconciler", "delete", resource, kind, errors.New("error"))
	tracker.UpdateFromRegistry(registry2)

	delay = tracker.ShouldRequeue()
	if delay != 4*time.Second {
		t.Errorf("Expected 4s requeue delay, got %v", delay)
	}

	// Exceed max retries - should not requeue
	for i := 0; i < 5; i++ {
		reg := NewErrorRegistry()
		reg.Record("TestReconciler", "delete", resource, kind, errors.New("error"))
		tracker.UpdateFromRegistry(reg)
	}

	delay = tracker.ShouldRequeue()
	if delay != 0 {
		t.Errorf("Expected no requeue after max retries, got %v", delay)
	}
}

func TestReconciliationErrorHandler_Reconcile(t *testing.T) {
	logger := logr.Discard()
	tracker := NewPersistentErrorTracker(logger)

	// Track if retry was scheduled
	var scheduledDelay time.Duration
	scheduleRetry := func(delay time.Duration, _ []controller.ResourceEvent) {
		scheduledDelay = delay
	}

	handler := NewReconciliationErrorHandler(tracker, scheduleRetry)

	ctx := context.Background()
	ctx = controller.LoggerIntoContext(ctx, logger)
	state := &sync.Map{}

	// No errors - should not set requeue
	err := handler.Reconcile(ctx, nil, nil, nil, state)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	if _, ok := state.Load(StateRequeueAfter); ok {
		t.Error("Expected no requeue decision with no errors")
	}

	// Add error to registry
	registry := GetOrCreateErrorRegistry(state)
	resource := k8stypes.NamespacedName{Namespace: "ns", Name: "resource"}
	kind := schema.GroupKind{Group: "test.io", Kind: "TestKind"}
	registry.Record("TestReconciler", "delete", resource, kind, errors.New("error"))

	// Run handler
	err = handler.Reconcile(ctx, nil, nil, nil, state)
	if err != nil {
		t.Errorf("Expected no error, got %v", err)
	}

	// Should have scheduled retry via callback
	if scheduledDelay != 2*time.Second {
		t.Errorf("Expected retry scheduled with 2s delay, got %v", scheduledDelay)
	}
}

func TestGetOrCreateErrorRegistry(t *testing.T) {
	state := &sync.Map{}

	// First call should create registry
	registry1 := GetOrCreateErrorRegistry(state)
	if registry1 == nil {
		t.Error("Expected registry to be created")
	}

	// Second call should return same registry
	registry2 := GetOrCreateErrorRegistry(state)
	if registry1 != registry2 {
		t.Error("Expected same registry instance")
	}

	// Should work with nil state
	registry3 := GetOrCreateErrorRegistry(nil)
	if registry3 == nil {
		t.Error("Expected registry to be created even with nil state")
	}
}

func TestPersistentErrorTracker_MinimumDelay(t *testing.T) {
	logger := logr.Discard()
	tracker := NewPersistentErrorTracker(logger)

	// Add two errors with different retry counts
	resource1 := k8stypes.NamespacedName{Namespace: "ns", Name: "resource1"}
	resource2 := k8stypes.NamespacedName{Namespace: "ns", Name: "resource2"}
	kind := schema.GroupKind{Group: "test.io", Kind: "TestKind"}

	// Add both errors initially
	registry1 := NewErrorRegistry()
	registry1.Record("TestReconciler", "delete", resource1, kind, errors.New("error1"))
	registry1.Record("TestReconciler", "delete", resource2, kind, errors.New("error2"))
	tracker.UpdateFromRegistry(registry1)

	// Manually set different retry counts
	tracker.mu.Lock()
	key1 := "TestKind.test.io/ns/resource1/delete"
	key2 := "TestKind.test.io/ns/resource2/delete"
	if err, found := tracker.errors[key1]; found {
		err.RetryCount = 0 // 2s delay
	}
	if err, found := tracker.errors[key2]; found {
		err.RetryCount = 2 // 8s delay
	}
	tracker.mu.Unlock()

	// Should requeue after minimum delay (2s from resource1)
	delay := tracker.ShouldRequeue()
	if delay != 2*time.Second {
		t.Errorf("Expected minimum delay of 2s, got %v", delay)
	}
}
