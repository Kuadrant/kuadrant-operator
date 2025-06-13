package extension

import (
	"context"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
	"github.com/samber/lo"
	v1 "sigs.k8s.io/gateway-api/apis/v1"

	extpb "github.com/kuadrant/kuadrant-operator/pkg/extension/grpc/v1"
)

// nilGuardedPointer is an atomic pointer that provides blocking behavior
// until the pointer is set to a non-nil value.
type nilGuardedPointer[T any] struct {
	ptr     atomic.Pointer[T]
	mu      sync.Mutex
	cond    *sync.Cond
	updates []chan T
}

// newNilGuardedPointer creates a new nilGuardedPointer.
func newNilGuardedPointer[T any]() *nilGuardedPointer[T] {
	ngp := nilGuardedPointer[T]{}
	ngp.cond = sync.NewCond(&ngp.mu)
	return &ngp
}

// set sets the pointer to x and signals any goroutines waiting for a non-nil value.
func (ngp *nilGuardedPointer[T]) set(x T) {
	previous := ngp.ptr.Swap(&x)

	ngp.mu.Lock()
	defer ngp.mu.Unlock()

	ngp.cond.Broadcast()

	if previous != nil && ngp.updates != nil {
		for _, update := range ngp.updates {
			update <- x
		}
	}
}

func (ngp *nilGuardedPointer[T]) newUpdateChannel() chan T {
	ngp.mu.Lock()
	defer ngp.mu.Unlock()

	channel := make(chan T)
	ngp.updates = append(ngp.updates, channel)
	return channel
}

// get returns the current value of the pointer without blocking.
func (ngp *nilGuardedPointer[T]) get() *T {
	return ngp.ptr.Load()
}

// getWait blocks until the pointer is set to a non-nil value and then returns that value.
func (ngp *nilGuardedPointer[T]) getWait() T {
	// First try a quick non-blocking check
	if val := ngp.ptr.Load(); val != nil {
		return *val
	}

	ngp.mu.Lock()
	defer ngp.mu.Unlock()

	for ngp.ptr.Load() == nil {
		ngp.cond.Wait()
	}

	return *ngp.ptr.Load()
}

// getWaitWithTimeout blocks until the pointer is set to a non-nil value or until the timeout is reached.
// Returns the current value of the pointer and a boolean indicating whether the value was set before the timeout.
func (ngp *nilGuardedPointer[T]) getWaitWithTimeout(timeout time.Duration) (*T, bool) {
	// First try a quick non-blocking check
	if val := ngp.ptr.Load(); val != nil {
		return ngp.ptr.Load(), true
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	result := make(chan *T, 1)

	go func() {
		ngp.mu.Lock()
		defer ngp.mu.Unlock()

		for ngp.ptr.Load() == nil {
			ngp.cond.Wait()
		}

		val := ngp.ptr.Load()
		result <- val
	}()

	select {
	case val := <-result:
		return val, true
	case <-timer.C:
		return ngp.ptr.Load(), false
	}
}

var BlockingDAG = newNilGuardedPointer[StateAwareDAG]()

type StateAwareDAG struct {
	topology *machinery.Topology
	state    *sync.Map
}

func (d *StateAwareDAG) FindGatewaysFor(targetRefs []*extpb.TargetRef) ([]*extpb.Gateway, error) {
	chain := d.topology.All().Items(func(o machinery.Object) bool {
		return len(lo.Filter(targetRefs, func(t *extpb.TargetRef, _ int) bool {
			return t.Name == o.GetName() && t.Kind == o.GroupVersionKind().Kind
		})) > 0
	})

	gateways := make([]*extpb.Gateway, 0)
	chainSize := len(chain)

	for i := 0; i < chainSize; i++ {
		object := chain[i]
		parents := d.topology.All().Parents(object)
		chain = append(chain, parents...)
		chainSize = len(chain)
		if gw, ok := object.(*machinery.Gateway); ok && gw != nil {
			gateways = append(gateways, toGw(*gw))
		}
	}

	return lo.UniqBy(gateways, func(gw *extpb.Gateway) string {
		return gw.GetMetadata().GetNamespace() + "/" + gw.GetMetadata().GetName()
	}), nil
}
func (d *StateAwareDAG) FindPoliciesFor(targetRefs []*extpb.TargetRef, policyType machinery.Policy) ([]*extpb.Policy, error) {
	initialTargets := d.topology.All().Items(func(o machinery.Object) bool {
		return len(lo.Filter(targetRefs, func(t *extpb.TargetRef, _ int) bool {
			return t.Name == o.GetName() && t.Kind == o.GroupVersionKind().Kind
		})) > 0
	})

	chain := make([]machinery.Object, 0)
	for _, target := range initialTargets {
		if policy, ok := target.(machinery.Policy); ok {
			// If the target is a policy, check its children
			children := d.topology.Targetables().Children(policy)
			for _, child := range children {
				chain = append(chain, child)
			}
		} else {
			// If the target is not a policy, use it directly
			chain = append(chain, target)
		}
	}

	policies := make([]*extpb.Policy, 0)
	chainSize := len(chain)

	for i := 0; i < chainSize; i++ {
		object := chain[i]
		parents := d.topology.All().Parents(object)
		chain = append(chain, parents...)
		chainSize = len(chain)

		// Check if this object is a targetable and has policies
		if targetable, ok := object.(machinery.Targetable); ok {
			for _, policy := range targetable.Policies() {
				if reflect.TypeOf(policy) == reflect.TypeOf(policyType) {
					policies = append(policies, toPolicy(policy))
				}
			}
		}
	}

	return lo.UniqBy(policies, func(p *extpb.Policy) string {
		return p.GetMetadata().GetNamespace() + "/" + p.GetMetadata().GetName()
	}), nil
}

func toGw(gw machinery.Gateway) *extpb.Gateway {
	return &extpb.Gateway{
		Metadata: &extpb.Metadata{
			Name:      gw.Name,
			Namespace: gw.Namespace,
		},
		Spec: &extpb.GatewaySpec{
			GatewayClassName: string(gw.Spec.GatewayClassName),
			Listeners:        toListeners(gw.Spec.Listeners),
		},
	}
}

func toPolicy(policy machinery.Policy) *extpb.Policy {
	return &extpb.Policy{
		Metadata: &extpb.Metadata{
			Group:     policy.GroupVersionKind().Group,
			Kind:      policy.GroupVersionKind().Kind,
			Namespace: policy.GetNamespace(),
			Name:      policy.GetName(),
		},
		TargetRefs: toTargetRefs(policy.GetTargetRefs()),
	}
}

func toTargetRefs(targetRefs []machinery.PolicyTargetReference) []*extpb.TargetRef {
	trs := make([]*extpb.TargetRef, len(targetRefs))
	for i, tr := range targetRefs {
		targetRef := extpb.TargetRef{
			Name:      tr.GetName(),
			Namespace: tr.GetNamespace(),
			Kind:      tr.GroupVersionKind().GroupKind().Kind,
			Group:     tr.GroupVersionKind().Group,
		}
		trs[i] = &targetRef
	}
	return trs
}

func toListeners(listeners []v1.Listener) []*extpb.Listener {
	ls := make([]*extpb.Listener, len(listeners))
	for i, l := range listeners {
		listener := extpb.Listener{}
		if l.Hostname != nil {
			listener.Hostname = string(*l.Hostname)
		}
		ls[i] = &listener
	}
	return ls
}

func Reconcile(_ context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, state *sync.Map) error {
	newDag := StateAwareDAG{
		topology: topology,
		state:    state,
	}
	BlockingDAG.set(newDag)
	return nil
}
