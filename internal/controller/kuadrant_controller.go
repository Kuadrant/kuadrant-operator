package controllers

import (
	"context"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/kuadrant/policy-machinery/controller"
	ctrlruntimereconcile "sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// KuadrantController wraps the policy-machinery controller with error tracking
// and retry capabilities for non-blocking errors
type KuadrantController struct {
	*controller.Controller
	errorTracker *PersistentErrorTracker
	logger       logr.Logger
	retryTimer   *time.Timer
	retryTimerMu sync.Mutex
}

// NewKuadrantController wraps a policy-machinery controller with error tracking
func NewKuadrantController(policyMachineryController *controller.Controller, errorTracker *PersistentErrorTracker, logger logr.Logger) *KuadrantController {
	return &KuadrantController{
		Controller:   policyMachineryController,
		errorTracker: errorTracker,
		logger:       logger.WithName("KuadrantController"),
	}
}

// Reconcile implements the controller-runtime Reconciler interface
// It delegates to the wrapped policy-machinery controller and handles error tracking
func (c *KuadrantController) Reconcile(ctx context.Context, req ctrlruntimereconcile.Request) (ctrlruntimereconcile.Result, error) {
	// Cancel any pending retry timer since we're reconciling now
	c.cancelPendingRetry()

	// Delegate to the policy-machinery controller's Reconcile method
	return c.Controller.Reconcile(ctx, req)
}

// ScheduleRetry schedules a retry after the specified delay
// This is called by ReconciliationErrorHandler from within the workflow
func (c *KuadrantController) ScheduleRetry(delay time.Duration, events []controller.ResourceEvent) {
	c.retryTimerMu.Lock()
	defer c.retryTimerMu.Unlock()

	// Cancel any existing timer
	if c.retryTimer != nil {
		c.retryTimer.Stop()
	}

	c.logger.V(1).Info("scheduling reconciliation retry for non-blocking errors",
		"delay", delay,
		"errorCount", c.errorTracker.GetErrorCount(),
	)

	// Defensive copy of events slice to avoid reference issues if caller reuses the backing array
	eventsCopy := append([]controller.ResourceEvent(nil), events...)

	// Create a new timer that will trigger propagation
	c.retryTimer = time.AfterFunc(delay, func() {
		c.logger.Info("triggering reconciliation retry for non-blocking errors",
			"errorCount", c.errorTracker.GetErrorCount(),
		)

		// Check if we should still retry (in case errors were resolved by external events)
		if c.errorTracker.ShouldRequeue() > 0 {
			// Trigger reconciliation by calling Propagate with the original events
			// This forces the reconciliation workflow to run with the same context
			c.Propagate(eventsCopy)
		}
	})
}

// cancelPendingRetry cancels any scheduled retry timer
func (c *KuadrantController) cancelPendingRetry() {
	c.retryTimerMu.Lock()
	defer c.retryTimerMu.Unlock()

	if c.retryTimer != nil {
		c.retryTimer.Stop()
		c.retryTimer = nil
		c.logger.V(1).Info("cancelled pending retry (new reconciliation started)")
	}
}
