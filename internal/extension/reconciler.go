package extension

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
)

var DAG *atomic.Pointer[StateAwareDAG]

type StateAwareDAG struct {
	topology *machinery.Topology
	state    *sync.Map
}

func Reconcile(_ context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, state *sync.Map) error {
	newDag := StateAwareDAG{
		topology: topology,
		state:    state,
	}
	DAG.Store(&newDag)
	return nil
}
