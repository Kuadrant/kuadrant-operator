package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	securityv1beta1 "istio.io/api/security/v1beta1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

// +kubebuilder:validation:Enum=ALLOW;CUSTOM;DENY;AUDIT
type AuthPolicyAction string

type AuthPolicySpec struct {
	// TargetRef identifies an API object to apply policy to.
	TargetRef gatewayapiv1alpha2.PolicyTargetReference `json:"targetRef"`

	// The action to take if the request is matches with the rules.
	Action AuthPolicyAction `json:"action,omitempty"`

	// A list of rules to match the request. A match occurs when at least one rule matches the request.
	Rules []securityv1beta1.Rule `json:"rules,omitempty"`

	// Specifies detailed configuration of the CUSTOM action. Must be used only with CUSTOM action.
	Provider securityv1beta1.AuthorizationPolicy_ExtensionProvider `json:"provider,omitempty"`
}

//+kubebuilder:object:root=true
type AuthPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec AuthPolicySpec `json:"spec,omitempty"`
}

//+kubebuilder:object:root=true

// AuthPolicyList contains a list of AuthPolicy
type AuthPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AuthPolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&AuthPolicy{}, &AuthPolicyList{})
}
