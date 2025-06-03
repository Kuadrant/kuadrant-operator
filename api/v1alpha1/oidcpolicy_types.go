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
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// OIDCPolicySpec defines the desired state of OIDCPolicy
type OIDCPolicySpec struct {
	// Reference to the object to which this policy applies.
	// +kubebuilder:validation:XValidation:rule="self.group == 'gateway.networking.k8s.io'",message="Invalid targetRef.group. The only supported value is 'gateway.networking.k8s.io'"
	// +kubebuilder:validation:XValidation:rule="self.kind == 'HTTPRoute' || self.kind == 'Gateway'",message="Invalid targetRef.kind. The only supported values are 'HTTPRoute' and 'Gateway'"
	TargetRef gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName `json:"targetRef"`

	// Bare set of policy rules (implicit defaults).
	OIDCPolicySpecProper `json:""`
}

// OIDCPolicySpecProper contains common shared fields for the future inclusion of defaults and overrides
type OIDCPolicySpecProper struct {
	// Provider holds the information for the OIDC provider
	Provider *Provider `json:"provider,omitempty"`
}

type Provider struct {
	IssuerURL string `json:"issuerURL"`
	ClientID  string `json:"clientID"`
}

// OIDCPolicyStatus defines the observed state of OIDCPolicy
type OIDCPolicyStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// OIDCPolicy is the Schema for the oidcpolicies API
type OIDCPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   OIDCPolicySpec   `json:"spec,omitempty"`
	Status OIDCPolicyStatus `json:"status,omitempty"`
}

func (p *OIDCPolicy) GetTargetRefs() []gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName {
	return []gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
		{
			LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
				Group: p.Spec.TargetRef.Group,
				Kind:  p.Spec.TargetRef.Kind,
				Name:  p.Spec.TargetRef.Name,
			},
		},
	}
}

// +kubebuilder:object:root=true

// OIDCPolicyList contains a list of OIDCPolicy
type OIDCPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []OIDCPolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&OIDCPolicy{}, &OIDCPolicyList{})
}
