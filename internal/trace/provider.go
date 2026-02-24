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
	"net/url"

	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
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
// Returns an error if no traces endpoint is configured.
func NewProvider(ctx context.Context, otelConfig *otel.Config) (*Provider, error) {
	return NewProviderWithEndpoint(ctx, otelConfig, otelConfig.TracesEndpoint(), otelConfig.Insecure)
}

// newTraceExporter creates an OTLP trace exporter based on endpoint URL scheme
// Following the Authorino pattern:
//   - rpc://host:port  → gRPC exporter
//   - http://host:port → HTTP exporter (insecure)
//   - https://host:port → HTTP exporter (secure)
func newTraceExporter(ctx context.Context, endpoint string, insecure bool) (sdktrace.SpanExporter, error) {
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("invalid endpoint URL: %w", err)
	}

	var client otlptrace.Client

	switch u.Scheme {
	case "rpc":
		opts := []otlptracegrpc.Option{
			otlptracegrpc.WithEndpoint(u.Host),
		}
		if insecure {
			opts = append(opts, otlptracegrpc.WithInsecure())
		}
		client = otlptracegrpc.NewClient(opts...)

	case "http", "https":
		opts := []otlptracehttp.Option{
			otlptracehttp.WithEndpoint(u.Host),
		}
		if path := u.Path; path != "" {
			opts = append(opts, otlptracehttp.WithURLPath(path))
		}
		if insecure || u.Scheme == "http" {
			opts = append(opts, otlptracehttp.WithInsecure())
		}
		client = otlptracehttp.NewClient(opts...)

	default:
		return nil, fmt.Errorf("unsupported endpoint scheme: %s (use 'rpc', 'http', or 'https')", u.Scheme)
	}

	return otlptrace.New(ctx, client)
}

// NewProviderWithEndpoint creates a trace provider using the given endpoint directly,
// bypassing env var lookups. Used by DynamicProvider for runtime reconfiguration.
func NewProviderWithEndpoint(ctx context.Context, otelConfig *otel.Config, endpoint string, insecure bool) (*Provider, error) {
	if endpoint == "" {
		return nil, fmt.Errorf("traces disabled: no endpoint configured")
	}

	res, err := otel.NewResource(ctx, otelConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	exporter, err := newTraceExporter(ctx, endpoint, insecure)
	if err != nil {
		return nil, fmt.Errorf("failed to create OTLP trace exporter: %w", err)
	}

	tracerProvider := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithBatcher(exporter),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)

	return &Provider{tracerProvider: tracerProvider}, nil
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
