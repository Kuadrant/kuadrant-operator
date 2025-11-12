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

package log

import (
	"context"
	"fmt"
	"io"

	"github.com/go-logr/logr"
	"go.opentelemetry.io/contrib/bridges/otellogr"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/log/global"
	sdklog "go.opentelemetry.io/otel/sdk/log"

	"github.com/kuadrant/kuadrant-operator/internal/otel"
)

// loggerProvider holds the global logger provider for shutdown
var loggerProvider *sdklog.LoggerProvider

// SetupOTelLogging sets up OpenTelemetry logging with the given configuration.
// It creates a LoggerProvider with two exporters:
// - OTLP exporter for remote telemetry collection
// - Zap exporter for formatted console output
// Returns a logr.Logger that bridges to OpenTelemetry
func SetupOTelLogging(ctx context.Context, config *otel.Config, zapLevel Level, zapMode Mode, zapWriter io.Writer) (logr.Logger, error) {
	if !config.Enabled {
		return logr.Logger{}, fmt.Errorf("OpenTelemetry logging is not enabled")
	}

	// Create shared resource for service identity (used across all signals)
	res, err := otel.NewResource(ctx, config)
	if err != nil {
		return logr.Logger{}, fmt.Errorf("failed to create resource: %w", err)
	}

	// Create OTLP HTTP exporter for remote telemetry
	opts := []otlploghttp.Option{
		otlploghttp.WithEndpoint(config.LogsEndpoint()),
	}
	if config.Insecure {
		opts = append(opts, otlploghttp.WithInsecure())
	}
	otlpExporter, err := otlploghttp.New(ctx, opts...)
	if err != nil {
		return logr.Logger{}, fmt.Errorf("failed to create OTLP exporter: %w", err)
	}

	// Create Zap exporter for console output
	stdOutExporter := newZapExporter(zapLevel, zapMode, zapWriter)

	// Create logger provider with both exporters:
	// - OTLP for remote collection (all logs)
	// - Zap for console output (respects LOG_LEVEL and LOG_MODE)
	loggerProvider = sdklog.NewLoggerProvider(
		sdklog.WithResource(res),
		sdklog.WithProcessor(sdklog.NewBatchProcessor(otlpExporter)),
		sdklog.WithProcessor(sdklog.NewBatchProcessor(stdOutExporter)),
	)

	// Set as global logger provider
	global.SetLoggerProvider(loggerProvider)

	// Create logr bridge to OpenTelemetry
	logsink := otellogr.NewLogSink(config.ServiceName,
		otellogr.WithLoggerProvider(loggerProvider),
		otellogr.WithVersion(config.ServiceVersion),
	)
	logger := logr.New(logsink)

	return logger, nil
}

// ShutdownOTelLogging gracefully shuts down the OpenTelemetry logger provider
func ShutdownOTelLogging(ctx context.Context) error {
	if loggerProvider != nil {
		return loggerProvider.Shutdown(ctx)
	}
	return nil
}
