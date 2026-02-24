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
	"go.opentelemetry.io/otel/trace/noop" // used for noop.NewTracerProvider() in Reconfigure

	kuadrantotel "github.com/kuadrant/kuadrant-operator/internal/otel"
)

// DynamicProvider wraps a trace Provider and supports runtime reconfiguration
// without restarting the operator. The underlying exporter is hot-swapped by
// replacing the global OTEL tracer provider.
//
// Precedence: ConfigMap endpoint > env var endpoint > disabled (noop).
// Deleting the ConfigMap reverts to the env var config captured at startup.
type DynamicProvider struct {
	mu               sync.Mutex
	current          *Provider
	otelConfig       *kuadrantotel.Config
	fallbackEndpoint string
	fallbackInsecure bool
}

// NewDynamicProvider creates a DynamicProvider initialized from env var config.
// If a traces endpoint is set via env vars, it attempts to start exporting immediately.
// The env var values are stored as fallback for when a ConfigMap is later deleted.
//
// Failure to connect to the initial endpoint is non-fatal: the provider is always
// returned so the tracing ConfigMap reconciler can still reconfigure it at runtime.
// The error is returned alongside the provider so callers can log it.
func NewDynamicProvider(ctx context.Context, otelConfig *kuadrantotel.Config) (*DynamicProvider, error) {
	dp := &DynamicProvider{
		otelConfig:       otelConfig,
		fallbackEndpoint: otelConfig.TracesEndpoint(),
		fallbackInsecure: otelConfig.Insecure,
	}

	if dp.fallbackEndpoint != "" {
		p, err := NewProviderWithEndpoint(ctx, otelConfig, dp.fallbackEndpoint, dp.fallbackInsecure)
		if err != nil {
			return dp, err
		}
		dp.current = p
		otel.SetTracerProvider(p.TracerProvider())
	}

	return dp, nil
}

// Reconfigure hot-swaps the trace provider to export to the given endpoint.
// Passing an empty endpoint disables tracing (sets a noop provider).
// The previous provider is shut down gracefully before the new one starts.
func (d *DynamicProvider) Reconfigure(ctx context.Context, endpoint string, insecure bool) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	_ = d.shutdownCurrent(ctx)

	if endpoint == "" {
		otel.SetTracerProvider(noop.NewTracerProvider())
		return nil
	}

	p, err := NewProviderWithEndpoint(ctx, d.otelConfig, endpoint, insecure)
	if err != nil {
		// Ensure a consistent state even on failure: no partial provider.
		otel.SetTracerProvider(noop.NewTracerProvider())
		return err
	}

	d.current = p
	otel.SetTracerProvider(p.TracerProvider())
	return nil
}

// RevertToFallback restores the trace provider to the env var configuration
// captured at startup. Used when the tracing ConfigMap is deleted.
// If no env var endpoint was set, this disables tracing.
func (d *DynamicProvider) RevertToFallback(ctx context.Context) error {
	return d.Reconfigure(ctx, d.fallbackEndpoint, d.fallbackInsecure)
}

// Shutdown gracefully shuts down the current provider, flushing any pending spans.
// The caller is responsible for setting an appropriate deadline on ctx.
func (d *DynamicProvider) Shutdown(ctx context.Context) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.shutdownCurrent(ctx)
}

// GlobalTracer returns a tracer that always delegates to the current global tracer
// provider. This ensures that after a Reconfigure call, spans are routed to the
// updated provider without needing to update every call site.
func (d *DynamicProvider) GlobalTracer(name string) otelapi.Tracer {
	return &globalTracerProxy{name: name}
}

// shutdownCurrent shuts down the current provider using the caller's context.
// Must be called with d.mu held.
func (d *DynamicProvider) shutdownCurrent(ctx context.Context) error {
	if d.current == nil {
		return nil
	}
	err := d.current.Shutdown(ctx)
	d.current = nil
	return err
}

// globalTracerProxy implements otelapi.Tracer by delegating every call to the
// current global tracer provider. This makes the tracer resilient to provider
// swaps performed by DynamicProvider.Reconfigure.
// embedded.Tracer satisfies the unexported embedded interface requirement of otelapi.Tracer.
type globalTracerProxy struct {
	embedded.Tracer
	name string
}

func (t *globalTracerProxy) Start(ctx context.Context, spanName string, opts ...otelapi.SpanStartOption) (context.Context, otelapi.Span) {
	return otel.Tracer(t.name).Start(ctx, spanName, opts...)
}
