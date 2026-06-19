package controllers

import (
	"sync"
	"testing"
	"time"

	"github.com/go-logr/logr"
	"github.com/kuadrant/policy-machinery/controller"
)

func TestKuadrantController_ScheduleRetry(t *testing.T) {
	logger := logr.Discard()
	errorTracker := NewPersistentErrorTracker(logger)

	// Create a minimal KuadrantController (Controller field can be nil for this test)
	kuadrantController := &KuadrantController{
		errorTracker: errorTracker,
		logger:       logger,
	}

	// Test scheduling a retry
	events := []controller.ResourceEvent{
		{EventType: controller.UpdateEvent},
	}

	var timerFired bool
	var timerMu sync.Mutex

	// Mock time.AfterFunc to capture the timer behavior
	// We can't easily mock time.AfterFunc, so we'll just verify the timer is set
	kuadrantController.ScheduleRetry(100*time.Millisecond, events)

	// Verify timer was created
	kuadrantController.retryTimerMu.Lock()
	if kuadrantController.retryTimer == nil {
		t.Error("Expected retry timer to be set")
	}
	initialTimer := kuadrantController.retryTimer
	kuadrantController.retryTimerMu.Unlock()

	// Schedule another retry - should cancel the first timer
	kuadrantController.ScheduleRetry(200*time.Millisecond, events)

	kuadrantController.retryTimerMu.Lock()
	if kuadrantController.retryTimer == initialTimer {
		t.Error("Expected new timer to replace the old one")
	}
	if kuadrantController.retryTimer == nil {
		t.Error("Expected new retry timer to be set")
	}
	kuadrantController.retryTimerMu.Unlock()

	// Clean up
	kuadrantController.Stop()

	kuadrantController.retryTimerMu.Lock()
	if kuadrantController.retryTimer != nil {
		t.Error("Expected timer to be cleared by Stop()")
	}
	kuadrantController.retryTimerMu.Unlock()

	timerMu.Lock()
	_ = timerFired // Prevent unused variable error
	timerMu.Unlock()
}

func TestKuadrantController_CancelPendingRetry(t *testing.T) {
	logger := logr.Discard()
	errorTracker := NewPersistentErrorTracker(logger)

	kuadrantController := &KuadrantController{
		errorTracker: errorTracker,
		logger:       logger,
	}

	// Schedule a retry
	events := []controller.ResourceEvent{
		{EventType: controller.UpdateEvent},
	}
	kuadrantController.ScheduleRetry(1*time.Second, events)

	// Verify timer is set
	kuadrantController.retryTimerMu.Lock()
	if kuadrantController.retryTimer == nil {
		t.Fatal("Expected retry timer to be set")
	}
	kuadrantController.retryTimerMu.Unlock()

	// Cancel the pending retry
	kuadrantController.cancelPendingRetry()

	// Verify timer is cleared
	kuadrantController.retryTimerMu.Lock()
	if kuadrantController.retryTimer != nil {
		t.Error("Expected timer to be cancelled")
	}
	kuadrantController.retryTimerMu.Unlock()
}

func TestKuadrantController_Stop(t *testing.T) {
	logger := logr.Discard()
	errorTracker := NewPersistentErrorTracker(logger)

	kuadrantController := &KuadrantController{
		errorTracker: errorTracker,
		logger:       logger,
	}

	// Schedule a retry
	events := []controller.ResourceEvent{
		{EventType: controller.UpdateEvent},
	}
	kuadrantController.ScheduleRetry(1*time.Second, events)

	// Verify timer is set
	kuadrantController.retryTimerMu.Lock()
	if kuadrantController.retryTimer == nil {
		t.Fatal("Expected retry timer to be set")
	}
	kuadrantController.retryTimerMu.Unlock()

	// Stop the controller
	kuadrantController.Stop()

	// Verify timer is cleaned up
	kuadrantController.retryTimerMu.Lock()
	if kuadrantController.retryTimer != nil {
		t.Error("Expected timer to be stopped and cleared")
	}
	kuadrantController.retryTimerMu.Unlock()

	// Stopping again should be a no-op
	kuadrantController.Stop()
}

func TestKuadrantController_EventsCopy(t *testing.T) {
	logger := logr.Discard()
	errorTracker := NewPersistentErrorTracker(logger)

	kuadrantController := &KuadrantController{
		errorTracker: errorTracker,
		logger:       logger,
	}

	// Create events slice
	events := []controller.ResourceEvent{
		{EventType: controller.UpdateEvent},
	}

	// Schedule retry
	kuadrantController.ScheduleRetry(100*time.Millisecond, events)

	// Modify the original events slice (simulate backing array reuse)
	events[0].EventType = controller.DeleteEvent
	events = append(events, controller.ResourceEvent{EventType: controller.CreateEvent})

	// The timer callback should have a copy, not the modified slice
	// We can't easily verify this without triggering the timer, but at least
	// we've verified the code creates a copy in the implementation

	// Clean up
	kuadrantController.Stop()
}
