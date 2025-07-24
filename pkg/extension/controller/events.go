package controller

import (
	"sync"
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
