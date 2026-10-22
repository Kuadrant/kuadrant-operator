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
	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	extctrl "github.com/kuadrant/kuadrant-operator/pkg/extension/controller"
)

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// TelemetryPolicy enables custom metric labelling and log field enrichment for
// Kuadrant data plane resources through the use of dynamically evaluated CEL expressions.
type TelemetryPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TelemetryPolicySpec   `json:"spec,omitempty"`
	Status TelemetryPolicyStatus `json:"status,omitempty"`
}

// +kubebuilder:validation:XValidation:rule="has(self.metrics) || has(self.logging)",message="At least one of spec.metrics or spec.logging must be defined"
type TelemetryPolicySpec struct {
	// Reference to the object to which this policy applies.
	// +kubebuilder:validation:XValidation:rule="self.group == 'gateway.networking.k8s.io'",message="Invalid targetRef.group. The only supported value is 'gateway.networking.k8s.io'"
	// +kubebuilder:validation:XValidation:rule="self.kind == 'Gateway'",message="Invalid targetRef.kind. The only supported value is 'Gateway'"
	TargetRef gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName `json:"targetRef"`

	// Metrics holds the telemetry metrics configuration
	// +optional
	Metrics *MetricsSpec `json:"metrics,omitempty"`

	// Logging holds the telemetry logging configuration
	// +optional
	Logging *LoggingSpec `json:"logging,omitempty"`
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

// LoggingSpec defines the configuration for telemetry logging
type LoggingSpec struct {
	// Default logging configuration that applies to all requests
	Default LoggingConfig `json:"default"`
}

// LoggingConfig defines reusable logging configuration.
// +kubebuilder:validation:XValidation:rule="has(self.fields) || has(self.ocsf)",message="at least one of fields or ocsf must be configured"
type LoggingConfig struct {
	// Fields to add to auth decision log records, where keys are field names and values are
	// CEL expressions referencing well-known attributes (e.g. auth.identity.sub).
	// Only fields whose CEL expressions resolve successfully will be included.
	// +optional
	// +kubebuilder:validation:MinProperties=1
	// +kubebuilder:validation:XValidation:rule="self.all(k, !k.contains('.'))",message="field names must not contain periods"
	Fields map[string]string `json:"fields,omitempty"`

	// OCSF configures a canonical OCSF event emitted by the gateway when request processing completes.
	// Unlike Fields, this is a fixed, security-sensitive event profile and does not accept arbitrary
	// request headers, query strings, or payload expressions.
	// +optional
	OCSF *OCSFLoggingConfig `json:"ocsf,omitempty"`
}

// OCSFProfile identifies a supported OCSF event preset.
// +kubebuilder:validation:Enum=aiOperation
type OCSFProfile string

const (
	// OCSFProfileAIOperation emits an OCSF 1.9.0 API Activity event with the ai_operation profile.
	OCSFProfileAIOperation OCSFProfile = "aiOperation"
)

// OCSFLoggingConfig configures gateway request-completion audit events.
type OCSFLoggingConfig struct {
	// Profile selects the fixed OCSF event preset.
	Profile OCSFProfile `json:"profile"`

	// Actor selects authenticated identity metadata supplied by Authorino.
	Actor OCSFActorConfig `json:"actor"`

	// Target identifies the resource and AI model from safe request-path segments.
	Target OCSFTargetConfig `json:"target"`
}

// OCSFActorConfig configures the authenticated actor included in an OCSF event.
type OCSFActorConfig struct {
	// Identity is a CEL field path rooted in Authorino's auth.identity metadata. Only dotted field
	// paths are accepted; arbitrary CEL expressions could bypass the intended request-data boundary.
	// +kubebuilder:validation:MinLength=6
	// +kubebuilder:validation:Pattern=`^auth\.identity(\.[A-Za-z_][A-Za-z0-9_]*)+$`
	Identity string `json:"identity"`

	// UID optionally selects the authenticated actor UID from Authorino auth metadata.
	// +optional
	// +kubebuilder:validation:MinLength=6
	// +kubebuilder:validation:Pattern=`^auth\.identity(\.[A-Za-z_][A-Za-z0-9_]*)+$`
	UID string `json:"uid,omitempty"`

	// Groups optionally selects a list of authenticated group names from Authorino auth metadata.
	// +optional
	// +kubebuilder:validation:MinLength=6
	// +kubebuilder:validation:Pattern=`^auth\.identity(\.[A-Za-z_][A-Za-z0-9_]*)+$`
	Groups string `json:"groups,omitempty"`
}

// OCSFTargetConfig configures the audited target resource and AI model.
type OCSFTargetConfig struct {
	// ResourceType is the resource type written to the OCSF resources array.
	// +kubebuilder:validation:MinLength=1
	ResourceType string `json:"resourceType"`

	// RequestPath selects resource namespace and name from the URL path after removing its leading slash.
	RequestPath OCSFRequestPathTarget `json:"requestPath"`

	// AIProvider is written to the OCSF ai_model object. The model name is the target resource name.
	// +kubebuilder:validation:MinLength=1
	AIProvider string `json:"aiProvider"`
}

// OCSFRequestPathTarget maps safe URL path segments to target resource fields.
// +kubebuilder:validation:XValidation:rule="self.namespaceSegment != self.nameSegment",message="namespaceSegment and nameSegment must differ"
type OCSFRequestPathTarget struct {
	// NamespaceSegment is the zero-based path segment containing the resource namespace.
	// +kubebuilder:validation:Minimum=0
	NamespaceSegment int32 `json:"namespaceSegment"`

	// NameSegment is the zero-based path segment containing the resource name.
	// +kubebuilder:validation:Minimum=0
	NameSegment int32 `json:"nameSegment"`
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

func (s *TelemetryPolicyStatus) Equals(other *TelemetryPolicyStatus, logger logr.Logger) bool {
	if s.ObservedGeneration != other.ObservedGeneration {
		diff := cmp.Diff(s.ObservedGeneration, other.ObservedGeneration)
		logger.V(1).Info("status observedGeneration not equal", "difference", diff)
		return false
	}

	// Marshalling sorts by condition type
	currentMarshaledJSON, _ := extctrl.ConditionMarshal(s.Conditions)
	otherMarshaledJSON, _ := extctrl.ConditionMarshal(other.Conditions)
	if string(currentMarshaledJSON) != string(otherMarshaledJSON) {
		diff := cmp.Diff(string(currentMarshaledJSON), string(otherMarshaledJSON))
		logger.V(1).Info("status conditions not equal", "difference", diff)
		return false
	}

	return true
}

//+kubebuilder:object:root=true

// TelemetryPolicyList contains a list of TelemetryPolicy
type TelemetryPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TelemetryPolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&TelemetryPolicy{}, &TelemetryPolicyList{})
}
