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

package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/utils/env"
)

// Config holds metrics-specific configuration
type Config struct {
	// ExportInterval is the interval for periodic metric export
	ExportInterval time.Duration
	// PrometheusGatherer is the Prometheus gatherer to bridge metrics from
	// This can be any type that implements prometheus.Gatherer interface
	// (e.g., prometheus.Registry, controller-runtime's metrics.Registry)
	PrometheusGatherer prometheus.Gatherer
}

// NewConfig creates metrics configuration from environment variables
func NewConfig(gatherer prometheus.Gatherer) *Config {
	intervalSeconds, _ := env.GetInt("OTEL_METRICS_INTERVAL_SECONDS", 15)

	return &Config{
		ExportInterval:     time.Duration(intervalSeconds) * time.Second,
		PrometheusGatherer: gatherer,
	}
}
