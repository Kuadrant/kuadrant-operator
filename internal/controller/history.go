package controllers

import (
	"context"
	"sync"

	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
)

type History struct {
	kuadrant *kuadrantv1beta1.Kuadrant
}

var (
	history *History
	mu      sync.RWMutex
)

func initializeHistory() {
	mu.Lock()
	defer mu.Unlock()
	if history == nil {
		history = &History{kuadrant: nil}
	}
}

func GetHistory() *History {
	mu.RLock()
	defer mu.RUnlock()

	return history

}

func updateHistory(topology *machinery.Topology) {
	mu.Lock()
	defer mu.Unlock()

	kObj := GetKuadrantFromTopology(topology)
	history.kuadrant = kObj
}

func InitializeHistoryFunc(_ context.Context, _ []controller.ResourceEvent, _ *machinery.Topology, _ error, _ *sync.Map) error {
	initializeHistory()
	return nil
}

func UpdateHistoryFunc(_ context.Context, _ []controller.ResourceEvent, topology *machinery.Topology, _ error, _ *sync.Map) error {
	updateHistory(topology)
	return nil
}
