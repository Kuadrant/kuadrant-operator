package controllers

import (
	"context"
	"sync"

	"github.com/kuadrant/policy-machinery/controller"
	"github.com/kuadrant/policy-machinery/machinery"
)

type EventLogger struct{}

func NewEventLogger() *EventLogger {
	return &EventLogger{}
}

func (e *EventLogger) Log(ctx context.Context, resourceEvents []controller.ResourceEvent, _ *machinery.Topology, err error, _ *sync.Map) error {
	logger := controller.LoggerFromContext(ctx).WithName("event logger")
	for _, event := range resourceEvents {
		// log the event
		obj := event.OldObject
		if obj == nil {
			obj = event.NewObject
		}
		logger.Info("new event",
			"type", event.EventType.String(),
			"kind", obj.GetObjectKind().GroupVersionKind().Kind,
			"namespace", obj.GetNamespace(),
			"name", obj.GetName(),
		)
		if err != nil {
			logger.Error(err, "error passed to reconcile")
		}
	}

	return nil
}
