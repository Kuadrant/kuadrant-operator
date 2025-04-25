package extension

import (
	"sync"
	"testing"
	"time"
)

func TestNilGuardedPointer(t *testing.T) {
	t.Run("set and get", func(t *testing.T) {
		ptr := newNilGuardedPointer[string]()

		if ptr.get() != nil {
			t.Errorf("Expected initial value to be nil, got %v", ptr.get())
		}

		value := "test"
		ptr.set(value)

		loaded := ptr.get()
		if loaded == nil {
			t.Error("Expected loaded value to be non-nil")
		} else if *loaded != value {
			t.Errorf("Expected loaded value to be %s, got %s", value, *loaded)
		}
	})

	t.Run("getWait blocks until value is set", func(t *testing.T) {
		ptr := newNilGuardedPointer[string]()

		done := make(chan struct{})
		var loaded string

		go func() {
			loaded = ptr.getWait()
			close(done)
		}()

		time.Sleep(100 * time.Millisecond)

		value := "test"
		ptr.set(value)

		select {
		case <-done:
			if loaded != value {
				t.Errorf("Expected loaded value to be %s, got %s", value, loaded)
			}
		case <-time.After(1 * time.Second):
			t.Error("Timed out waiting for getWait to return")
		}
	})

	t.Run("getWait returns immediately if value is already set", func(t *testing.T) {
		ptr := newNilGuardedPointer[string]()

		value := "test"
		ptr.set(value)

		start := time.Now()
		loaded := ptr.getWait()
		elapsed := time.Since(start)

		if elapsed > 100*time.Millisecond {
			t.Errorf("Expected getWait to return immediately, took %v", elapsed)
		}

		if loaded != value {
			t.Errorf("Expected loaded value to be %s, got %s", value, loaded)
		}
	})

	t.Run("getWaitWithTimeout returns false on timeout", func(t *testing.T) {
		ptr := newNilGuardedPointer[string]()

		start := time.Now()
		_, success := ptr.getWaitWithTimeout(100 * time.Millisecond)
		elapsed := time.Since(start)

		if elapsed < 100*time.Millisecond {
			t.Errorf("Expected getWaitWithTimeout to wait for at least the timeout duration, took %v", elapsed)
		}

		if success {
			t.Error("Expected success to be false on timeout")
		}
	})

	t.Run("getWaitWithTimeout returns true when value is set before timeout", func(t *testing.T) {
		ptr := newNilGuardedPointer[string]()

		done := make(chan bool)
		var loaded string

		go func() {
			var success bool
			l, success := ptr.getWaitWithTimeout(1 * time.Second)
			loaded = *l
			done <- success
		}()

		time.Sleep(100 * time.Millisecond)

		value := "test"
		ptr.set(value)

		select {
		case success := <-done:
			if !success {
				t.Error("Expected success to be true when value is set before timeout")
			}
			if loaded != value {
				t.Errorf("Expected loaded value to be %s, got %s", value, loaded)
			}
		case <-time.After(2 * time.Second):
			t.Error("Timed out waiting for getWaitWithTimeout to return")
		}
	})

	t.Run("BlockingDAG variable", func(t *testing.T) {
		if BlockingDAG.get() != nil {
			t.Error("Expected initial BlockingDAG to be nil")
		}

		dag := StateAwareDAG{
			topology: nil,
			state:    &sync.Map{},
		}

		BlockingDAG.set(dag)

		loaded := BlockingDAG.get()
		if loaded == nil {
			t.Error("Expected loaded BlockingDAG to be non-nil")
		}
	})
}
