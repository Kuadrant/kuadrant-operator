package controllers

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"k8s.io/apimachinery/pkg/runtime/schema"
	k8stypes "k8s.io/apimachinery/pkg/types"
)

const (
	// StateReconciliationErrors is the key for storing errors in the workflow state
	StateReconciliationErrors = "ReconciliationErrors"

	// StateRequeueAfter is the key for storing the requeue delay in workflow state
	StateRequeueAfter = "RequeueAfter"

	// Retry configuration
	initialRetryDelay = 2 * time.Second
	maxRetryDelay     = 1 * time.Minute
	maxRetryAttempts  = 5

	// Operation types for error recording
	OperationCreate = "create"
	OperationUpdate = "update"
	OperationDelete = "delete"
)

// ReconciliationError represents a single non-blocking error
type ReconciliationError struct {
	Source       string                  // Reconciler name
	Operation    string                  // create, update, delete
	Resource     k8stypes.NamespacedName // Resource that failed
	ResourceKind schema.GroupKind        // Resource type
	Err          error                   // Underlying error
	FirstSeen    time.Time               // When first recorded
	LastSeen     time.Time               // When last attempted
	RetryCount   int                     // Number of retry attempts
}

// Key returns a unique identifier for this error
func (e *ReconciliationError) Key() string {
	return fmt.Sprintf("%s/%s/%s/%s",
		e.ResourceKind.String(),
		e.Resource.Namespace,
		e.Resource.Name,
		e.Operation,
	)
}

// NextRetryDelay calculates exponential backoff delay
func (e *ReconciliationError) NextRetryDelay() time.Duration {
	// Calculate 2^retryCount with safe conversion
	// Cap the shift at 30 to avoid overflow (2^30 seconds = ~34 years, well over maxRetryDelay)
	retryCount := e.RetryCount
	if retryCount < 0 {
		retryCount = 0
	}
	if retryCount > 30 {
		retryCount = 30
	}

	// Safe conversion: retryCount is now guaranteed to be in [0, 30]
	// #nosec G115 -- retryCount is bounded to [0, 30]
	shift := uint(retryCount)
	delay := time.Duration(float64(initialRetryDelay) * float64(uint(1)<<shift))
	if delay > maxRetryDelay {
		delay = maxRetryDelay
	}
	return delay
}

// ShouldRetry determines if this error should be retried
func (e *ReconciliationError) ShouldRetry() bool {
	return e.RetryCount < maxRetryAttempts
}

// ErrorRegistry collects non-blocking errors during a single reconciliation
// It's stored in the workflow state map and only lives for one reconciliation cycle
type ErrorRegistry struct {
	mu     sync.Mutex
	errors map[string]*ReconciliationError
}

// NewErrorRegistry creates a new error registry
func NewErrorRegistry() *ErrorRegistry {
	return &ErrorRegistry{
		errors: make(map[string]*ReconciliationError),
	}
}

// Record adds a non-blocking error to the registry
func (r *ErrorRegistry) Record(source, operation string, resource k8stypes.NamespacedName, kind schema.GroupKind, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	rec := &ReconciliationError{
		Source:       source,
		Operation:    operation,
		Resource:     resource,
		ResourceKind: kind,
		Err:          err,
		FirstSeen:    time.Now(),
		LastSeen:     time.Now(),
		RetryCount:   0,
	}

	r.errors[rec.Key()] = rec
}

// GetErrors returns all recorded errors
func (r *ErrorRegistry) GetErrors() []*ReconciliationError {
	r.mu.Lock()
	defer r.mu.Unlock()

	errors := make([]*ReconciliationError, 0, len(r.errors))
	for _, err := range r.errors {
		errors = append(errors, err)
	}
	return errors
}

// HasErrors returns true if any errors were recorded
func (r *ErrorRegistry) HasErrors() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.errors) > 0
}

// GetOrCreateErrorRegistry retrieves or creates an error registry from workflow state
func GetOrCreateErrorRegistry(state *sync.Map) *ErrorRegistry {
	if state == nil {
		return NewErrorRegistry()
	}

	reg, _ := state.LoadOrStore(StateReconciliationErrors, NewErrorRegistry())
	return reg.(*ErrorRegistry)
}

// PersistentErrorTracker tracks errors across reconciliation cycles
// It's stored in the Controller and persists beyond individual reconciliations
type PersistentErrorTracker struct {
	mu     sync.RWMutex
	errors map[string]*ReconciliationError
	logger logr.Logger
}

// NewPersistentErrorTracker creates a new persistent error tracker
func NewPersistentErrorTracker(logger logr.Logger) *PersistentErrorTracker {
	return &PersistentErrorTracker{
		errors: make(map[string]*ReconciliationError),
		logger: logger.WithName("PersistentErrorTracker"),
	}
}

