package v1beta2

import (
	"fmt"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gatewayapiv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	authorinoapi "github.com/kuadrant/authorino/api/v1beta2"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
)

type AuthSchemeSpec struct {
	// Named sets of patterns that can be referred in `when` conditions and in pattern-matching authorization policy rules.
	// +optional
	NamedPatterns map[string]authorinoapi.PatternExpressions `json:"patterns,omitempty"`

	// Overall conditions for the AuthPolicy to be enforced.
	// If omitted, the AuthPolicy will be enforced at all requests to the protected routes.
	// If present, all conditions must match for the AuthPolicy to be enforced; otherwise, the authorization service skips the AuthPolicy and returns to the auth request with status OK.
	// +optional
	Conditions []authorinoapi.PatternExpressionOrRef `json:"when,omitempty"`

	// TODO(@guicassolato): define top-level `routeSelectors`

	// Authentication configs.
	// At least one config MUST evaluate to a valid identity object for the auth request to be successful.
	// +optional
	Authentication map[string]authorinoapi.AuthenticationSpec `json:"authentication,omitempty"`

	// Metadata sources.
	// Authorino fetches auth metadata as JSON from sources specified in this config.
	// +optional
	Metadata map[string]authorinoapi.MetadataSpec `json:"metadata,omitempty"`

	// Authorization policies.
	// All policies MUST evaluate to "allowed = true" for the auth request be successful.
	// +optional
	Authorization map[string]authorinoapi.AuthorizationSpec `json:"authorization,omitempty"`

	// Response items.
	// Authorino builds custom responses to the client of the auth request.
	// +optional
	Response *authorinoapi.ResponseSpec `json:"response,omitempty"`

	// Callback functions.
	// Authorino sends callbacks at the end of the auth pipeline to the endpoints specified in this config.
	// +optional
	Callbacks map[string]authorinoapi.CallbackSpec `json:"callbacks,omitempty"`
}

type AuthPolicySpec struct {
	// TargetRef identifies an API object to apply policy to.
	TargetRef gatewayapiv1alpha2.PolicyTargetReference `json:"targetRef"`

	// Route rules specify the HTTP route attributes that trigger the external authorization service
	// TODO(@guicassolato): remove – conditions to trigger the ext-authz service will be computed from `routeSelectors`
	RouteRules []RouteRule `json:"routes,omitempty"`

	// The auth rules of the policy.
	// See Authorino's AuthConfig CRD for more details.
	AuthScheme AuthSchemeSpec `json:"rules,omitempty"`
}

type RouteRule struct {
	Hosts   []string `json:"hosts,omitempty"`
	Methods []string `json:"methods,omitempty"`
	Paths   []string `json:"paths,omitempty"`
}

type AuthPolicyStatus struct {
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
}

func (s *AuthPolicyStatus) Equals(other *AuthPolicyStatus, logger logr.Logger) bool {
	if s.ObservedGeneration != other.ObservedGeneration {
		diff := cmp.Diff(s.ObservedGeneration, other.ObservedGeneration)
		logger.V(1).Info("ObservedGeneration not equal", "difference", diff)
		return false
	}

	// Marshalling sorts by condition type
	currentMarshaledJSON, _ := common.ConditionMarshal(s.Conditions)
	otherMarshaledJSON, _ := common.ConditionMarshal(other.Conditions)
	if string(currentMarshaledJSON) != string(otherMarshaledJSON) {
		diff := cmp.Diff(string(currentMarshaledJSON), string(otherMarshaledJSON))
		logger.V(1).Info("Conditions not equal", "difference", diff)
		return false
	}

	return true
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
type AuthPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AuthPolicySpec   `json:"spec,omitempty"`
	Status AuthPolicyStatus `json:"status,omitempty"`
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
	if ap.Spec.TargetRef.Group != ("gateway.networking.k8s.io") {
		return fmt.Errorf("invalid targetRef.Group %s. The only supported group is gateway.networking.k8s.io", ap.Spec.TargetRef.Group)
	}

	switch kind := ap.Spec.TargetRef.Kind; kind {
	case
		"HTTPRoute",
		"Gateway":
	default:
		return fmt.Errorf("invalid targetRef.Kind %s. The only supported kinds are HTTPRoute and Gateway", kind)
	}

	if ap.Spec.TargetRef.Namespace != nil && string(*ap.Spec.TargetRef.Namespace) != ap.Namespace {
		return fmt.Errorf("invalid targetRef.Namespace %s. Currently only supporting references to the same namespace", *ap.Spec.TargetRef.Namespace)
	}
	return nil
}

func (ap *AuthPolicy) GetTargetRef() gatewayapiv1alpha2.PolicyTargetReference {
	return ap.Spec.TargetRef
}

func (ap *AuthPolicy) GetWrappedNamespace() gatewayapiv1beta1.Namespace {
	return gatewayapiv1beta1.Namespace(ap.Namespace)
}

func (ap *AuthPolicy) GetRulesHostnames() (ruleHosts []string) {
	ruleHosts = make([]string, 0)
	for _, rule := range ap.Spec.RouteRules {
		ruleHosts = append(ruleHosts, rule.Hosts...)
	}
	return
}
