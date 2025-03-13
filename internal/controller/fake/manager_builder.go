//go:build unit

package fake

import (
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrlruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ManagerBuilder struct {
	scheme        *runtime.Scheme
	client        client.Client
	apiReader     client.Reader
	eventRecorder record.EventRecorder
}

func NewManagerBuilder() *ManagerBuilder {
	return &ManagerBuilder{}
}

func (m *ManagerBuilder) WithScheme(scheme *runtime.Scheme) *ManagerBuilder {
	m.scheme = scheme
	return m
}

func (m *ManagerBuilder) WithClient(client client.Client) *ManagerBuilder {
	m.client = client
	return m
}

func (m *ManagerBuilder) Build() ctrlruntime.Manager {
	return &manager{
		client:        m.client,
		scheme:        m.scheme,
		apiReader:     &apireader{},
		eventRecorder: &eventrecorder{},
	}
}
