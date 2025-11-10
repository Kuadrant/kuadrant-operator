package otel

import "k8s.io/utils/env"

// Config holds configuration for OpenTelemetry (logs, traces, metrics)
type Config struct {
	// Enabled indicates if OpenTelemetry is enabled
	Enabled bool
	// Endpoint is the OTLP endpoint to send telemetry to
	Endpoint string
	// Insecure disables TLS for OTLP export (useful for local development)
	Insecure bool
	// ServiceName is the name of the service
	ServiceName string
	// ServiceVersion is the version of the service
	ServiceVersion string
	// GitSHA is the git commit SHA of the build
	GitSHA string
	// GitDirty indicates if the build had uncommitted changes
	GitDirty string
}

// NewConfig creates OTel configuration from environment variables.
// gitSHA, dirty, and version should be build information injected via ldflags.
func NewConfig(gitSHA, dirty, version string) *Config {
	enabled, _ := env.GetBool("OTEL_ENABLED", false)
	endpoint := env.GetString("OTEL_EXPORTER_OTLP_ENDPOINT", "localhost:4318")
	insecure, _ := env.GetBool("OTEL_EXPORTER_OTLP_INSECURE", true)

	serviceName := env.GetString("OTEL_SERVICE_NAME", "kuadrant-operator")
	serviceVersion := env.GetString("OTEL_SERVICE_VERSION", version)

	return &Config{
		Enabled:        enabled,
		Endpoint:       endpoint,
		Insecure:       insecure,
		ServiceName:    serviceName,
		ServiceVersion: serviceVersion,
		GitSHA:         gitSHA,
		GitDirty:       dirty,
	}
}

// LogsEndpoint returns the endpoint for logs, with signal-specific override support
func (c *Config) LogsEndpoint() string {
	if endpoint := env.GetString("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT", ""); endpoint != "" {
		return endpoint
	}
	return c.Endpoint
}

// MetricsEndpoint returns the endpoint for metrics, with signal-specific override support
func (c *Config) MetricsEndpoint() string {
	if endpoint := env.GetString("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT", ""); endpoint != "" {
		return endpoint
	}
	return c.Endpoint
}
