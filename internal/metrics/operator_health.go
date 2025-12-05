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
	"github.com/prometheus/client_golang/prometheus"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
)

var (
	// dependencyDetected tracks whether each dependency was detected at operator startup.
	// This helps diagnose why certain policy types or controllers may not be available.
	// A value of 1 means the dependency was detected, 0 means it was not detected.
	dependencyDetected = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kuadrant_dependency_detected",
			Help: "Whether a dependency was detected at operator startup (1=detected, 0=not detected)",
		},
		[]string{"dependency"}) // authorino, limitador, cert-manager, dns-operator, istio, envoygateway

	// controllerRegistered tracks whether each controller was registered based on dependency detection at operator startup.
	// A value of 1 means the controller is active, 0 means it was skipped due to missing dependencies.
	controllerRegistered = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kuadrant_controller_registered",
			Help: "Whether a controller was registered at startup and is active (1=registered, 0=not registered)",
		},
		[]string{"controller"}) // auth_policies, rate_limit_policies, dns_policies, tls_policies, etc.

	// kuadrantReady tracks whether the Kuadrant CR has a Ready condition with status True.
	// The Kuadrant CR is responsible for deploying Authorino and Limitador, so this metric is critical
	// for understanding whether the operator can enforce AuthPolicy and RateLimitPolicy.
	// Note: This metric is removed entirely when no Kuadrant CR exists. Use kuadrantExists to detect missing CRs.
	kuadrantReady = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kuadrant_ready",
			Help: "Whether the Kuadrant CR is Ready (1=ready, 0=not ready). Metric is absent when CR doesn't exist.",
		},
		[]string{"namespace", "name"})

	// kuadrantComponentReady tracks the readiness of individual components managed by the Kuadrant CR.
	// This provides granular visibility into whether Authorino and Limitador deployments are ready.
	// Note: This metric is removed entirely when no Kuadrant CR exists. Use kuadrantExists to detect missing CRs.
	kuadrantComponentReady = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kuadrant_component_ready",
			Help: "Whether a Kuadrant-managed component is ready (1=ready, 0=not ready). Metric is absent when CR doesn't exist.",
		},
		[]string{"component", "namespace"}) // component: authorino, limitador

	// kuadrantExists tracks whether a Kuadrant CR exists in the cluster at all.
	// This is important to distinguish between "CR doesn't exist" vs "CR exists but not ready".
	kuadrantExists = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "kuadrant_exists",
			Help: "Whether a Kuadrant CR exists in the cluster (1=exists, 0=does not exist)",
		})
)

func init() {
	// Register metrics with controller-runtime's Prometheus registry
	metrics.Registry.MustRegister(
		dependencyDetected,
		controllerRegistered,
		kuadrantReady,
		kuadrantComponentReady,
		kuadrantExists,
	)
}

// SetDependencyDetected records whether a dependency was detected at startup.
// This should be called once during operator initialization for each dependency.
func SetDependencyDetected(dependency string, detected bool) {
	value := 0.0
	if detected {
		value = 1.0
	}
	dependencyDetected.WithLabelValues(dependency).Set(value)
}

// SetControllerRegistered records whether a controller was registered.
// This should be called after dependency detection determines if controllers should be enabled.
func SetControllerRegistered(controller string, registered bool) {
	value := 0.0
	if registered {
		value = 1.0
	}
	controllerRegistered.WithLabelValues(controller).Set(value)
}

// SetKuadrantReady records whether the Kuadrant CR exists and is Ready.
// This should be called by the KuadrantStatusUpdater reconciler.
func SetKuadrantReady(namespace, name string, ready bool) {
	value := 0.0
	if ready {
		value = 1.0
	}
	kuadrantReady.WithLabelValues(namespace, name).Set(value)
}

// SetComponentReady records whether a Kuadrant-managed component is ready.
// This should be called for each component (authorino, limitador) by the KuadrantStatusUpdater.
func SetComponentReady(component, namespace string, ready bool) {
	value := 0.0
	if ready {
		value = 1.0
	}
	kuadrantComponentReady.WithLabelValues(component, namespace).Set(value)
}

// SetKuadrantExists records whether a Kuadrant CR exists in the cluster at all.
// This should be called by the KuadrantStatusUpdater to distinguish between
// "no CR exists" vs "CR exists but not ready".
func SetKuadrantExists(exists bool) {
	value := 0.0
	if exists {
		value = 1.0
	}
	kuadrantExists.Set(value)
}

// ResetKuadrantMetrics clears all Kuadrant CR-specific metrics.
// This should be called when no Kuadrant CR exists to prevent stale metrics
// from remaining with the last known state.
func ResetKuadrantMetrics() {
	kuadrantReady.Reset()
	kuadrantComponentReady.Reset()
}
