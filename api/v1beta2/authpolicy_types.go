package v1beta2

import (
	"fmt"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

	// Authentication configs.
	// At least one config MUST evaluate to a valid identity object for the auth request to be successful.
	// +optional
	Authentication map[string]AuthenticationSpec `json:"authentication,omitempty"`

	// Metadata sources.
	// Authorino fetches auth metadata as JSON from sources specified in this config.
	// +optional
	Metadata map[string]MetadataSpec `json:"metadata,omitempty"`

	// Authorization policies.
	// All policies MUST evaluate to "allowed = true" for the auth request be successful.
	// +optional
	Authorization map[string]AuthorizationSpec `json:"authorization,omitempty"`

	// Response items.
	// Authorino builds custom responses to the client of the auth request.
	// +optional
	Response *ResponseSpec `json:"response,omitempty"`

	// Callback functions.
	// Authorino sends callbacks at the end of the auth pipeline to the endpoints specified in this config.
	// +optional
	Callbacks map[string]CallbackSpec `json:"callbacks,omitempty"`
}

type CommonAuthRuleSpec struct {
	// Top-level route selectors.
	// If present, the elements will be used to select HTTPRoute rules that, when activated, trigger the auth rule.
	// At least one selected HTTPRoute rule must match to trigger the auth rule.
	// If no route selectors are specified, the auth rule will be evaluated at all requests to the protected routes.
	// +optional
	RouteSelectors []RouteSelector `json:"routeSelectors,omitempty"`
}

// GetRouteSelectors returns the route selectors of the auth rule spec.
// impl: RouteSelectorsGetter
func (s CommonAuthRuleSpec) GetRouteSelectors() []RouteSelector {
	return s.RouteSelectors
}

type AuthenticationSpec struct {
	authorinoapi.AuthenticationSpec `json:""`
	CommonAuthRuleSpec              `json:""`
}

type MetadataSpec struct {
	authorinoapi.MetadataSpec `json:""`
	CommonAuthRuleSpec        `json:""`
}

type AuthorizationSpec struct {
	authorinoapi.AuthorizationSpec `json:""`
	CommonAuthRuleSpec             `json:""`
}

type ResponseSpec struct {
	// Customizations on the denial status attributes when the request is unauthenticated.
	// For integration of Authorino via proxy, the proxy must honour the response status attributes specified in this config.
	// Default: 401 Unauthorized
	// +optional
	Unauthenticated *authorinoapi.DenyWithSpec `json:"unauthenticated,omitempty"`

	// Customizations on the denial status attributes when the request is unauthorized.
	// For integration of Authorino via proxy, the proxy must honour the response status attributes specified in this config.
	// Default: 403 Forbidden
	// +optional
	Unauthorized *authorinoapi.DenyWithSpec `json:"unauthorized,omitempty"`

	// Response items to be included in the auth response when the request is authenticated and authorized.
	// For integration of Authorino via proxy, the proxy must use these settings to propagate dynamic metadata and/or inject data in the request.
	// +optional
	Success WrappedSuccessResponseSpec `json:"success,omitempty"`
}

type WrappedSuccessResponseSpec struct {
	// Custom success response items wrapped as HTTP headers.
	// For integration of Authorino via proxy, the proxy must use these settings to inject data in the request.
	Headers map[string]HeaderSuccessResponseSpec `json:"headers,omitempty"`

	// Custom success response items wrapped as HTTP headers.
	// For integration of Authorino via proxy, the proxy must use these settings to propagate dynamic metadata.
	// See https://www.envoyproxy.io/docs/envoy/latest/configuration/advanced/well_known_dynamic_metadata
	DynamicMetadata map[string]SuccessResponseSpec `json:"dynamicMetadata,omitempty"`
}

type HeaderSuccessResponseSpec struct {
	SuccessResponseSpec `json:",omitempty"`
}

type SuccessResponseSpec struct {
	SuccessResponseSpec authorinoapi.SuccessResponseSpec `json:""`
	CommonAuthRuleSpec  `json:""`
}

type CallbackSpec struct {
	authorinoapi.CallbackSpec `json:""`
	CommonAuthRuleSpec        `json:""`
}

type AuthPolicySpec struct {
	// TargetRef identifies an API object to apply policy to.
	TargetRef gatewayapiv1alpha2.PolicyTargetReference `json:"targetRef"`

	// Top-level route selectors.
	// If present, the elements will be used to select HTTPRoute rules that, when activated, trigger the external authorization service.
	// At least one selected HTTPRoute rule must match to trigger the AuthPolicy.
	// If no route selectors are specified, the AuthPolicy will be enforced at all requests to the protected routes.
	// +optional
	RouteSelectors []RouteSelector `json:"routeSelectors,omitempty"`

	// The auth rules of the policy.
	// See Authorino's AuthConfig CRD for more details.
	AuthScheme AuthSchemeSpec `json:"rules,omitempty"`
}

// GetRouteSelectors returns the top-level route selectors of the auth scheme.
// impl: RouteSelectorsGetter
func (s AuthPolicySpec) GetRouteSelectors() []RouteSelector {
	return s.RouteSelectors
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

func (ap *AuthPolicy) TargetKey() client.ObjectKey {
	ns := ap.Namespace
	if ap.Spec.TargetRef.Namespace != nil {
		ns = string(*ap.Spec.TargetRef.Namespace)
	}

	return client.ObjectKey{
		Name:      string(ap.Spec.TargetRef.Name),
		Namespace: ns,
	}
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

	appendRuleHosts := func(obj RouteSelectorsGetter) {
		for _, routeSelector := range obj.GetRouteSelectors() {
			ruleHosts = append(ruleHosts, common.HostnamesToStrings(routeSelector.Hostnames)...)
		}
	}

	appendRuleHosts(ap.Spec)
	for _, config := range ap.Spec.AuthScheme.Authentication {
		appendRuleHosts(config)
	}
	for _, config := range ap.Spec.AuthScheme.Metadata {
		appendRuleHosts(config)
	}
	for _, config := range ap.Spec.AuthScheme.Authorization {
		appendRuleHosts(config)
	}
	if response := ap.Spec.AuthScheme.Response; response != nil {
		for _, config := range response.Success.Headers {
			appendRuleHosts(config)
		}
		for _, config := range response.Success.DynamicMetadata {
			appendRuleHosts(config)
		}
	}
	for _, config := range ap.Spec.AuthScheme.Callbacks {
		appendRuleHosts(config)
	}

	return
}
