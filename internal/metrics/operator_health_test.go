/*
Copyright 2025.

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
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func TestSetDependencyDetected(t *testing.T) {
	tests := []struct {
		name       string
		dependency string
		detected   bool
		wantValue  float64
	}{
		{
			name:       "dependency detected - authorino",
			dependency: "authorino",
			detected:   true,
			wantValue:  1.0,
		},
		{
			name:       "dependency not detected - limitador",
			dependency: "limitador",
			detected:   false,
			wantValue:  0.0,
		},
		{
			name:       "dependency detected - cert-manager",
			dependency: "cert-manager",
			detected:   true,
			wantValue:  1.0,
		},
		{
			name:       "dependency not detected - dns-operator",
			dependency: "dns-operator",
			detected:   false,
			wantValue:  0.0,
		},
		{
			name:       "dependency detected - istio",
			dependency: "istio",
			detected:   true,
			wantValue:  1.0,
		},
		{
			name:       "dependency not detected - envoygateway",
			dependency: "envoygateway",
			detected:   false,
			wantValue:  0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset the metric before test
			dependencyDetected.Reset()

			// Set the metric
			SetDependencyDetected(tt.dependency, tt.detected)

			// Verify the metric value
			metric := &dto.Metric{}
			if err := dependencyDetected.WithLabelValues(tt.dependency).Write(metric); err != nil {
				t.Fatalf("failed to write metric: %v", err)
			}

			if metric.Gauge == nil {
				t.Fatal("expected gauge metric, got nil")
			}

			if *metric.Gauge.Value != tt.wantValue {
				t.Errorf("expected value %v, got %v", tt.wantValue, *metric.Gauge.Value)
			}

			// Verify label
			if len(metric.Label) != 1 {
				t.Fatalf("expected 1 label, got %d", len(metric.Label))
			}
			if *metric.Label[0].Name != "dependency" {
				t.Errorf("expected label name 'dependency', got '%s'", *metric.Label[0].Name)
			}
			if *metric.Label[0].Value != tt.dependency {
				t.Errorf("expected label value '%s', got '%s'", tt.dependency, *metric.Label[0].Value)
			}
		})
	}
}

func TestSetControllerRegistered(t *testing.T) {
	tests := []struct {
		name       string
		controller string
		registered bool
		wantValue  float64
	}{
		{
			name:       "controller registered - auth_policies",
			controller: "auth_policies",
			registered: true,
			wantValue:  1.0,
		},
		{
			name:       "controller not registered - rate_limit_policies",
			controller: "rate_limit_policies",
			registered: false,
			wantValue:  0.0,
		},
		{
			name:       "controller registered - dns_policies",
			controller: "dns_policies",
			registered: true,
			wantValue:  1.0,
		},
		{
			name:       "controller not registered - tls_policies",
			controller: "tls_policies",
			registered: false,
			wantValue:  0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset the metric before test
			controllerRegistered.Reset()

			// Set the metric
			SetControllerRegistered(tt.controller, tt.registered)

			// Verify the metric value
			metric := &dto.Metric{}
			if err := controllerRegistered.WithLabelValues(tt.controller).Write(metric); err != nil {
				t.Fatalf("failed to write metric: %v", err)
			}

			if metric.Gauge == nil {
				t.Fatal("expected gauge metric, got nil")
			}

			if *metric.Gauge.Value != tt.wantValue {
				t.Errorf("expected value %v, got %v", tt.wantValue, *metric.Gauge.Value)
			}

			// Verify label
			if len(metric.Label) != 1 {
				t.Fatalf("expected 1 label, got %d", len(metric.Label))
			}
			if *metric.Label[0].Name != "controller" {
				t.Errorf("expected label name 'controller', got '%s'", *metric.Label[0].Name)
			}
			if *metric.Label[0].Value != tt.controller {
				t.Errorf("expected label value '%s', got '%s'", tt.controller, *metric.Label[0].Value)
			}
		})
	}
}

func TestSetKuadrantReady(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		crName    string
		ready     bool
		wantValue float64
	}{
		{
			name:      "kuadrant ready",
			namespace: "kuadrant-system",
			crName:    "kuadrant",
			ready:     true,
			wantValue: 1.0,
		},
		{
			name:      "kuadrant not ready",
			namespace: "kuadrant-system",
			crName:    "kuadrant",
			ready:     false,
			wantValue: 0.0,
		},
		{
			name:      "kuadrant ready in different namespace",
			namespace: "custom-namespace",
			crName:    "my-kuadrant",
			ready:     true,
			wantValue: 1.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset the metric before test
			kuadrantReady.Reset()

			// Set the metric
			SetKuadrantReady(tt.namespace, tt.crName, tt.ready)

			// Verify the metric value
			metric := &dto.Metric{}
			if err := kuadrantReady.WithLabelValues(tt.namespace, tt.crName).Write(metric); err != nil {
				t.Fatalf("failed to write metric: %v", err)
			}

			if metric.Gauge == nil {
				t.Fatal("expected gauge metric, got nil")
			}

			if *metric.Gauge.Value != tt.wantValue {
				t.Errorf("expected value %v, got %v", tt.wantValue, *metric.Gauge.Value)
			}

			// Verify labels
			if len(metric.Label) != 2 {
				t.Fatalf("expected 2 labels, got %d", len(metric.Label))
			}

			// Labels are sorted alphabetically by name
			expectedLabels := map[string]string{
				"name":      tt.crName,
				"namespace": tt.namespace,
			}

			for _, label := range metric.Label {
				expectedValue, ok := expectedLabels[*label.Name]
				if !ok {
					t.Errorf("unexpected label name: %s", *label.Name)
					continue
				}
				if *label.Value != expectedValue {
					t.Errorf("for label '%s', expected value '%s', got '%s'", *label.Name, expectedValue, *label.Value)
				}
			}
		})
	}
}

func TestSetComponentReady(t *testing.T) {
	tests := []struct {
		name      string
		component string
		namespace string
		ready     bool
		wantValue float64
	}{
		{
			name:      "authorino component ready",
			component: "authorino",
			namespace: "kuadrant-system",
			ready:     true,
			wantValue: 1.0,
		},
		{
			name:      "limitador component not ready",
			component: "limitador",
			namespace: "kuadrant-system",
			ready:     false,
			wantValue: 0.0,
		},
		{
			name:      "authorino component ready in different namespace",
			component: "authorino",
			namespace: "custom-namespace",
			ready:     true,
			wantValue: 1.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset the metric before test
			kuadrantComponentReady.Reset()

			// Set the metric
			SetComponentReady(tt.component, tt.namespace, tt.ready)

			// Verify the metric value
			metric := &dto.Metric{}
			if err := kuadrantComponentReady.WithLabelValues(tt.component, tt.namespace).Write(metric); err != nil {
				t.Fatalf("failed to write metric: %v", err)
			}

			if metric.Gauge == nil {
				t.Fatal("expected gauge metric, got nil")
			}

			if *metric.Gauge.Value != tt.wantValue {
				t.Errorf("expected value %v, got %v", tt.wantValue, *metric.Gauge.Value)
			}

			// Verify labels
			if len(metric.Label) != 2 {
				t.Fatalf("expected 2 labels, got %d", len(metric.Label))
			}

			// Labels are sorted alphabetically by name
			expectedLabels := map[string]string{
				"component": tt.component,
				"namespace": tt.namespace,
			}

			for _, label := range metric.Label {
				expectedValue, ok := expectedLabels[*label.Name]
				if !ok {
					t.Errorf("unexpected label name: %s", *label.Name)
					continue
				}
				if *label.Value != expectedValue {
					t.Errorf("for label '%s', expected value '%s', got '%s'", *label.Name, expectedValue, *label.Value)
				}
			}
		})
	}
}

func TestSetKuadrantExists(t *testing.T) {
	tests := []struct {
		name      string
		exists    bool
		wantValue float64
	}{
		{
			name:      "kuadrant exists",
			exists:    true,
			wantValue: 1.0,
		},
		{
			name:      "kuadrant does not exist",
			exists:    false,
			wantValue: 0.0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set the metric
			SetKuadrantExists(tt.exists)

			// Verify the metric value
			metric := &dto.Metric{}
			if err := kuadrantExists.Write(metric); err != nil {
				t.Fatalf("failed to write metric: %v", err)
			}

			if metric.Gauge == nil {
				t.Fatal("expected gauge metric, got nil")
			}

			if *metric.Gauge.Value != tt.wantValue {
				t.Errorf("expected value %v, got %v", tt.wantValue, *metric.Gauge.Value)
			}

			// Verify no labels (this is a simple gauge without labels)
			if len(metric.Label) != 0 {
				t.Errorf("expected 0 labels, got %d", len(metric.Label))
			}
		})
	}
}

func TestResetKuadrantMetrics(t *testing.T) {
	// Set some values first
	SetKuadrantReady("kuadrant-system", "kuadrant", true)
	SetComponentReady("authorino", "kuadrant-system", true)
	SetComponentReady("limitador", "kuadrant-system", false)

	// Reset the metrics
	ResetKuadrantMetrics()

	// Try to gather metrics - after reset, the metrics should not have any values
	// We can verify this by checking that Write returns an error or that the collector has no metrics
	registry := prometheus.NewRegistry()
	registry.MustRegister(kuadrantReady)
	registry.MustRegister(kuadrantComponentReady)

	metrics, err := registry.Gather()
	if err != nil {
		t.Fatalf("failed to gather metrics: %v", err)
	}

	// After reset, the metric families should exist but have no metrics
	for _, mf := range metrics {
		if mf.GetName() == "kuadrant_ready" || mf.GetName() == "kuadrant_component_ready" {
			if len(mf.GetMetric()) != 0 {
				t.Errorf("expected metric %s to have 0 entries after reset, got %d", mf.GetName(), len(mf.GetMetric()))
			}
		}
	}
}

func TestMultipleUpdates(t *testing.T) {
	t.Run("dependency detection updated multiple times", func(t *testing.T) {
		dependencyDetected.Reset()

		// First update
		SetDependencyDetected("authorino", true)

		metric := &dto.Metric{}
		if err := dependencyDetected.WithLabelValues("authorino").Write(metric); err != nil {
			t.Fatalf("failed to write metric: %v", err)
		}
		if *metric.Gauge.Value != 1.0 {
			t.Errorf("expected value 1.0 after first update, got %v", *metric.Gauge.Value)
		}

		// Second update - toggle to false
		SetDependencyDetected("authorino", false)

		metric = &dto.Metric{}
		if err := dependencyDetected.WithLabelValues("authorino").Write(metric); err != nil {
			t.Fatalf("failed to write metric: %v", err)
		}
		if *metric.Gauge.Value != 0.0 {
			t.Errorf("expected value 0.0 after second update, got %v", *metric.Gauge.Value)
		}
	})

	t.Run("kuadrant readiness updated multiple times", func(t *testing.T) {
		kuadrantReady.Reset()

		// First update - not ready
		SetKuadrantReady("kuadrant-system", "kuadrant", false)

		metric := &dto.Metric{}
		if err := kuadrantReady.WithLabelValues("kuadrant-system", "kuadrant").Write(metric); err != nil {
			t.Fatalf("failed to write metric: %v", err)
		}
		if *metric.Gauge.Value != 0.0 {
			t.Errorf("expected value 0.0 after first update, got %v", *metric.Gauge.Value)
		}

		// Second update - ready
		SetKuadrantReady("kuadrant-system", "kuadrant", true)

		metric = &dto.Metric{}
		if err := kuadrantReady.WithLabelValues("kuadrant-system", "kuadrant").Write(metric); err != nil {
			t.Fatalf("failed to write metric: %v", err)
		}
		if *metric.Gauge.Value != 1.0 {
			t.Errorf("expected value 1.0 after second update, got %v", *metric.Gauge.Value)
		}
	})
}

func TestMultipleDependencies(t *testing.T) {
	dependencyDetected.Reset()

	// Set multiple dependencies
	dependencies := map[string]bool{
		"authorino":    true,
		"limitador":    true,
		"cert-manager": false,
		"dns-operator": false,
		"istio":        true,
		"envoygateway": false,
	}

	for dep, detected := range dependencies {
		SetDependencyDetected(dep, detected)
	}

	// Verify each dependency
	for dep, detected := range dependencies {
		metric := &dto.Metric{}
		if err := dependencyDetected.WithLabelValues(dep).Write(metric); err != nil {
			t.Fatalf("failed to write metric for %s: %v", dep, err)
		}

		expectedValue := 0.0
		if detected {
			expectedValue = 1.0
		}

		if *metric.Gauge.Value != expectedValue {
			t.Errorf("for dependency %s, expected value %v, got %v", dep, expectedValue, *metric.Gauge.Value)
		}
	}
}

func TestMultipleControllers(t *testing.T) {
	controllerRegistered.Reset()

	// Set multiple controllers
	controllers := map[string]bool{
		"auth_policies":        true,
		"rate_limit_policies":  true,
		"dns_policies":         false,
		"tls_policies":         false,
		"token_rate_limiting":  true,
		"gateway_controller":   true,
		"httproute_controller": false,
	}

	for controller, registered := range controllers {
		SetControllerRegistered(controller, registered)
	}

	// Verify each controller
	for controller, registered := range controllers {
		metric := &dto.Metric{}
		if err := controllerRegistered.WithLabelValues(controller).Write(metric); err != nil {
			t.Fatalf("failed to write metric for %s: %v", controller, err)
		}

		expectedValue := 0.0
		if registered {
			expectedValue = 1.0
		}

		if *metric.Gauge.Value != expectedValue {
			t.Errorf("for controller %s, expected value %v, got %v", controller, expectedValue, *metric.Gauge.Value)
		}
	}
}

func TestMultipleComponents(t *testing.T) {
	kuadrantComponentReady.Reset()

	// Set readiness for both components
	SetComponentReady("authorino", "kuadrant-system", true)
	SetComponentReady("limitador", "kuadrant-system", false)

	// Verify authorino is ready
	authMetric := &dto.Metric{}
	if err := kuadrantComponentReady.WithLabelValues("authorino", "kuadrant-system").Write(authMetric); err != nil {
		t.Fatalf("failed to write authorino metric: %v", err)
	}
	if *authMetric.Gauge.Value != 1.0 {
		t.Errorf("expected authorino value 1.0, got %v", *authMetric.Gauge.Value)
	}

	// Verify limitador is not ready
	limitadorMetric := &dto.Metric{}
	if err := kuadrantComponentReady.WithLabelValues("limitador", "kuadrant-system").Write(limitadorMetric); err != nil {
		t.Fatalf("failed to write limitador metric: %v", err)
	}
	if *limitadorMetric.Gauge.Value != 0.0 {
		t.Errorf("expected limitador value 0.0, got %v", *limitadorMetric.Gauge.Value)
	}
}
