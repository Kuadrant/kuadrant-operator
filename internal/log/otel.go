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
	"net/url"

	"github.com/go-logr/logr"
	"github.com/go-logr/zapr"
	"go.opentelemetry.io/contrib/bridges/otelzap"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/log/global"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/kuadrant/kuadrant-operator/internal/otel"
)

// loggerProvider holds the global logger provider for shutdown
var loggerProvider *sdklog.LoggerProvider

// SetupOTelLogging sets up OpenTelemetry logging with the given configuration.
// It creates a Zap logger with a Tee core that sends logs to both:
// - Console output (formatted via Zap encoder)
// - OTel LoggerProvider (for OTLP export to remote collectors) using official otelzap bridge
// Returns a logr.Logger that wraps the Zap logger
func SetupOTelLogging(ctx context.Context, config *otel.Config, zapLevel Level, zapMode Mode, zapWriter io.Writer) (logr.Logger, error) {
	if !config.Enabled {
		return logr.Logger{}, fmt.Errorf("OpenTelemetry logging is not enabled")
	}

	// Create shared resource for service identity (used across all signals)
	res, err := otel.NewResource(ctx, config)
	if err != nil {
		return logr.Logger{}, fmt.Errorf("failed to create resource: %w", err)
	}

	// Create log exporter based on endpoint URL
	otlpExporter, err := newLogExporter(ctx, config)
	if err != nil {
		return logr.Logger{}, fmt.Errorf("failed to create OTLP exporter: %w", err)
	}

	// Create logger provider with OTLP exporter
	loggerProvider = sdklog.NewLoggerProvider(
		sdklog.WithResource(res),
		sdklog.WithProcessor(sdklog.NewBatchProcessor(otlpExporter)),
	)

	// Set as global logger provider
	global.SetLoggerProvider(loggerProvider)

	// Create console and OTel cores, then tee them together
	logger := newLoggerWithOTel(config.ServiceName, loggerProvider, zapLevel, zapMode, zapWriter)

	return logger, nil
}

// newLogExporter creates an OTLP log exporter based on endpoint URL scheme.
// Following the Authorino pattern:
//   - rpc://host:port  → gRPC exporter
//   - http://host:port → HTTP exporter (insecure)
//   - https://host:port → HTTP exporter (secure)
func newLogExporter(ctx context.Context, otelConfig *otel.Config) (sdklog.Exporter, error) {
	u, err := url.Parse(otelConfig.LogsEndpoint())
	if err != nil {
		return nil, fmt.Errorf("invalid endpoint URL: %w", err)
	}

	switch u.Scheme {
	case "rpc":
		opts := []otlploggrpc.Option{
			otlploggrpc.WithEndpoint(u.Host),
		}
		if otelConfig.Insecure {
			opts = append(opts, otlploggrpc.WithInsecure())
		}
		return otlploggrpc.New(ctx, opts...)

	case "http", "https":
		opts := []otlploghttp.Option{
			otlploghttp.WithEndpoint(u.Host),
		}
		if path := u.Path; path != "" {
			opts = append(opts, otlploghttp.WithURLPath(path))
		}
		if otelConfig.Insecure || u.Scheme == "http" {
			opts = append(opts, otlploghttp.WithInsecure())
		}
		return otlploghttp.New(ctx, opts...)

	default:
		return nil, fmt.Errorf("unsupported endpoint scheme: %s (use 'rpc', 'http', or 'https')", u.Scheme)
	}
}

// newLoggerWithOTel creates a zapr logger that sends logs to both console and OTel
func newLoggerWithOTel(serviceName string, provider *sdklog.LoggerProvider, level Level, mode Mode, writer io.Writer) logr.Logger {
	// Create console core (with context field filtering to avoid noisy output)
	consoleCore := &contextFilterCore{
		Core: zapcore.NewCore(
			createEncoder(mode),
			zapcore.AddSync(writer),
			zapcore.Level(level),
		),
	}

	// Create OTel core using official otelzap bridge (needs context for trace extraction)
	otelCore := otelzap.NewCore(
		serviceName,
		otelzap.WithLoggerProvider(provider),
	)

	// Tee both cores - logs go to both console (filtered) and OTel (with context)
	teeCore := zapcore.NewTee(consoleCore, otelCore)
	zapLogger := zap.New(teeCore)

	return zapr.NewLogger(zapLogger)
}

// contextFilterCore wraps a zapcore.Core and extracts trace context from context.Context fields.
// It replaces the noisy "context" field with clean "trace_id" and "span_id" fields for console output.
type contextFilterCore struct {
	zapcore.Core
}

// With extracts trace context from "context" field and adds trace_id/span_id
func (c *contextFilterCore) With(fields []zapcore.Field) zapcore.Core {
	transformed := c.extractTraceContext(fields)
	return &contextFilterCore{Core: c.Core.With(transformed)}
}

// Write extracts trace context from "context" field before writing
func (c *contextFilterCore) Write(entry zapcore.Entry, fields []zapcore.Field) error {
	transformed := c.extractTraceContext(fields)
	return c.Core.Write(entry, transformed)
}

// extractTraceContext finds "context" fields, extracts trace_id and span_id, and removes the noisy context field
func (c *contextFilterCore) extractTraceContext(fields []zapcore.Field) []zapcore.Field {
	result := make([]zapcore.Field, 0, len(fields)+2) // +2 for potential trace_id and span_id

	for _, field := range fields {
		if field.Key == "context" {
			// Try to extract context.Context and get trace info
			if ctx, ok := field.Interface.(context.Context); ok {
				span := trace.SpanFromContext(ctx)
				if span != nil && span.SpanContext().IsValid() {
					spanCtx := span.SpanContext()
					if spanCtx.HasTraceID() {
						result = append(result, zap.String("trace_id", spanCtx.TraceID().String()))
					}
					if spanCtx.HasSpanID() {
						result = append(result, zap.String("span_id", spanCtx.SpanID().String()))
					}
				}
			}
			// Don't include the original "context" field
			continue
		}
		result = append(result, field)
	}

	return result
}

// ShutdownOTelLogging gracefully shuts down the OpenTelemetry logger provider
func ShutdownOTelLogging(ctx context.Context) error {
	if loggerProvider != nil {
		return loggerProvider.Shutdown(ctx)
	}
	return nil
}
