package controller

import (
	"context"
	"sync"

	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlruntimeevent "sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	EventTypeCreate  = "CREATE"
	EventTypeUpdate  = "UPDATE"
	EventTypeDelete  = "DELETE"
	EventTypeGeneric = "GENERIC"
	EventTypeUnknown = "UNKNOWN"
)

type EventTypeCache struct {
	mutex  sync.RWMutex
	events map[string][]string
}

func newEventTypeCache() *EventTypeCache {
	return &EventTypeCache{
		events: make(map[string][]string),
	}
}

func (ec *EventTypeCache) pushEvent(namespace, name, eventType string) {
	ec.mutex.Lock()
	defer ec.mutex.Unlock()
	key := namespace + "/" + name
	ec.events[key] = append(ec.events[key], eventType)
}

func (ec *EventTypeCache) popEvent(namespace, name string) (string, bool) {
	ec.mutex.Lock()
	defer ec.mutex.Unlock()
	key := namespace + "/" + name
	queue, exists := ec.events[key]
	if !exists || len(queue) == 0 {
		return "", false
	}

	event := queue[0]
	ec.events[key] = queue[1:]

	if len(ec.events[key]) == 0 {
		delete(ec.events, key)
	}

	return event, true
}

type EventCachingHandler struct {
	eventCache *EventTypeCache
}

func newEventCachingHandler(eventCache *EventTypeCache) *EventCachingHandler {
	return &EventCachingHandler{
		eventCache: eventCache,
	}
}

func (h *EventCachingHandler) Create(_ context.Context, event ctrlruntimeevent.TypedCreateEvent[client.Object], queue workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	if event.Object != nil {
		h.eventCache.pushEvent(event.Object.GetNamespace(), event.Object.GetName(), EventTypeCreate)
		enqueueRequest(event.Object, queue)
	}
}

func (h *EventCachingHandler) Update(_ context.Context, event ctrlruntimeevent.TypedUpdateEvent[client.Object], queue workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	obj := event.ObjectNew
	if obj == nil {
		obj = event.ObjectOld
	}
	if obj != nil {
		h.eventCache.pushEvent(obj.GetNamespace(), obj.GetName(), EventTypeUpdate)
		enqueueRequest(obj, queue)
	}
}

func (h *EventCachingHandler) Delete(_ context.Context, event ctrlruntimeevent.TypedDeleteEvent[client.Object], queue workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	if event.Object != nil {
		h.eventCache.pushEvent(event.Object.GetNamespace(), event.Object.GetName(), EventTypeDelete)
		enqueueRequest(event.Object, queue)
	}
}

func (h *EventCachingHandler) Generic(_ context.Context, event ctrlruntimeevent.TypedGenericEvent[client.Object], queue workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	if event.Object != nil {
		h.eventCache.pushEvent(event.Object.GetNamespace(), event.Object.GetName(), EventTypeGeneric)
		enqueueRequest(event.Object, queue)
	}
}

func enqueueRequest(obj client.Object, queue workqueue.TypedRateLimitingInterface[reconcile.Request]) {
	request := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Namespace: obj.GetNamespace(),
			Name:      obj.GetName(),
		},
	}
	queue.Add(request)
}
