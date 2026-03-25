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

// ThreatPolicy attaches to HTTPRoutes or Gateways to enforce threat assessment
// using an external ThreatAssessmentService.
type ThreatPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ThreatPolicySpec   `json:"spec,omitempty"`
	Status ThreatPolicyStatus `json:"status,omitempty"`
}

type ThreatPolicySpec struct {
	// Reference to the Gateway API resource to which this policy applies.
	// +kubebuilder:validation:XValidation:rule="self.group == 'gateway.networking.k8s.io'",message="Invalid targetRef.group. The only supported value is 'gateway.networking.k8s.io'"
	// +kubebuilder:validation:XValidation:rule="self.kind == 'HTTPRoute' || self.kind == 'Gateway'",message="Invalid targetRef.kind. The only supported values are 'HTTPRoute' and 'Gateway'"
	TargetRef gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName `json:"targetRef"`

	// Threshold is the threat level threshold (0-10) for blocking requests.
	// Requests with a threat score at or above this threshold will be blocked.
	// +kubebuilder:validation:Minimum=0
	// +kubebuilder:validation:Maximum=10
	Threshold int `json:"threshold"`
}

func (p *ThreatPolicy) GetName() string {
	return p.Name
}

func (p *ThreatPolicy) GetNamespace() string {
	return p.Namespace
}

func (p *ThreatPolicy) GetTargetRefs() []gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName {
	return []gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
		p.Spec.TargetRef,
	}
}

// ThreatPolicyStatus defines the observed state of ThreatPolicy
type ThreatPolicyStatus struct {
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`
}

func (s *ThreatPolicyStatus) Equals(other *ThreatPolicyStatus, logger logr.Logger) bool {
	if s.ObservedGeneration != other.ObservedGeneration {
		diff := cmp.Diff(s.ObservedGeneration, other.ObservedGeneration)
		logger.V(1).Info("status observedGeneration not equal", "difference", diff)
		return false
	}

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

// ThreatPolicyList contains a list of ThreatPolicy
type ThreatPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ThreatPolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ThreatPolicy{}, &ThreatPolicyList{})
}
