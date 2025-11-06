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
	"os"
	"runtime"

	"github.com/go-logr/logr"
	"go.opentelemetry.io/contrib/bridges/otellogr"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/stdout/stdoutlog"
	"go.opentelemetry.io/otel/log/global"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.27.0"
)

// OTelConfig holds configuration for OpenTelemetry logging
type OTelConfig struct {
	// Enabled determines if OpenTelemetry logging is enabled
	Enabled bool
	// Endpoint is the OTLP endpoint to send logs to
	Endpoint string
	// ServiceName is the name of the service
	ServiceName string
	// ServiceVersion is the version of the service
	ServiceVersion string
	// GitSHA is the git commit SHA of the build
	GitSHA string
	// GitDirty indicates if the build had uncommitted changes
	GitDirty string
}

// NewOTelConfigFromEnv creates OTelConfig from environment variables
// version, gitSHA, and dirty should be build information injected via ldflags
func NewOTelConfigFromEnv(version, gitSHA, dirty string) *OTelConfig {
	// Check if OTel logging is explicitly enabled
	enabled := os.Getenv("OTEL_LOGS_ENABLED") == "true"

	// Get endpoint, preferring logs-specific endpoint
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT")
	if endpoint == "" {
		endpoint = os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	}

	serviceName := os.Getenv("OTEL_SERVICE_NAME")
	if serviceName == "" {
		serviceName = "kuadrant-operator"
	}

	// Use the build version, but allow override via environment variable
	serviceVersion := os.Getenv("OTEL_SERVICE_VERSION")
	if serviceVersion == "" {
		serviceVersion = version
	}

	return &OTelConfig{
		Enabled:        enabled,
		Endpoint:       endpoint,
		ServiceName:    serviceName,
		ServiceVersion: serviceVersion,
		GitSHA:         gitSHA,
		GitDirty:       dirty,
	}
}

// loggerProvider holds the global logger provider for shutdown
var loggerProvider *sdklog.LoggerProvider

// SetupOTelLogging sets up OpenTelemetry logging with the given configuration
// Returns a logr.Logger that bridges to OpenTelemetry
func SetupOTelLogging(ctx context.Context, config *OTelConfig) (logr.Logger, error) {
	if !config.Enabled {
		return logr.Logger{}, fmt.Errorf("OpenTelemetry logging is not enabled")
	}

	// Build resource attributes
	attrs := []attribute.KeyValue{
		semconv.ServiceName(config.ServiceName),
		semconv.ServiceVersion(config.ServiceVersion),
	}

	// Add VCS (version control system) attributes
	if config.GitSHA != "" {
		attrs = append(attrs, attribute.String("vcs.revision", config.GitSHA))
	}
	if config.GitDirty != "" {
		attrs = append(attrs, attribute.String("vcs.dirty", config.GitDirty))
	}

	// Add build information
	attrs = append(attrs, attribute.String("build.go.version", runtime.Version()))

	// Create resource with service information
	res, err := resource.New(ctx,
		resource.WithAttributes(attrs...),
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
	)
	if err != nil {
		return logr.Logger{}, fmt.Errorf("failed to create resource: %w", err)
	}

	// Create OTLP HTTP exporter
	otlpExporter, err := otlploghttp.New(ctx,
		otlploghttp.WithEndpoint(config.Endpoint),
		otlploghttp.WithInsecure(), // TODO: Use HTTP instead of HTTPS for local development
	)
	if err != nil {
		return logr.Logger{}, fmt.Errorf("failed to create OTLP exporter: %w", err)
	}

	// TODO: This is good for local development without needing to look logs remotely but do we want to keep this in production?
	// Create console/stdout exporter
	stdoutExporter, err := stdoutlog.New()
	if err != nil {
		return logr.Logger{}, fmt.Errorf("failed to create stdout exporter: %w", err)
	}

	// Create logger provider with OTLP exporter
	loggerProvider = sdklog.NewLoggerProvider(
		sdklog.WithResource(res),
		sdklog.WithProcessor(sdklog.NewBatchProcessor(otlpExporter)),
		sdklog.WithProcessor(sdklog.NewBatchProcessor(stdoutExporter)),
	)

	// Set as global logger provider
	global.SetLoggerProvider(loggerProvider)

	// Create logr bridge to OpenTelemetry
	logsink := otellogr.NewLogSink(config.ServiceName,
		otellogr.WithLoggerProvider(loggerProvider),
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
