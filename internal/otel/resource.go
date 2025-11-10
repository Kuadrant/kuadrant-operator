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

import (
	"context"
	"runtime"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.27.0"
)

// NewResource creates a shared OpenTelemetry resource for all signals (logs, traces, metrics).
// The resource represents the service identity and should be consistent across all telemetry signals
// to enable correlation in observability backends.
func NewResource(ctx context.Context, config *Config) (*resource.Resource, error) {
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
	return resource.New(ctx,
		resource.WithAttributes(attrs...),
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
	)
}
