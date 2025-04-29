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
	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
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

func (k *Kuadrant) BasicResilienceStatus() ResilienceStatus {
	result := ResilienceStatus{}
	result.RateLimiting = ptr.To((k.Spec.Resilience != nil && k.Spec.Resilience.RateLimiting))
	if k.Spec.Resilience != nil && k.Spec.Resilience.CounterStorage != nil {
		result.CounterStorage = ptr.To(true)
	} else {
		result.CounterStorage = ptr.To(false)
	}
	return result
}

// KuadrantSpec defines the desired state of Kuadrant
type KuadrantSpec struct {
	Observability Observability `json:"observability,omitempty"`
	// +optional
	// MTLS is an optional entry which when enabled is set to true, kuadrant-operator
	// will add the configuration required to enable mTLS between an Istio provided
	// gateway and the Kuadrant components.
	MTLS *MTLS `json:"mtls,omitempty"`

	// +optional
	// Resilience is an optional entry which enables different control plane resilience features.
	Resilience *Resilience `json:"resilience,omitempty"`
}

type Observability struct {
	Enable bool `json:"enable,omitempty"`
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

// +kubebuilder:validation:XValidation:rule="has(self.rateLimiting) ? (self.rateLimiting == true && has(self.counterStorage)) || (self.rateLimiting == false && has(self.counterStorage)) || (self.rateLimiting == false && !has(self.counterStorage)) : true",message="resilience.counterStorage needs to be explictly configured when using resilience.rateLimiting."
type Resilience struct {
	RateLimiting   bool                       `json:"rateLimiting,omitempty"`
	CounterStorage *limitadorv1alpha1.Storage `json:"counterStorage,omitempty"`
}

func (r *Resilience) IsRateLimitingConfigured() bool {
	if r == nil {
		return false
	}
	return r.RateLimiting
}

type ResilienceStatus struct {
	RateLimiting   *bool `json:"rateLimiting,omitempty"`
	CounterStorage *bool `json:"counterStorage,omitempty"`
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

	// Resilience reflects the resilience deployment state
	// +optional
	Resilience *ResilienceStatus `json:"resilience,omitempty"`
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
