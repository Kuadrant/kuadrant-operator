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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// TelemetryPolicy enables rate limiting through plans of identified requests
type TelemetryPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata"`

	Spec   TelemetryPolicySpec   `json:"spec"`
	Status TelemetryPolicyStatus `json:"status"`
}

type TelemetryPolicySpec struct {
	// Reference to the object to which this policy applies.
	// +kubebuilder:validation:XValidation:rule="self.group == 'gateway.networking.k8s.io'",message="Invalid targetRef.group. The only supported value is 'gateway.networking.k8s.io'"
	// +kubebuilder:validation:XValidation:rule="self.kind == 'Gateway'",message="Invalid targetRef.kind. The only supported value is 'Gateway'"
	TargetRef gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName `json:"targetRef"`

	// Metrics holds the telemetry metrics configuration
	Metrics MetricsSpec `json:"metrics"`
}

func (p *TelemetryPolicy) GetName() string {
	return p.Name
}

func (p *TelemetryPolicy) GetNamespace() string {
	return p.Namespace
}

func (p *TelemetryPolicy) GetTargetRefs() []gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName {
	return []gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
		p.Spec.TargetRef,
	}
}

// MetricsSpec defines the configuration for telemetry metrics
type MetricsSpec struct {
	// Default metrics configuration that applies to all requests
	Default MetricsConfig `json:"default"`
}

// MetricsConfig defines reusable metrics configuration that can be applied to different metric types
type MetricsConfig struct {
	// Labels to add to metrics, where keys are label names and values are CEL expressions.
	// Only labels whose CEL expressions resolve successfully will be included.
	// +kubebuilder:validation:MinProperties=1
	Labels map[string]string `json:"labels"`
}

// TelemetryPolicyStatus defines the observed state of TelemetryPolicy
type TelemetryPolicyStatus struct {
	// ObservedGeneration reflects the generation of the most recently observed spec.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Represents the observations of a TelemetryPolicy's current state.
	// Known .status.conditions.type are: "Accepted", "Enforced"
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`
}

//+kubebuilder:object:root=true

// TelemetryPolicyList contains a list of TelemetryPolicy
type TelemetryPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata"`
	Items           []TelemetryPolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&TelemetryPolicy{}, &TelemetryPolicyList{})
}
