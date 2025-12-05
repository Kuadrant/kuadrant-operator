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

	// controllerRegistered tracks whether each controller was registered based on dependency detection.
	// A value of 1 means the controller is active, 0 means it was skipped due to missing dependencies.
	controllerRegistered = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "kuadrant_controller_registered",
			Help: "Whether a controller was registered and is active (1=registered, 0=not registered)",
		},
		[]string{"controller"}) // auth_policies, rate_limit_policies, dns_policies, tls_policies, etc.
)

func init() {
	// Register metrics with controller-runtime's Prometheus registry
	metrics.Registry.MustRegister(dependencyDetected, controllerRegistered)
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
