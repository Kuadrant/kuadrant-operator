/*
Copyright 2024.

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

package metrics

import (
	"context"
	"fmt"

	prombridge "go.opentelemetry.io/contrib/bridges/prometheus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/sdk/metric"

	kuadrantotel "github.com/kuadrant/kuadrant-operator/internal/otel"
)

// Provider wraps the OpenTelemetry MeterProvider and manages its lifecycle
type Provider struct {
	meterProvider *metric.MeterProvider
	otlpEnabled   bool
}

// NewProvider creates a new OpenTelemetry metrics provider that bridges
// existing Prometheus metrics to OTLP export.
//
// This allows all existing Prometheus metrics (including controller-runtime
// metrics and custom metrics like kuadrant_dns_policy_ready) to be exported
// via OTLP without any code changes to the metrics themselves.
//
// The Prometheus /metrics endpoint continues to work as before.
//
// otelConfig provides shared service identity (used across logs, traces, metrics)
// metricsConfig provides metrics-specific settings (export interval, Prometheus gatherer)
func NewProvider(ctx context.Context, otelConfig *kuadrantotel.Config, metricsConfig *Config) (*Provider, error) {
	// Create shared resource for service identity (same as logs/traces)
	res, err := kuadrantotel.NewResource(ctx, otelConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create resource: %w", err)
	}

	// Bridge Prometheus metrics to OpenTelemetry
	// This creates a MetricProducer that reads from the Prometheus gatherer
	// and converts metrics to OpenTelemetry format
	promBridge := prombridge.NewMetricProducer(
		prombridge.WithGatherer(metricsConfig.PrometheusGatherer),
	)

	var readers []metric.Reader

	// Only setup OTLP export if OTel is enabled
	if otelConfig.Enabled {
		// Configure OTLP HTTP exporter options
		opts := []otlpmetrichttp.Option{
			otlpmetrichttp.WithEndpoint(otelConfig.MetricsEndpoint()),
		}
		if otelConfig.Insecure {
			opts = append(opts, otlpmetrichttp.WithInsecure())
		}

		// Create OTLP HTTP exporter
		otlpExporter, err := otlpmetrichttp.New(ctx, opts...)
		if err != nil {
			return nil, fmt.Errorf("failed to create OTLP exporter: %w", err)
		}

		// Create periodic reader that:
		// 1. Reads metrics from the Prometheus bridge
		// 2. Exports them via OTLP at the configured interval
		reader := metric.NewPeriodicReader(
			otlpExporter,
			metric.WithInterval(metricsConfig.ExportInterval),
			metric.WithProducer(promBridge),
		)
		readers = append(readers, reader)
	}

	// Create MeterProvider with the configured readers
	opts := []metric.Option{
		metric.WithResource(res),
	}
	for _, reader := range readers {
		opts = append(opts, metric.WithReader(reader))
	}
	meterProvider := metric.NewMeterProvider(opts...)

	// Set as global MeterProvider for any future OTel metrics
	otel.SetMeterProvider(meterProvider)

	return &Provider{
		meterProvider: meterProvider,
		otlpEnabled:   otelConfig.Enabled,
	}, nil
}

// Shutdown gracefully shuts down the metrics provider, flushing any pending metrics
func (p *Provider) Shutdown(ctx context.Context) error {
	if p.meterProvider == nil {
		return nil
	}
	return p.meterProvider.Shutdown(ctx)
}

// IsOTLPEnabled returns true if OTLP export is enabled
func (p *Provider) IsOTLPEnabled() bool {
	return p.otlpEnabled
}
