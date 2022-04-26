package v1alpha1

import (
	"errors"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	securityv1beta1 "istio.io/api/security/v1beta1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

// +kubebuilder:validation:Enum=ALLOW;CUSTOM;DENY;AUDIT
type AuthPolicyAction string

type AuthPolicyConfig struct {
	// The action to take if the request is matches with the rules.
	Action AuthPolicyAction `json:"action"`

	// A list of rules to match the request. A match occurs when at least one rule matches the request.
	Rules []securityv1beta1.Rule `json:"rules,omitempty"`

	// +kubebuilder:default=""
	// Specifies detailed configuration of the CUSTOM action. Must be used only with CUSTOM action.
	Provider string `json:"provider,omitempty"`
}

type AuthPolicySpec struct {
	// TargetRef identifies an API object to apply policy to.
	TargetRef gatewayapiv1alpha2.PolicyTargetReference `json:"targetRef"`

	// +listType=map
	// +listMapKey=action
	// +listMapKey=provider
	// Policy per action type but also per provider if using custom type.
	PolicyConfigs []*AuthPolicyConfig `json:"policy,omitempty"`
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

	for _, policyConfig := range ap.Spec.PolicyConfigs {
		if string(policyConfig.Action) != "CUSTOM" && len(policyConfig.Provider) > 0 {
			return errors.New("provider field is only allowed with action type CUSTOM")
		}
	}
	return nil
}
