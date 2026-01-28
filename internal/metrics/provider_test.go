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
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	kuadrantotel "github.com/kuadrant/kuadrant-operator/internal/otel"
)

func TestNewProvider(t *testing.T) {
	ctx := context.Background()
	registry := prometheus.NewRegistry()

	tests := []struct {
		name          string
		otelConfig    *kuadrantotel.Config
		metricsConfig *Config
		wantEnabled   bool
		wantErr       bool
	}{
		{
			name: "OTLP disabled",
			otelConfig: &kuadrantotel.Config{
				Endpoint:       "", // Empty endpoint = disabled
				Insecure:       true,
				ServiceName:    "test",
				ServiceVersion: "v1.0.0",
			},
			metricsConfig: &Config{
				PrometheusGatherer: registry,
				ExportInterval:     30 * time.Second,
			},
			wantEnabled: false,
			wantErr:     false,
		},
		{
			name: "OTLP enabled with insecure endpoint",
			otelConfig: &kuadrantotel.Config{
				Endpoint:       "http://localhost:4318",
				Insecure:       true,
				ServiceName:    "test",
				ServiceVersion: "v1.0.0",
			},
			metricsConfig: &Config{
				PrometheusGatherer: registry,
				ExportInterval:     30 * time.Second,
			},
			wantEnabled: true,
			wantErr:     false,
		},
		{
			name: "OTLP enabled with RPC endpoint",
			otelConfig: &kuadrantotel.Config{
				Endpoint:       "rpc://localhost:4317",
				Insecure:       true,
				ServiceName:    "test",
				ServiceVersion: "v1.0.0",
			},
			metricsConfig: &Config{
				PrometheusGatherer: registry,
				ExportInterval:     30 * time.Second,
			},
			wantEnabled: true,
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, err := NewProvider(ctx, tt.otelConfig, tt.metricsConfig)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewProvider() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if err != nil {
				return
			}
			defer provider.Shutdown(ctx)

			if provider.IsOTLPEnabled() != tt.wantEnabled {
				t.Errorf("IsOTLPEnabled() = %v, want %v", provider.IsOTLPEnabled(), tt.wantEnabled)
			}
		})
	}
}

func TestProviderShutdown(t *testing.T) {
	ctx := context.Background()
	registry := prometheus.NewRegistry()

	otelConfig := &kuadrantotel.Config{
		ServiceName:    "test",
		ServiceVersion: "v1.0.0",
	}
	metricsConfig := &Config{
		PrometheusGatherer: registry,
		ExportInterval:     30 * time.Second,
	}

	provider, err := NewProvider(ctx, otelConfig, metricsConfig)
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}

	// Test shutdown
	if err := provider.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown() error = %v", err)
	}

	// Test double shutdown (should not panic, error is acceptable)
	// Second shutdown may return an error (e.g., "reader is shutdown")
	// which is expected behavior, we just want to ensure it doesn't panic
	_ = provider.Shutdown(ctx)
}

func TestBridgePrometheusMetrics(t *testing.T) {
	ctx := context.Background()
	registry := prometheus.NewRegistry()

	// Create a sample Prometheus counter
	counter := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "test_counter",
		Help: "A test counter",
	})
	registry.MustRegister(counter)
	counter.Inc()

	// Create provider with the registry
	otelConfig := &kuadrantotel.Config{
		ServiceName:    "test",
		ServiceVersion: "v1.0.0",
	}
	metricsConfig := &Config{
		PrometheusGatherer: registry,
		ExportInterval:     30 * time.Second,
	}

	provider, err := NewProvider(ctx, otelConfig, metricsConfig)
	if err != nil {
		t.Fatalf("NewProvider() error = %v", err)
	}
	defer provider.Shutdown(ctx)

	// Verify the counter is registered
	metrics, err := registry.Gather()
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}

	found := false
	for _, m := range metrics {
		if m.GetName() == "test_counter" {
			found = true
			break
		}
	}
	if !found {
		t.Error("test_counter not found in registry")
	}
}
