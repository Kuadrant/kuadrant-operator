package v1beta1

import (
	"fmt"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	authorinov1beta1 "github.com/kuadrant/authorino/api/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gatewayapiv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/kuadrant/kuadrant-operator/pkg/common"
)

type AuthSchemeSpec struct {
	// Named sets of JSON patterns that can be referred in `when` conditionals and in JSON-pattern matching policy rules.
	Patterns map[string]authorinov1beta1.JSONPatternExpressions `json:"patterns,omitempty"`

	// Conditions for the AuthConfig to be enforced.
	// If omitted, the AuthConfig will be enforced for all requests.
	// If present, all conditions must match for the AuthConfig to be enforced; otherwise, Authorino skips the AuthConfig and returns immediately with status OK.
	Conditions []authorinov1beta1.JSONPattern `json:"when,omitempty"`

	// List of identity sources/authentication modes.
	// At least one config of this list MUST evaluate to a valid identity for a request to be successful in the identity verification phase.
	Identity []*authorinov1beta1.Identity `json:"identity,omitempty"`

	// List of metadata source configs.
	// Authorino fetches JSON content from sources on this list on every request.
	Metadata []*authorinov1beta1.Metadata `json:"metadata,omitempty"`

	// Authorization is the list of authorization policies.
	// All policies in this list MUST evaluate to "true" for a request be successful in the authorization phase.
	Authorization []*authorinov1beta1.Authorization `json:"authorization,omitempty"`

	// List of response configs.
	// Authorino gathers data from the auth pipeline to build custom responses for the client.
	Response []*authorinov1beta1.Response `json:"response,omitempty"`

	// Custom denial response codes, statuses and headers to override default 40x's.
	DenyWith *authorinov1beta1.DenyWith `json:"denyWith,omitempty"`
}

type AuthPolicySpec struct {
	// TargetRef identifies an API object to apply policy to.
	TargetRef gatewayapiv1alpha2.PolicyTargetReference `json:"targetRef"`

	// Rule describe the requests that will be routed to external authorization provider
	AuthRules []AuthRule `json:"rules,omitempty"`

	// AuthSchemes are embedded Authorino's AuthConfigs
	AuthScheme AuthSchemeSpec `json:"authScheme,omitempty"`
}

type AuthRule struct {
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
	for _, rule := range ap.Spec.AuthRules {
		ruleHosts = append(ruleHosts, rule.Hosts...)
	}
	return
}
