package v1beta3

import (
	"context"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	authorinoapi "github.com/kuadrant/authorino/api/v1beta2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
)

var (
	AuthPolicyGVK schema.GroupVersionKind = schema.GroupVersionKind{
		Group:   GroupVersion.Group,
		Version: GroupVersion.Version,
		Kind:    "AuthPolicy",
	}
)

const (
	AuthPolicyBackReferenceAnnotationName   = "kuadrant.io/authpolicies"
	AuthPolicyDirectReferenceAnnotationName = "kuadrant.io/authpolicy"
)

type AuthSchemeSpec struct {
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

// Mutual Exclusivity Validation
// +kubebuilder:validation:XValidation:rule="!(has(self.defaults) && (has(self.patterns) || has(self.when) || has(self.rules)))",message="Implicit and explicit defaults are mutually exclusive"
// +kubebuilder:validation:XValidation:rule="!(has(self.overrides) && (has(self.patterns) || has(self.when) || has(self.rules)))",message="Implicit defaults and explicit overrides are mutually exclusive"
// +kubebuilder:validation:XValidation:rule="!(has(self.overrides) && has(self.defaults))",message="Explicit overrides and explicit defaults are mutually exclusive"
// +kubebuilder:validation:XValidation:rule="!(has(self.overrides) && self.targetRef.kind == 'HTTPRoute')",message="Overrides are not allowed for policies targeting a HTTPRoute resource"
type AuthPolicySpec struct {
	// TargetRef identifies an API object to apply policy to.
	// +kubebuilder:validation:XValidation:rule="self.group == 'gateway.networking.k8s.io'",message="Invalid targetRef.group. The only supported value is 'gateway.networking.k8s.io'"
	// +kubebuilder:validation:XValidation:rule="self.kind == 'HTTPRoute' || self.kind == 'Gateway'",message="Invalid targetRef.kind. The only supported values are 'HTTPRoute' and 'Gateway'"
	TargetRef gatewayapiv1alpha2.LocalPolicyTargetReference `json:"targetRef"`

	// Defaults define explicit default values for this policy and for policies inheriting this policy.
	// Defaults are mutually exclusive with implicit defaults defined by AuthPolicyCommonSpec.
	// +optional
	Defaults *AuthPolicyCommonSpec `json:"defaults,omitempty"`

	// Overrides define explicit override values for this policy.
	// Overrides are mutually exclusive with explicit and implicit defaults defined by AuthPolicyCommonSpec.
	// +optional
	Overrides *AuthPolicyCommonSpec `json:"overrides,omitempty"`

	// AuthPolicyCommonSpec defines implicit default values for this policy and for policies inheriting this policy.
	// AuthPolicyCommonSpec is mutually exclusive with explicit defaults defined by Defaults.
	AuthPolicyCommonSpec `json:""`
}

// AuthPolicyCommonSpec contains common shared fields for defaults and overrides
type AuthPolicyCommonSpec struct {
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
	AuthScheme *AuthSchemeSpec `json:"rules,omitempty"`
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
	currentMarshaledJSON, _ := kuadrant.ConditionMarshal(s.Conditions)
	otherMarshaledJSON, _ := kuadrant.ConditionMarshal(other.Conditions)
	if string(currentMarshaledJSON) != string(otherMarshaledJSON) {
		diff := cmp.Diff(string(currentMarshaledJSON), string(otherMarshaledJSON))
		logger.V(1).Info("Conditions not equal", "difference", diff)
		return false
	}

	return true
}

func (s *AuthPolicyStatus) GetConditions() []metav1.Condition {
	return s.Conditions
}

var _ kuadrant.Policy = &AuthPolicy{}
var _ kuadrant.Referrer = &AuthPolicy{}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:metadata:labels="gateway.networking.k8s.io/policy=inherited"
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

func (ap *AuthPolicy) IsAtomicOverride() bool {
	return ap.Spec.Overrides != nil
}

func (ap *AuthPolicy) GetTargetRef() gatewayapiv1alpha2.LocalPolicyTargetReference {
	return ap.Spec.TargetRef
}

func (ap *AuthPolicy) GetStatus() kuadrantgatewayapi.PolicyStatus {
	return &ap.Status
}

func (ap *AuthPolicy) GetWrappedNamespace() gatewayapiv1.Namespace {
	return gatewayapiv1.Namespace(ap.Namespace)
}

// GetRulesHostnames
// in v1beta2 this returned the list of route selectors
// in v1beta3 this should work with section name, once implemented.
func (ap *AuthPolicy) GetRulesHostnames() []string {
	return make([]string, 0)
}

func (ap *AuthPolicy) Kind() string {
	return NewAuthPolicyType().GetGVK().Kind
}

func (ap *AuthPolicy) TargetProgrammedGatewaysOnly() bool {
	return true
}

func (ap *AuthPolicy) PolicyClass() kuadrantgatewayapi.PolicyClass {
	return kuadrantgatewayapi.InheritedPolicy
}

func (ap *AuthPolicy) BackReferenceAnnotationName() string {
	return NewAuthPolicyType().BackReferenceAnnotationName()
}

func (ap *AuthPolicy) DirectReferenceAnnotationName() string {
	return NewAuthPolicyType().DirectReferenceAnnotationName()
}

func (ap *AuthPolicySpec) CommonSpec() *AuthPolicyCommonSpec {
	if ap.Defaults != nil {
		return ap.Defaults
	}

	if ap.Overrides != nil {
		return ap.Overrides
	}

	return &ap.AuthPolicyCommonSpec
}

//+kubebuilder:object:root=true

// AuthPolicyList contains a list of AuthPolicy
type AuthPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []AuthPolicy `json:"items"`
}

func (l *AuthPolicyList) GetItems() []kuadrant.Policy {
	return utils.Map(l.Items, func(item AuthPolicy) kuadrant.Policy {
		return &item
	})
}

type authPolicyType struct{}

func NewAuthPolicyType() kuadrantgatewayapi.PolicyType {
	return &authPolicyType{}
}

func (a authPolicyType) GetGVK() schema.GroupVersionKind {
	return AuthPolicyGVK
}
func (a authPolicyType) GetInstance() client.Object {
	return &AuthPolicy{
		TypeMeta: metav1.TypeMeta{
			Kind:       AuthPolicyGVK.Kind,
			APIVersion: GroupVersion.String(),
		},
	}
}

func (a authPolicyType) GetList(ctx context.Context, cl client.Client, listOpts ...client.ListOption) ([]kuadrantgatewayapi.Policy, error) {
	list := &AuthPolicyList{}
	err := cl.List(ctx, list, listOpts...)
	if err != nil {
		return nil, err
	}
	return utils.Map(list.Items, func(p AuthPolicy) kuadrantgatewayapi.Policy { return &p }), nil
}

func (a authPolicyType) BackReferenceAnnotationName() string {
	return AuthPolicyBackReferenceAnnotationName
}

func (a authPolicyType) DirectReferenceAnnotationName() string {
	return AuthPolicyDirectReferenceAnnotationName
}

func init() {
	SchemeBuilder.Register(&AuthPolicy{}, &AuthPolicyList{})
}
