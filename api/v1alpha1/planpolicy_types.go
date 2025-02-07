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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
)

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// PlanPolicy enables rate limiting through plans of identified requests
type PlanPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   PlanPolicySpec   `json:"spec,omitempty"`
	Status PlanPolicyStatus `json:"status,omitempty"`
}

// PlanPolicySpec defines the desired state of PlanPolicy
// +kubebuilder:validation:XValidation:rule="self.plans.all(a, self.plans.filter(b, a.tier == b.tier).size() == 1)",message="Plan tier names should be unique"
type PlanPolicySpec struct {
	// Reference to the object to which this policy applies.
	// todo(adam-cattermole): This doesn't have to be tied to a particular IdentityPolicy, but could be updated to support other resources
	// +kubebuilder:validation:XValidation:rule="self.group == 'kuadrant.io'",message="Invalid targetRef.group. The only supported value is 'kuadrant.io'"
	// +kubebuilder:validation:XValidation:rule="self.kind == 'XIdentityPolicy'",message="Invalid targetRef.kind. The only supported value is 'XIdentityPolicy'"
	TargetRef gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName `json:"targetRef"`

	// Plans defines the list of plans for the policy. The identity is categorised by the first matching plan in the list.
	Plans []Plan `json:"plans"`
}

type Plan struct {
	// Tier this plan represents.
	Tier string `json:"tier"`

	// Limits contains the list of limits that the plan enforces.
	// +optional
	Limits []Limit `json:"limits,omitempty"`

	// Predicate is a CEL expression used to determine if the plan is applied.
	// +kubebuilder:validation:MinLength=1
	Predicate string `json:"predicate"`
}

type Limit struct {
	// Daily limit of requests for this plan.
	// +optional
	Daily *int `json:"daily,omitempty"`

	// Weekly limit of requests for this plan.
	// +optional
	Weekly *int `json:"weekly,omitempty"`

	// Monthly limit of requests for this plan.
	// +optional
	Monthly *int `json:"monthly,omitempty"`

	// Yearly limit of requests for this plan.
	// +optional
	Yearly *int `json:"yearly,omitempty"`

	// Custom defines any additional limits defined in terms of a RateLimitPolicy Rate.
	// +optional
	Custom []kuadrantv1.Rate `json:"custom,omitempty"`
}

// PlanPolicyStatus defines the observed state of PlanPolicy
type PlanPolicyStatus struct {
}

//+kubebuilder:object:root=true

// PlanPolicyList contains a list of PlanPolicy
type PlanPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []PlanPolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&PlanPolicy{}, &PlanPolicyList{})
}