// UpdateFromRegistry merges errors from a workflow's error registry
// This is called at the end of each reconciliation to persist errors
func (t *PersistentErrorTracker) UpdateFromRegistry(registry *ErrorRegistry) {
	t.mu.Lock()
	defer t.mu.Unlock()

	newErrors := registry.GetErrors()

	// Track which errors are still active in this reconciliation
	currentKeys := make(map[string]bool)

	for _, newErr := range newErrors {
		key := newErr.Key()
		currentKeys[key] = true

		if existing, found := t.errors[key]; found {
			// Error persists - increment retry count
			existing.RetryCount++
			existing.LastSeen = time.Now()
			existing.Err = newErr.Err // Update error message in case it changed

			// Check if we've exceeded max retries - if so, give up and remove from tracker
			if !existing.ShouldRetry() {
				t.logger.Error(existing.Err, "max retry attempts reached, giving up",
					"source", existing.Source,
					"operation", existing.Operation,
					"resource", existing.Resource.String(),
					"kind", existing.ResourceKind.String(),
					"attempts", existing.RetryCount,
					"duration", time.Since(existing.FirstSeen),
				)
				delete(t.errors, key)
				delete(currentKeys, key) // Don't count as "still active"
				continue
			}

			t.logger.V(1).Info("error persists",
				"key", key,
				"retryCount", existing.RetryCount,
				"nextDelay", existing.NextRetryDelay(),
			)
		} else {
			// New error
			newErr.FirstSeen = time.Now()
			newErr.LastSeen = time.Now()
			newErr.RetryCount = 0
			t.errors[key] = newErr

			t.logger.Info("new non-blocking error recorded",
				"source", newErr.Source,
				"operation", newErr.Operation,
				"resource", newErr.Resource.String(),
				"kind", newErr.ResourceKind.String(),
				"error", newErr.Err.Error(),
			)
		}
	}

	// Remove errors that were not seen in this reconciliation
	// (they were successfully resolved)
	for key, err := range t.errors {
		if !currentKeys[key] {
			t.logger.Info("non-blocking error resolved",
				"key", key,
				"attempts", err.RetryCount+1,
				"duration", time.Since(err.FirstSeen),
			)
			delete(t.errors, key)
		}
	}
}

// ShouldRequeue determines if we need to schedule a retry reconciliation
// Returns the delay until the next retry, or zero if no retry needed
func (t *PersistentErrorTracker) ShouldRequeue() time.Duration {
	t.mu.RLock()
	defer t.mu.RUnlock()

	if len(t.errors) == 0 {
		return 0
	}

	// Find the minimum retry delay among all errors
	// (errors that exceed max retries are already removed from the tracker)
	var minDelay time.Duration
	for _, err := range t.errors {
		delay := err.NextRetryDelay()
		if minDelay == 0 || delay < minDelay {
			minDelay = delay
		}
	}

	return minDelay
}

// GetErrorCount returns the number of tracked errors
func (t *PersistentErrorTracker) GetErrorCount() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return len(t.errors)
}

// Clear removes all tracked errors (used for testing or manual intervention)
func (t *PersistentErrorTracker) Clear() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.logger.Info("clearing all tracked errors", "count", len(t.errors))
	t.errors = make(map[string]*ReconciliationError)
}

// RetryScheduler is a callback to schedule a retry after a delay
type RetryScheduler func(delay time.Duration, events []controller.ResourceEvent)

// ReconciliationErrorHandler is a workflow postcondition that:
// 1. Collects errors from the ErrorRegistry in workflow state
// 2. Merges them into the PersistentErrorTracker
// 3. Schedules a retry if needed (via callback)
type ReconciliationErrorHandler struct {
	tracker       *PersistentErrorTracker
	scheduleRetry RetryScheduler
}

func NewReconciliationErrorHandler(tracker *PersistentErrorTracker, scheduleRetry RetryScheduler) *ReconciliationErrorHandler {
	return &ReconciliationErrorHandler{
		tracker:       tracker,
		scheduleRetry: scheduleRetry,
	}
}

// Reconcile processes errors from the registry and determines requeue strategy
// This is meant to be called as a workflow postcondition, not via the Subscription pattern
func (h *ReconciliationErrorHandler) Reconcile(ctx context.Context, events []controller.ResourceEvent, _ *machinery.Topology, _ error, state *sync.Map) error {
	logger := controller.LoggerFromContext(ctx).WithName("ReconciliationErrorHandler")

	registry := GetOrCreateErrorRegistry(state)

	if !registry.HasErrors() {
		logger.V(1).Info("no non-blocking errors in this reconciliation")
		// No errors in this cycle - tracker will clean up resolved errors
		h.tracker.UpdateFromRegistry(registry)
		return nil
	}

	errors := registry.GetErrors()
	logger.V(1).Info("processing non-blocking errors", "count", len(errors))

	// Merge errors into persistent tracker
	h.tracker.UpdateFromRegistry(registry)

	// Determine if we should requeue
	requeueAfter := h.tracker.ShouldRequeue()

	if requeueAfter > 0 {
		logger.Info("scheduling retry for non-blocking errors",
			"errorCount", h.tracker.GetErrorCount(),
			"requeueAfter", requeueAfter,
		)

		// Schedule the retry via callback (runs in the workflow's goroutine)
		if h.scheduleRetry != nil {
			h.scheduleRetry(requeueAfter, events)
		}
	} else {
		logger.Info("all non-blocking errors have exceeded max retry attempts",
			"errorCount", h.tracker.GetErrorCount(),
		)
	}

	return nil
}
