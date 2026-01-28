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

package otel

import "k8s.io/utils/env"

// Config holds configuration for OpenTelemetry (logs, traces, metrics)
type Config struct {
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
	endpoint := env.GetString("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	insecure, _ := env.GetBool("OTEL_EXPORTER_OTLP_INSECURE", false)

	serviceName := env.GetString("OTEL_SERVICE_NAME", "kuadrant-operator")
	serviceVersion := env.GetString("OTEL_SERVICE_VERSION", version)

	return &Config{
		Endpoint:       endpoint,
		Insecure:       insecure,
		ServiceName:    serviceName,
		ServiceVersion: serviceVersion,
		GitSHA:         gitSHA,
		GitDirty:       dirty,
	}
}

// LogsEndpoint returns the endpoint for logs, with signal-specific override support
// Returns empty string if no endpoint is configured (logs disabled).
func (c *Config) LogsEndpoint() string {
	if endpoint := env.GetString("OTEL_EXPORTER_OTLP_LOGS_ENDPOINT", ""); endpoint != "" {
		return endpoint
	}
	return c.Endpoint
}

// TracesEndpoint returns the endpoint for traces, with signal-specific override support
// Returns empty string if no endpoint is configured (traces disabled).
func (c *Config) TracesEndpoint() string {
	if endpoint := env.GetString("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", ""); endpoint != "" {
		return endpoint
	}
	return c.Endpoint
}

// MetricsEndpoint returns the endpoint for metrics, with signal-specific override support
// Returns empty string if no endpoint is configured (metrics disabled).
func (c *Config) MetricsEndpoint() string {
	if endpoint := env.GetString("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT", ""); endpoint != "" {
		return endpoint
	}
	return c.Endpoint
}
