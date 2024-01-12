package v1beta2

import (
	"fmt"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	authorinoapi "github.com/kuadrant/authorino/api/v1beta2"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
)

type AuthSchemeSpec struct {
	// Authentication configs.
	// At least one config MUST evaluate to a valid identity object for the auth request to be successful.
	// +optional
	// +kubebuilder:validation:MaxProperties=14
	Authentication map[string]AuthenticationSpec `json:"authentication,omitempty"`

	// Metadata sources.
	// Authorino fetches auth metadata as JSON from sources specified in this config.
	// +optional
	// +kubebuilder:validation:MaxProperties=14
	Metadata map[string]MetadataSpec `json:"metadata,omitempty"`

	// Authorization policies.
	// All policies MUST evaluate to "allowed = true" for the auth request be successful.
	// +optional
	// +kubebuilder:validation:MaxProperties=14
	Authorization map[string]AuthorizationSpec `json:"authorization,omitempty"`

	// Response items.
	// Authorino builds custom responses to the client of the auth request.
	// +optional
	Response *ResponseSpec `json:"response,omitempty"`

	// Callback functions.
	// Authorino sends callbacks at the end of the auth pipeline to the endpoints specified in this config.
	// +optional
	// +kubebuilder:validation:MaxProperties=14
	Callbacks map[string]CallbackSpec `json:"callbacks,omitempty"`
}

type CommonAuthRuleSpec struct {
	// Top-level route selectors.
	// If present, the elements will be used to select HTTPRoute rules that, when activated, trigger the auth rule.
	// At least one selected HTTPRoute rule must match to trigger the auth rule.
	// If no route selectors are specified, the auth rule will be evaluated at all requests to the protected routes.
	// +optional
	// +kubebuilder:validation:MaxItems=15
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
	// +kubebuilder:validation:MaxProperties=14
	Headers map[string]HeaderSuccessResponseSpec `json:"headers,omitempty"`

	// Custom success response items wrapped as HTTP headers.
	// For integration of Authorino via proxy, the proxy must use these settings to propagate dynamic metadata.
	// See https://www.envoyproxy.io/docs/envoy/latest/configuration/advanced/well_known_dynamic_metadata
	// +kubebuilder:validation:MaxProperties=14
	DynamicMetadata map[string]SuccessResponseSpec `json:"dynamicMetadata,omitempty"`
}

type HeaderSuccessResponseSpec struct {
	SuccessResponseSpec `json:""`
}

type SuccessResponseSpec struct {
	authorinoapi.SuccessResponseSpec `json:""`
	CommonAuthRuleSpec               `json:""`
}

type CallbackSpec struct {
	authorinoapi.CallbackSpec `json:""`
	CommonAuthRuleSpec        `json:""`
}

