package extension

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
)

// nilGuardedPointer is an atomic pointer that provides blocking behavior
// until the pointer is set to a non-nil value.
type nilGuardedPointer[T any] struct {
	ptr  atomic.Pointer[T]
	mu   sync.Mutex
	cond *sync.Cond
}

// newNilGuardedPointer creates a new nilGuardedPointer.
func newNilGuardedPointer[T any]() *nilGuardedPointer[T] {
	cgp := &nilGuardedPointer[T]{}
	cgp.cond = sync.NewCond(&cgp.mu)
	return cgp
}

// set sets the pointer to x and signals any goroutines waiting for a non-nil value.
func (c *nilGuardedPointer[T]) set(x T) {
	c.ptr.Store(&x)

	c.mu.Lock()
	defer c.mu.Unlock()

	c.cond.Broadcast()
}

// get returns the current value of the pointer without blocking.
func (c *nilGuardedPointer[T]) get() *T {
	return c.ptr.Load()
}

// getWait blocks until the pointer is set to a non-nil value and then returns that value.
func (c *nilGuardedPointer[T]) getWait() T {
	// First try a quick non-blocking check
	if val := c.ptr.Load(); val != nil {
		return *val
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	for c.ptr.Load() == nil {
		c.cond.Wait()
	}

	return *c.ptr.Load()
}

// getWaitWithTimeout blocks until the pointer is set to a non-nil value or until the timeout is reached.
// Returns the current value of the pointer and a boolean indicating whether the value was set before the timeout.
func (c *nilGuardedPointer[T]) getWaitWithTimeout(timeout time.Duration) (*T, bool) {
	// First try a quick non-blocking check
	if val := c.ptr.Load(); val != nil {
		return c.ptr.Load(), true
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	result := make(chan *T, 1)

	go func() {
		c.mu.Lock()
		defer c.mu.Unlock()

		for c.ptr.Load() == nil {
			c.cond.Wait()
		}

		val := c.ptr.Load()
		result <- val
	}()

	select {
	case val := <-result:
		return val, true
	case <-timer.C:
		return c.ptr.Load(), false
	}
}

// BlockingDAG is a condition variable guarded atomic pointer that blocks until the pointer is set to a non-nil value
var BlockingDAG = newNilGuardedPointer[StateAwareDAG]()

type StateAwareDAG struct {
	topology *machinery.Topology
	state    *sync.Map
}

func Reconcile(_ context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, state *sync.Map) error {
	newDag := StateAwareDAG{
		topology: topology,
		state:    state,
	}
	BlockingDAG.set(newDag)
	return nil
}
