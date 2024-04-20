package controllers

import (
	"context"
	"reflect"

	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"

	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
)

// PolicyStatusEventHandlerFromMapFunc returns a PolicyStatusEventHandler that handles events from a mapping function.
func PolicyStatusEventHandlerFromMapFunc(mapFunc handler.MapFunc) handler.EventHandler {
	return NewPolicyStatusEventHandler(WithHandler(handler.EnqueueRequestsFromMapFunc(mapFunc)))
}

// NewPolicyStatusEventHandler returns a new PolicyStatusEventHandler.
func NewPolicyStatusEventHandler(opts ...PolicyStatusEventHandlerOption) handler.EventHandler {
	h := &PolicyStatusEventHandler{}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

type PolicyStatusEventHandlerOption func(*PolicyStatusEventHandler)

func WithHandler(h handler.EventHandler) PolicyStatusEventHandlerOption {
	return func(p *PolicyStatusEventHandler) {
		p.handler = h
	}
}

var _ handler.EventHandler = &PolicyStatusEventHandler{}

// PolicyStatusEventHandler enqueues reconcile.Requests in response to events for Policy objects
// whose status blocks have changed.
// The handling of the events is delegated to the provided handler.
type PolicyStatusEventHandler struct {
	handler handler.EventHandler
}

// Create implements EventHandler.
func (h *PolicyStatusEventHandler) Create(ctx context.Context, evt event.CreateEvent, q workqueue.RateLimitingInterface) {
	if h.handler == nil {
		return
	}
	h.handler.Create(ctx, evt, q)
}

// Update implements EventHandler.
func (h *PolicyStatusEventHandler) Update(ctx context.Context, evt event.UpdateEvent, q workqueue.RateLimitingInterface) {
	if h.handler == nil {
		return
	}
	oldPolicy, ok := evt.ObjectOld.(kuadrantgatewayapi.Policy)
	if !ok {
		return
	}
	newPolicy, ok := evt.ObjectNew.(kuadrantgatewayapi.Policy)
	if !ok {
		return
	}
	if statusChanged(oldPolicy, newPolicy) {
		h.handler.Update(ctx, evt, q)
	}
}

// Delete implements EventHandler.
func (h *PolicyStatusEventHandler) Delete(ctx context.Context, evt event.DeleteEvent, q workqueue.RateLimitingInterface) {
	if h.handler == nil {
		return
	}
	h.handler.Delete(ctx, evt, q)
}

// Generic implements EventHandler.
func (h *PolicyStatusEventHandler) Generic(ctx context.Context, evt event.GenericEvent, q workqueue.RateLimitingInterface) {
	if h.handler == nil {
		return
	}
	h.handler.Generic(ctx, evt, q)
}

func statusChanged(old, new kuadrantgatewayapi.Policy) bool {
	if old == nil || new == nil {
		return false
	}

	oldStatus := old.GetStatus()
	newStatus := new.GetStatus()

	return !reflect.DeepEqual(oldStatus, newStatus)
}
