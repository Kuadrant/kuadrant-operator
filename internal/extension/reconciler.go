package extension

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
)

var DAG *atomic.Pointer[machinery.Topology]

func Reconcile(_ context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, _ *sync.Map) error {
	DAG.Store(topology)
	return nil
}
