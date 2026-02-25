/*
Copyright 2025 Red Hat, Inc.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package trace

import (
	"context"
	"sync"

	"go.opentelemetry.io/otel"
	otelapi "go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/embedded"
	"go.opentelemetry.io/otel/trace/noop"

	kuadrantotel "github.com/kuadrant/kuadrant-operator/internal/otel"
)

// DynamicProvider wraps a trace Provider and supports runtime reconfiguration
// without restarting the operator. The underlying exporter is hot-swapped by
// replacing the global OTEL tracer provider.
//
// Precedence: ConfigMap endpoint > env var endpoint > disabled (noop).
// Deleting the ConfigMap reverts to the env var config.
type DynamicProvider struct {
	mu         sync.Mutex
	current    *Provider
	otelConfig *kuadrantotel.Config
	// currentEndpoint and currentInsecure track the active configuration so
	// Reconfigure can skip unnecessary provider restarts on every reconcile cycle.
	currentEndpoint string
	currentInsecure bool
}

// NewDynamicProvider creates a DynamicProvider.
// If a traces endpoint is configured, it attempts to start exporting immediately.
//
// Failure to connect to the initial endpoint is non-fatal: the provider is always
// returned so the tracing ConfigMap reconciler can still reconfigure it at runtime.
func NewDynamicProvider(ctx context.Context, otelConfig *kuadrantotel.Config) (*DynamicProvider, error) {
	dp := &DynamicProvider{
		otelConfig: otelConfig,
	}

	if endpoint := otelConfig.TracesEndpoint(); endpoint != "" {
		p, err := NewProviderWithEndpoint(ctx, otelConfig, endpoint, otelConfig.Insecure)
		if err != nil {
			return dp, err
		}
		dp.current = p
		dp.currentEndpoint = endpoint
		dp.currentInsecure = otelConfig.Insecure
		otel.SetTracerProvider(p.TracerProvider())
	}

	return dp, nil
}

// Reconfigure hot-swaps the trace provider to export to the given endpoint.
// Passing an empty endpoint disables tracing (sets a noop provider).
// Idempotent: if the endpoint and insecure flag are unchanged it returns
// immediately without restarting the provider.
func (d *DynamicProvider) Reconfigure(ctx context.Context, endpoint string, insecure bool) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.currentEndpoint == endpoint && d.currentInsecure == insecure {
		return nil
	}

	_ = d.shutdownCurrent(ctx)

	if endpoint == "" {
		otel.SetTracerProvider(noop.NewTracerProvider())
		d.currentEndpoint = ""
		d.currentInsecure = false
		return nil
	}

	p, err := NewProviderWithEndpoint(ctx, d.otelConfig, endpoint, insecure)
	if err != nil {
		otel.SetTracerProvider(noop.NewTracerProvider())
		d.currentEndpoint = ""
		d.currentInsecure = false
		return err
	}

	d.current = p
	otel.SetTracerProvider(p.TracerProvider())
	d.currentEndpoint = endpoint
	d.currentInsecure = insecure
	return nil
}

// RevertToFallback restores the trace provider to the env var configuration.
// If no env var endpoint is configured, this disables tracing.
func (d *DynamicProvider) RevertToFallback(ctx context.Context) error {
	return d.Reconfigure(ctx, d.otelConfig.TracesEndpoint(), d.otelConfig.Insecure)
}

// Shutdown gracefully shuts down the current provider, flushing any pending spans.
// The caller is responsible for setting an appropriate deadline on ctx.
func (d *DynamicProvider) Shutdown(ctx context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.shutdownCurrent(ctx)
}

// GlobalTracer returns a tracer that always delegates to the current global tracer
// provider. This is necessary because the controller caches the tracer as a field
// and reuses it across reconcile cycles — the proxy ensures provider swaps are
// picked up without replacing the cached instance.
func (d *DynamicProvider) GlobalTracer(name string) otelapi.Tracer {
	return &globalTracerProxy{name: name}
}

// Must be called with d.mu held.
func (d *DynamicProvider) shutdownCurrent(ctx context.Context) error {
	if d.current == nil {
		return nil
	}
	err := d.current.Shutdown(ctx)
	d.current = nil
	return err
}

// globalTracerProxy implements otelapi.Tracer.
// embedded.Tracer satisfies its unexported interface requirement.
type globalTracerProxy struct {
	embedded.Tracer
	name string
}

func (t *globalTracerProxy) Start(ctx context.Context, spanName string, opts ...otelapi.SpanStartOption) (context.Context, otelapi.Span) {
	return otel.Tracer(t.name).Start(ctx, spanName, opts...)
}
