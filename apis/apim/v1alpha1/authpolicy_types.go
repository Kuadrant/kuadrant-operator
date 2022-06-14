package v1alpha1

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	authorinov1beta1 "github.com/kuadrant/authorino/api/v1beta1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

type AuthPolicySpec struct {
	// TargetRef identifies an API object to apply policy to.
	TargetRef gatewayapiv1alpha2.PolicyTargetReference `json:"targetRef"`

	// Rule describe the requests that will be routed to external authorization provider
	AuthRules []*AuthRule `json:"rules,omitempty"`

	// AuthSchemes are embedded Authorino's AuthConfigs
	AuthSchemes []*authorinov1beta1.AuthConfigSpec `json:"authSchemes,omitempty"`
}

type AuthRule struct {
	Hosts   []string `json:"hosts,omitempty"`
	Methods []string `json:"methods,omitempty"`
	Paths   []string `json:"paths,omitempty"`
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

func (ap *AuthPolicy) Validate() error {
	if ap.Spec.TargetRef.Group != gatewayapiv1alpha2.Group("gateway.networking.k8s.io") {
		return fmt.Errorf("invalid targetRef.Group %s. The only supported group is gateway.networking.k8s.io", ap.Spec.TargetRef.Group)
	}

	if ap.Spec.TargetRef.Kind != gatewayapiv1alpha2.Kind("HTTPRoute") {
		return fmt.Errorf("invalid targetRef.Kind %s. The only supported kind is HTTPRoute", ap.Spec.TargetRef.Kind)
	}

	if ap.Spec.TargetRef.Namespace != nil && string(*ap.Spec.TargetRef.Namespace) != ap.Namespace {
		return fmt.Errorf("invalid targetRef.Namespace %s. Currently only supporting references to the same namespace", *ap.Spec.TargetRef.Namespace)
	}
	return nil
}