// +kubebuilder:validation:XValidation:rule="self.targetRef.kind != 'Gateway' || !has(self.routeSelectors)",message="route selectors not supported when targeting a Gateway"
// +kubebuilder:validation:XValidation:rule="self.targetRef.kind != 'Gateway' || !has(self.rules.authentication) || !self.rules.authentication.exists(x, has(self.rules.authentication[x].routeSelectors))",message="route selectors not supported when targeting a Gateway"
// +kubebuilder:validation:XValidation:rule="self.targetRef.kind != 'Gateway' || !has(self.rules.metadata) || !self.rules.metadata.exists(x, has(self.rules.metadata[x].routeSelectors))",message="route selectors not supported when targeting a Gateway"
// +kubebuilder:validation:XValidation:rule="self.targetRef.kind != 'Gateway' || !has(self.rules.authorization) || !self.rules.authorization.exists(x, has(self.rules.authorization[x].routeSelectors))",message="route selectors not supported when targeting a Gateway"
// +kubebuilder:validation:XValidation:rule="self.targetRef.kind != 'Gateway' || !has(self.rules.response) || !has(self.rules.response.success) || !has(self.rules.response.success.headers) || !self.rules.response.success.headers.exists(x, has(self.rules.response.success.headers[x].routeSelectors))",message="route selectors not supported when targeting a Gateway"
// +kubebuilder:validation:XValidation:rule="self.targetRef.kind != 'Gateway' || !has(self.rules.response) || !has(self.rules.response.success) || !has(self.rules.response.success.dynamicMetadata) || !self.rules.response.success.dynamicMetadata.exists(x, has(self.rules.response.success.dynamicMetadata[x].routeSelectors))",message="route selectors not supported when targeting a Gateway"
// +kubebuilder:validation:XValidation:rule="self.targetRef.kind != 'Gateway' || !has(self.rules.callbacks) || !self.rules.callbacks.exists(x, has(self.rules.callbacks[x].routeSelectors))",message="route selectors not supported when targeting a Gateway"
type AuthPolicySpec struct {
	// TargetRef identifies an API object to apply policy to.
	// +kubebuilder:validation:XValidation:rule="self.group == 'gateway.networking.k8s.io'",message="Invalid targetRef.group. The only supported value is 'gateway.networking.k8s.io'"
	// +kubebuilder:validation:XValidation:rule="self.kind == 'HTTPRoute' || self.kind == 'Gateway'",message="Invalid targetRef.kind. The only supported values are 'HTTPRoute' and 'Gateway'"
	TargetRef gatewayapiv1alpha2.PolicyTargetReference `json:"targetRef"`

	// Top-level route selectors.
	// If present, the elements will be used to select HTTPRoute rules that, when activated, trigger the external authorization service.
	// At least one selected HTTPRoute rule must match to trigger the AuthPolicy.
	// If no route selectors are specified, the AuthPolicy will be enforced at all requests to the protected routes.
	// +optional
	// +kubebuilder:validation:MaxItems=15
	RouteSelectors []RouteSelector `json:"routeSelectors,omitempty"`

	// Named sets of patterns that can be referred in `when` conditions and in pattern-matching authorization policy rules.
	// +optional
	NamedPatterns map[string]authorinoapi.PatternExpressions `json:"patterns,omitempty"`

	// Overall conditions for the AuthPolicy to be enforced.
	// If omitted, the AuthPolicy will be enforced at all requests to the protected routes.
	// If present, all conditions must match for the AuthPolicy to be enforced; otherwise, the authorization service skips the AuthPolicy and returns to the auth request with status OK.
	// +optional
	Conditions []authorinoapi.PatternExpressionOrRef `json:"when,omitempty"`

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

var _ common.KuadrantPolicy = &AuthPolicy{}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:metadata:labels="gateway.networking.k8s.io/policy=direct"
// +kubebuilder:printcolumn:name="Accepted",type=string,JSONPath=`.status.conditions[?(@.type=="Accepted")].status`,description="AuthPolicy Accepted",priority=2
// +kubebuilder:printcolumn:name="Enforced",type=string,JSONPath=`.status.conditions[?(@.type=="Enforced")].status`,description="AuthPolicy Enforced",priority=2
// +kubebuilder:printcolumn:name="TargetRefKind",type="string",JSONPath=".spec.targetRef.kind",description="Type of the referenced Gateway API resource",priority=2
// +kubebuilder:printcolumn:name="TargetRefName",type="string",JSONPath=".spec.targetRef.name",description="Name of the referenced Gateway API resource",priority=2
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// AuthPolicy enables authentication and authorization for service workloads in a Gateway API network
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

func (ap *AuthPolicy) Validate() error {
	if ap.Spec.TargetRef.Namespace != nil && string(*ap.Spec.TargetRef.Namespace) != ap.Namespace {
		return fmt.Errorf("invalid targetRef.Namespace %s. Currently only supporting references to the same namespace", *ap.Spec.TargetRef.Namespace)
	}

	return nil
}

func (ap *AuthPolicy) GetTargetRef() gatewayapiv1alpha2.PolicyTargetReference {
	return ap.Spec.TargetRef
}

func (ap *AuthPolicy) GetWrappedNamespace() gatewayapiv1.Namespace {
	return gatewayapiv1.Namespace(ap.Namespace)
}

// GetRulesHostnames returns all hostnames referenced in the route selectors of the policy.
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

func (ap *AuthPolicy) Kind() string {
	return ap.TypeMeta.Kind
}

//+kubebuilder:object:root=true

// AuthPolicyList contains a list of AuthPolicy
type AuthPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AuthPolicy `json:"items"`
}

func (l *AuthPolicyList) GetItems() []common.KuadrantPolicy {
	return utils.Map(l.Items, func(item AuthPolicy) common.KuadrantPolicy {
		return &item
	})
}

func init() {
	SchemeBuilder.Register(&AuthPolicy{}, &AuthPolicyList{})
}
