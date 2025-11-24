package controllers

import (
	"context"
	"slices"
	"sync"

	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
)

type EventLogger struct{}

func NewEventLogger() *EventLogger {
	return &EventLogger{}
}

func (e *EventLogger) Log(ctx context.Context, resourceEvents []controller.ResourceEvent, _ *machinery.Topology, err error, _ *sync.Map) error {
	logger := controller.LoggerFromContext(ctx).WithName("event logger").WithValues("context", ctx)
	eventType := make(map[string]int, 0)
	resources := make([]string, 0)
	for _, event := range resourceEvents {
		// log the event
		obj := event.OldObject
		if obj == nil {
			obj = event.NewObject
		}
		logger.V(1).Info("new event",
			"type", event.EventType.String(),
			"kind", obj.GetObjectKind().GroupVersionKind().Kind,
			"namespace", obj.GetNamespace(),
			"name", obj.GetName(),
		)
		if err != nil {
			logger.Error(err, "error passed to reconcile")
		}

		_, ok := eventType[event.EventType.String()]
		if ok {
			eventType[event.EventType.String()]++
		} else {
			eventType[event.EventType.String()] = 1
		}

		if !slices.Contains(resources, obj.GetObjectKind().GroupVersionKind().Kind) {
			resources = append(resources, obj.GetObjectKind().GroupVersionKind().Kind)
		}
	}

	logger.Info("new events", "resources", resources, "eventTypes", eventType)

	return nil
}
