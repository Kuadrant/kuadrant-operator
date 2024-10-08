//go:build unit

package fake

import (
	"context"
	"net/http"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	ctrlruntimemanager "sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

type manager struct {
	client        client.Client
	scheme        *runtime.Scheme
	apiReader     client.Reader
	eventRecorder record.EventRecorder
}

func (m *manager) Add(ctrlruntimemanager.Runnable) error {
	panic("Not Implemented")
}

func (m *manager) Elected() <-chan struct{} {
	panic("Not Implemented")
}

func (m *manager) AddMetricsServerExtraHandler(path string, handler http.Handler) error {
	panic("Not Implemented")

}
func (m *manager) AddHealthzCheck(name string, check healthz.Checker) error {
	panic("Not Implemented")
}

func (m *manager) AddReadyzCheck(name string, check healthz.Checker) error {
	panic("Not Implemented")
}

func (m *manager) Start(ctx context.Context) error {
	panic("Not Implemented")
}

func (m *manager) GetWebhookServer() webhook.Server {
	panic("Not Implemented")
}

func (m *manager) GetLogger() logr.Logger {
	panic("Not Implemented")
}

func (m *manager) GetControllerOptions() config.Controller {
	panic("Not Implemented")
}

func (m *manager) GetHTTPClient() *http.Client {
	panic("Not Implemented")
}

func (m *manager) GetConfig() *rest.Config {
	panic("Not Implemented")
}

func (m *manager) GetCache() cache.Cache {
	panic("Not Implemented")
}

func (m *manager) GetScheme() *runtime.Scheme {
	return m.scheme
}

func (m *manager) GetClient() client.Client {
	return m.client
}

func (m *manager) GetFieldIndexer() client.FieldIndexer {
	panic("Not Implemented")
}

func (m *manager) GetEventRecorderFor(name string) record.EventRecorder {
	return m.eventRecorder
}

func (m *manager) GetRESTMapper() meta.RESTMapper {
	panic("Not Implemented")
}

func (m *manager) GetAPIReader() client.Reader {
	return m.apiReader
}
