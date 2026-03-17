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

// UpstreamPolicy registers an external gRPC upstream service with the Kuadrant data plane.
type UpstreamPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   UpstreamPolicySpec   `json:"spec,omitempty"`
	Status UpstreamPolicyStatus `json:"status,omitempty"`
}

type UpstreamPolicySpec struct {
	// Reference to the Gateway to which this policy applies.
	// +kubebuilder:validation:XValidation:rule="self.group == 'gateway.networking.k8s.io'",message="Invalid targetRef.group. The only supported value is 'gateway.networking.k8s.io'"
	// +kubebuilder:validation:XValidation:rule="self.kind == 'Gateway'",message="Invalid targetRef.kind. The only supported value is 'Gateway'"
	TargetRef gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName `json:"targetRef"`

	// URL of the upstream gRPC service, e.g. "grpc://my-service.namespace.svc.cluster.local:50051"
	// +kubebuilder:validation:MinLength=1
	URL string `json:"url"`
}

func (p *UpstreamPolicy) GetName() string {
	return p.Name
}

func (p *UpstreamPolicy) GetNamespace() string {
	return p.Namespace
}

func (p *UpstreamPolicy) GetTargetRefs() []gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName {
	return []gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
		p.Spec.TargetRef,
	}
}

// UpstreamPolicyStatus defines the observed state of UpstreamPolicy
type UpstreamPolicyStatus struct {
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`
}

func (s *UpstreamPolicyStatus) Equals(other *UpstreamPolicyStatus, logger logr.Logger) bool {
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

// UpstreamPolicyList contains a list of UpstreamPolicy
type UpstreamPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []UpstreamPolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&UpstreamPolicy{}, &UpstreamPolicyList{})
}
