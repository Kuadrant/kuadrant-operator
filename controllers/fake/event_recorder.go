//go:build unit

package fake

import "k8s.io/apimachinery/pkg/runtime"

type eventrecorder struct {
}

func (e *eventrecorder) Event(object runtime.Object, eventtype, reason, message string) {
	panic("Not Implemented")
}

func (e *eventrecorder) Eventf(object runtime.Object, eventtype, reason, messageFmt string, args ...interface{}) {
	panic("Not Implemented")
}

func (e *eventrecorder) AnnotatedEventf(object runtime.Object, annotations map[string]string, eventtype, reason, messageFmt string, args ...interface{}) {
	panic("Not Implemented")
}
