package controllers

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
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
	registry2.MarkReconciled("TestReconciler")

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

func TestPersistentErrorTracker_UpdateFromRegistry_SourceNotReconciled(t *testing.T) {
	logger := logr.Discard()
	tracker := NewPersistentErrorTracker(logger)

	resource := k8stypes.NamespacedName{Namespace: "ns", Name: "resource"}
	kind := schema.GroupKind{Group: "test.io", Kind: "TestKind"}
	key := "TestKind.test.io/ns/resource/create"

	// First reconciliation - ReconcilerA fails to create the resource
	registry1 := NewErrorRegistry()
	registry1.Record("ReconcilerA", "create", resource, kind, errors.New("forbidden"))
	registry1.MarkReconciled("ReconcilerA")
	tracker.UpdateFromRegistry(registry1)

	// Second reconciliation - triggered by an unrelated event, only ReconcilerB runs
	registry2 := NewErrorRegistry()
	registry2.MarkReconciled("ReconcilerB")
	tracker.UpdateFromRegistry(registry2)

	// Third reconciliation - ReconcilerA returns an error before retrying the create,
	// so it is not marked as reconciled
	tracker.UpdateFromRegistry(NewErrorRegistry())

	// The error must still be tracked, untouched, since ReconcilerA never retried the operation
	if tracker.GetErrorCount() != 1 {
		t.Fatalf("Expected error to remain tracked while its source did not run, got %d errors", tracker.GetErrorCount())
	}
	tracker.mu.RLock()
	if err, found := tracker.errors[key]; !found {
		t.Error("Expected error to still be tracked")
	} else if err.RetryCount != 0 {
		t.Errorf("Expected retry count 0 for an error that was not retried, got %d", err.RetryCount)
	}
	tracker.mu.RUnlock()
	if delay := tracker.ShouldRequeue(); delay != 2*time.Second {
		t.Errorf("Expected 2s requeue delay, got %v", delay)
	}

	// Fourth reconciliation - ReconcilerA runs and no longer records the error
	registry4 := NewErrorRegistry()
	registry4.MarkReconciled("ReconcilerA")
	tracker.UpdateFromRegistry(registry4)

	if tracker.GetErrorCount() != 0 {
		t.Errorf("Expected error to be resolved once its source ran without recording it, got %d errors", tracker.GetErrorCount())
	}
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

	// Exceed max retries - error should be removed from tracker
	// We've already done 2 updates above (RetryCount is now 1)
	// Need 4 more to reach RetryCount = 5, which triggers deletion
	for i := 0; i < 4; i++ {
		reg := NewErrorRegistry()
		reg.Record("TestReconciler", "delete", resource, kind, errors.New("error"))
		tracker.UpdateFromRegistry(reg)
	}

	// After exceeding max retries, the error should be removed
	if tracker.GetErrorCount() != 0 {
		t.Errorf("Expected error to be removed after max retries, got %d errors", tracker.GetErrorCount())
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

func TestErrorTrackingReconcileFunc(t *testing.T) {
	ctx := context.Background()
	reconcileErr := errors.New("reconcile error")

	tests := []struct {
		name               string
		reconcileErr       error
		expectedReconciled bool
	}{
		{"marks source as reconciled on success", nil, true},
		{"does not mark source as reconciled on error", reconcileErr, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := &sync.Map{}
			called := false
			reconcileFunc := errorTrackingReconcileFunc("TestReconciler", func(_ context.Context, _ []controller.ResourceEvent, _ *machinery.Topology, _ error, s *sync.Map) error {
				called = true
				if s != state {
					t.Error("Expected state to be passed through to the wrapped reconcile func")
				}
				return tt.reconcileErr
			})

			if err := reconcileFunc(ctx, nil, nil, nil, state); !errors.Is(err, tt.reconcileErr) {
				t.Errorf("Expected error %v, got %v", tt.reconcileErr, err)
			}
			if !called {
				t.Error("Expected wrapped reconcile func to be called")
			}

			registry := GetOrCreateErrorRegistry(state)
			if registry.WasReconciled("TestReconciler") != tt.expectedReconciled {
				t.Errorf("Expected WasReconciled() = %v, got %v", tt.expectedReconciled, registry.WasReconciled("TestReconciler"))
			}
			if registry.WasReconciled("OtherReconciler") {
				t.Error("Expected other sources not to be marked as reconciled")
			}
		})
	}
}

func TestReconciliationErrorHandler_UnrelatedEventDoesNotResolveError(t *testing.T) {
	logger := logr.Discard()
	tracker := NewPersistentErrorTracker(logger)
	handler := NewReconciliationErrorHandler(tracker, nil)

	ctx := controller.LoggerIntoContext(context.Background(), logger)
	resource := k8stypes.NamespacedName{Namespace: "ns", Name: "resource"}
	subscribedKind := schema.GroupKind{Group: "test.io", Kind: "SubscribedKind"}
	unrelatedKind := schema.GroupKind{Group: "test.io", Kind: "UnrelatedKind"}

	failing := true
	subscription := controller.Subscription{
		ReconcileFunc: errorTrackingReconcileFunc("TestReconciler", func(_ context.Context, _ []controller.ResourceEvent, _ *machinery.Topology, _ error, state *sync.Map) error {
			if failing {
				GetOrCreateErrorRegistry(state).Record("TestReconciler", OperationCreate, resource, subscribedKind, errors.New("forbidden"))
			}
			return nil
		}),
		Events: []controller.ResourceEventMatcher{{Kind: &subscribedKind}},
	}

	reconcile := func(kind schema.GroupKind) {
		events := []controller.ResourceEvent{{Kind: kind, EventType: controller.UpdateEvent, NewObject: &unstructured.Unstructured{}}}
		state := &sync.Map{}
		if err := subscription.Reconcile(ctx, events, nil, nil, state); err != nil {
			t.Fatalf("Expected no error from subscription, got %v", err)
		}
		if err := handler.Reconcile(ctx, events, nil, nil, state); err != nil {
			t.Fatalf("Expected no error from handler, got %v", err)
		}
	}

	// The subscribed reconciler runs and fails
	reconcile(subscribedKind)
	if tracker.GetErrorCount() != 1 {
		t.Fatalf("Expected 1 tracked error, got %d", tracker.GetErrorCount())
	}

	// An unrelated event does not run the subscribed reconciler, so its error must not be resolved
	reconcile(unrelatedKind)
	if tracker.GetErrorCount() != 1 {
		t.Fatalf("Expected error to remain tracked after an unrelated reconciliation, got %d errors", tracker.GetErrorCount())
	}

	// The subscribed reconciler runs again and the operation now succeeds
	failing = false
	reconcile(subscribedKind)
	if tracker.GetErrorCount() != 0 {
		t.Errorf("Expected error to be resolved once its reconciler succeeded, got %d errors", tracker.GetErrorCount())
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

func TestPersistentErrorTracker_MaxRetriesCleanup(t *testing.T) {
	logger := logr.Discard()
	tracker := NewPersistentErrorTracker(logger)

	resource := k8stypes.NamespacedName{Namespace: "ns", Name: "resource"}
	kind := schema.GroupKind{Group: "test.io", Kind: "TestKind"}

	// Record error and fail it maxRetryAttempts times
	for i := 0; i <= maxRetryAttempts; i++ {
		registry := NewErrorRegistry()
		registry.Record("TestReconciler", "delete", resource, kind, errors.New("persistent error"))
		tracker.UpdateFromRegistry(registry)

		if i < maxRetryAttempts {
			// Should still be tracked
			if tracker.GetErrorCount() != 1 {
				t.Errorf("Iteration %d: Expected 1 error in tracker, got %d", i, tracker.GetErrorCount())
			}
		} else {
			// After max retries, should be removed
			if tracker.GetErrorCount() != 0 {
				t.Errorf("After max retries: Expected error to be removed, got %d errors", tracker.GetErrorCount())
			}
		}
	}

	// Verify no requeue is scheduled
	if delay := tracker.ShouldRequeue(); delay != 0 {
		t.Errorf("Expected no requeue after cleanup, got %v", delay)
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
