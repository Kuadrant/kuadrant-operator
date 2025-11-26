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
	"fmt"

	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/kuadrant/kuadrant-operator/internal/otel"
)

// Provider wraps the OpenTelemetry TracerProvider and manages its lifecycle
type Provider struct {
	tracerProvider *sdktrace.TracerProvider
}

// NewProvider creates a new OpenTelemetry trace provider.
//
// This enables distributed tracing for reconcilers, allowing:
// - Trace spans to be created for reconciliation operations
// - Logs to be automatically correlated with traces (via trace_id)
// - Performance analysis and debugging across reconciler operations
//
// otelConfig provides shared service identity (used across logs, traces, metrics)
func NewProvider(ctx context.Context, otelConfig *otel.Config) (*Provider, error) {
	if !otelConfig.Enabled {
		return nil, fmt.Errorf("OpenTelemetry is not enabled")
	}

	// Create shared resource for service identity (same as logs/metrics)
	res, err := otel.NewResource(ctx, otelConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	// Configure OTLP HTTP exporter options
	opts := []otlptracehttp.Option{
		otlptracehttp.WithEndpoint(otelConfig.TracesEndpoint()),
	}
	if otelConfig.Insecure {
		opts = append(opts, otlptracehttp.WithInsecure())
	}

	// Create OTLP HTTP exporter for traces
	exporter, err := otlptracehttp.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTLP trace exporter: %w", err)
	}

	// Create TracerProvider with batch span processor
	// Batching improves performance by sending spans in groups
	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithBatcher(exporter),
		// Sample all traces in development
		// In production, you may want to use a sampling strategy
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)

	return &Provider{
		tracerProvider: tracerProvider,
	}, nil
}

// TracerProvider returns the underlying TracerProvider
func (p *Provider) TracerProvider() *sdktrace.TracerProvider {
	return p.tracerProvider
}

// Shutdown gracefully shuts down the trace provider, flushing any pending spans
func (p *Provider) Shutdown(ctx context.Context) error {
	if p.tracerProvider == nil {
		return nil
	}
	return p.tracerProvider.Shutdown(ctx)
}
