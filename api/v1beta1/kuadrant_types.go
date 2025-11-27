/*
Copyright 2021.

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

package v1beta1

import (
	"reflect"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	"github.com/kuadrant/policy-machinery/machinery"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"

	"github.com/kuadrant/kuadrant-operator/internal/kuadrant"
)

var (
	KuadrantGroupKind = schema.GroupKind{Group: GroupVersion.Group, Kind: "Kuadrant"}

	KuadrantsResource = GroupVersion.WithResource("kuadrants")
)

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="Status",type=string,JSONPath=`.status.conditions[0].reason`,priority=2
//+kubebuilder:printcolumn:name="mTLS Authorino",type=boolean,JSONPath=".status.mtlsAuthorino"
//+kubebuilder:printcolumn:name="mTLS Limitador",type=boolean,JSONPath=".status.mtlsLimitador"
//+kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// Kuadrant configures installations of Kuadrant Service Protection components
type Kuadrant struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   KuadrantSpec   `json:"spec,omitempty"`
	Status KuadrantStatus `json:"status,omitempty"`
}

var _ machinery.Object = &Kuadrant{}

func (k *Kuadrant) GetLocator() string {
	return machinery.LocatorFromObject(k)
}

func (k *Kuadrant) IsMTLSLimitadorEnabled() bool {
	if k == nil {
		return false
	}
	return k.Spec.MTLS.IsLimitadorEnabled()
}

func (k *Kuadrant) IsMTLSAuthorinoEnabled() bool {
	if k == nil {
		return false
	}
	return k.Spec.MTLS.IsAuthorinoEnabled()
}

// KuadrantSpec defines the desired state of Kuadrant
type KuadrantSpec struct {
	Observability Observability `json:"observability,omitempty"`
	// +optional
	// MTLS is an optional entry which when enabled is set to true, kuadrant-operator
	// will add the configuration required to enable mTLS between an Istio provided
	// gateway and the Kuadrant components.
	MTLS *MTLS `json:"mtls,omitempty"`
}

// Observability configures telemetry and monitoring settings for Kuadrant components.
// When enabled, it configures logging, tracing, and other observability features for both
// the control plane and data plane components.
type Observability struct {
	// Enable controls whether observability features are active.
	// When false, no additional logging or tracing configuration is applied.
	Enable bool `json:"enable,omitempty"`

	// DataPlane configures observability settings for the data plane components.
	// +optional
	DataPlane *DataPlane `json:"dataPlane,omitempty"`

	// Tracing configures distributed tracing for request flows through the system.
	// +optional
	Tracing *Tracing `json:"tracing"`
}

// DataPlane configures logging and observability for data plane components.
// It controls logging behavior and request-level observability features.
type DataPlane struct {
	// DefaultLevels specifies the default logging levels and their activation predicates.
	// Each entry defines a log level (debug, info, warn, error) and an optional CEL expression
	// that determines when that level should be active for a given request.
	// +optional
	DefaultLevels []LogLevel `json:"defaultLevels,omitempty"`

	// HTTPHeaderIdentifier specifies the HTTP header name used to identify and correlate
	// requests in logs and traces (e.g., "x-request-id", "x-correlation-id").
	// If set, this header value will be included in log output for request correlation.
	// +optional
	HTTPHeaderIdentifier *string `json:"httpHeaderIdentifier"`
}

// Tracing configures distributed tracing integration for request flows.
// It enables tracing spans to be exported to external tracing systems (e.g., Jaeger, Zipkin).
type Tracing struct {
	// DefaultEndpoint is the default URL of the tracing collector backend where spans should be sent.
	// Can be overridden per-gateway in future versions.
	DefaultEndpoint string `json:"defaultEndpoint,omitempty"`

	// Insecure controls whether to skip TLS certificate verification.
	Insecure bool `json:"insecure,omitempty"`
}

// LogLevel defines a logging level with its activation predicate
// Only one field should be set per LogLevel entry
type LogLevel struct {
	// Debug level - highest verbosity
	// +optional
	Debug *string `json:"debug,omitempty"`
	// Info level
	// +optional
	Info *string `json:"info,omitempty"`
	// Warn level
	// +optional
	Warn *string `json:"warn,omitempty"`
	// Error level - lowest verbosity
	// +optional
	Error *string `json:"error,omitempty"`
}

type MTLS struct {
	Enable bool `json:"enable,omitempty"`

	// +optional
	Authorino *bool `json:"authorino,omitempty"`

	// +optional
	Limitador *bool `json:"limitador,omitempty"`
}

func (m *MTLS) IsLimitadorEnabled() bool {
	if m == nil {
		return false
	}

	return m.Enable && ptr.Deref(m.Limitador, m.Enable)
}

func (m *MTLS) IsAuthorinoEnabled() bool {
	if m == nil {
		return false
	}

	return m.Enable && ptr.Deref(m.Authorino, m.Enable)
}

// KuadrantStatus defines the observed state of Kuadrant
type KuadrantStatus struct {
	// ObservedGeneration reflects the generation of the most recently observed spec.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Represents the observations of a foo's current state.
	// Known .status.conditions.type are: "Available"
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`

	// Mtls Authorino reflects the mtls feature state regarding comms with authorino.
	// +optional
	MtlsAuthorino *bool `json:"mtlsAuthorino,omitempty"`

	// Mtls Limitador reflects the mtls feature state regarding comms with limitador.
	// +optional
	MtlsLimitador *bool `json:"mtlsLimitador,omitempty"`
}

func (r *KuadrantStatus) Equals(other *KuadrantStatus, logger logr.Logger) bool {
	if r.ObservedGeneration != other.ObservedGeneration {
		diff := cmp.Diff(r.ObservedGeneration, other.ObservedGeneration)
		logger.V(1).Info("ObservedGeneration not equal", "difference", diff)
		return false
	}

	if !reflect.DeepEqual(r.MtlsAuthorino, other.MtlsAuthorino) {
		diff := cmp.Diff(r.MtlsAuthorino, other.MtlsAuthorino)
		logger.V(1).Info("MtlsAuthorino not equal", "difference", diff)
		return false
	}

	if !reflect.DeepEqual(r.MtlsLimitador, other.MtlsLimitador) {
		diff := cmp.Diff(r.MtlsLimitador, other.MtlsLimitador)
		logger.V(1).Info("MtlsAuthorino not equal", "difference", diff)
		return false
	}

	// Marshalling sorts by condition type
	currentMarshaledJSON, _ := kuadrant.ConditionMarshal(r.Conditions)
	otherMarshaledJSON, _ := kuadrant.ConditionMarshal(other.Conditions)
	if string(currentMarshaledJSON) != string(otherMarshaledJSON) {
		diff := cmp.Diff(string(currentMarshaledJSON), string(otherMarshaledJSON))
		logger.V(1).Info("Conditions not equal", "difference", diff)
		return false
	}

	return true
}

//+kubebuilder:object:root=true

// KuadrantList contains a list of Kuadrant
type KuadrantList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Kuadrant `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Kuadrant{}, &KuadrantList{})
}
