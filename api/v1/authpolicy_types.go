/*
Copyright 2024.

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

package v1

import (
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	authorinov1beta3 "github.com/kuadrant/authorino/api/v1beta3"
	"github.com/kuadrant/policy-machinery/machinery"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/pkg/kuadrant"
)

var (
	AuthPolicyGroupKind  = schema.GroupKind{Group: GroupVersion.Group, Kind: "AuthPolicy"}
	AuthPoliciesResource = GroupVersion.WithResource("authpolicies")
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:metadata:labels="gateway.networking.k8s.io/policy=inherited"
// +kubebuilder:printcolumn:name="Accepted",type=string,JSONPath=`.status.conditions[?(@.type=="Accepted")].status`,description="AuthPolicy Accepted",priority=2
// +kubebuilder:printcolumn:name="Enforced",type=string,JSONPath=`.status.conditions[?(@.type=="Enforced")].status`,description="AuthPolicy Enforced",priority=2
// +kubebuilder:printcolumn:name="TargetKind",type="string",JSONPath=".spec.targetRef.kind",description="Kind of the object to which the policy aaplies",priority=2
// +kubebuilder:printcolumn:name="TargetName",type="string",JSONPath=".spec.targetRef.name",description="Name of the object to which the policy applies",priority=2
// +kubebuilder:printcolumn:name="TargetSection",type="string",JSONPath=".spec.targetRef.sectionName",description="Name of the section within the object to which the policy applies ",priority=2
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// AuthPolicy enables authentication and authorization for service workloads in a Gateway API network
type AuthPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AuthPolicySpec   `json:"spec,omitempty"`
	Status AuthPolicyStatus `json:"status,omitempty"`
}

var _ machinery.Policy = &AuthPolicy{}

func (p *AuthPolicy) GetNamespace() string {
	return p.Namespace
}

func (p *AuthPolicy) GetName() string {
	return p.Name
}

func (p *AuthPolicy) GetLocator() string {
	return machinery.LocatorFromObject(p)
}

// Deprecated: Use GetTargetRefs instead
func (p *AuthPolicy) GetTargetRef() gatewayapiv1alpha2.LocalPolicyTargetReference {
	return p.Spec.TargetRef.LocalPolicyTargetReference
}

func (p *AuthPolicy) GetTargetRefs() []machinery.PolicyTargetReference {
	return []machinery.PolicyTargetReference{
		machinery.LocalPolicyTargetReferenceWithSectionName{
			LocalPolicyTargetReferenceWithSectionName: p.Spec.TargetRef,
			PolicyNamespace: p.Namespace,
		},
	}
}

func (p *AuthPolicy) GetMergeStrategy() machinery.MergeStrategy {
	if spec := p.Spec.Defaults; spec != nil {
		return DefaultsMergeStrategy(spec.Strategy)
	}
	if spec := p.Spec.Overrides; spec != nil {
		return OverridesMergeStrategy(spec.Strategy)
	}
	return AtomicDefaultsMergeStrategy
}

func (p *AuthPolicy) Merge(other machinery.Policy) machinery.Policy {
	source, ok := other.(*AuthPolicy)
	if !ok {
		return p
	}
	return source.GetMergeStrategy()(source, p)
}

var _ MergeablePolicy = &AuthPolicy{}

func (p *AuthPolicy) Empty() bool {
	return p.Spec.Proper().AuthScheme == nil
}

func (p *AuthPolicy) Rules() map[string]MergeableRule {
	rules := make(map[string]MergeableRule)
	policyLocator := p.GetLocator()
	spec := p.Spec.Proper()

	for ruleID := range spec.NamedPatterns {
		rule := spec.NamedPatterns[ruleID]
		rules[fmt.Sprintf("patterns#%s", ruleID)] = NewMergeableRule(&rule, policyLocator)
	}

	if whenPredicates := spec.MergeableWhenPredicates; len(whenPredicates.Predicates) > 0 {
		rules["conditions#"] = NewMergeableRule(&whenPredicates, policyLocator)
	}

	if spec.AuthScheme == nil {
		return rules
	}

	for ruleID := range spec.AuthScheme.Authentication {
		rule := spec.AuthScheme.Authentication[ruleID]
		rules[fmt.Sprintf("authentication#%s", ruleID)] = NewMergeableRule(&rule, policyLocator)
	}

	for ruleID := range spec.AuthScheme.Metadata {
		rule := spec.AuthScheme.Metadata[ruleID]
		rules[fmt.Sprintf("metadata#%s", ruleID)] = NewMergeableRule(&rule, policyLocator)
	}

	for ruleID := range spec.AuthScheme.Authorization {
		rule := spec.AuthScheme.Authorization[ruleID]
		rules[fmt.Sprintf("authorization#%s", ruleID)] = NewMergeableRule(&rule, policyLocator)
	}

	for ruleID := range spec.AuthScheme.Callbacks {
		rule := spec.AuthScheme.Callbacks[ruleID]
		rules[fmt.Sprintf("callbacks#%s", ruleID)] = NewMergeableRule(&rule, policyLocator)
	}

	if spec.AuthScheme.Response == nil {
		return rules
	}

	if rule := spec.AuthScheme.Response.Unauthenticated; rule != nil {
		rules["response.unauthenticated#"] = NewMergeableRule(rule, policyLocator)
	}
	if rule := spec.AuthScheme.Response.Unauthorized; rule != nil {
		rules["response.unauthorized#"] = NewMergeableRule(rule, policyLocator)
	}

	for ruleID := range spec.AuthScheme.Response.Success.Headers {
		rule := spec.AuthScheme.Response.Success.Headers[ruleID]
		rules[fmt.Sprintf("response.success.headers#%s", ruleID)] = NewMergeableRule(&rule, policyLocator)
	}

	for ruleID := range spec.AuthScheme.Response.Success.DynamicMetadata {
		rule := spec.AuthScheme.Response.Success.DynamicMetadata[ruleID]
		rules[fmt.Sprintf("response.success.metadata#%s", ruleID)] = NewMergeableRule(&rule, policyLocator)
	}

	return rules
}

func (p *AuthPolicy) SetRules(rules map[string]MergeableRule) {
	// clear all rules of the policy before setting new ones
	p.Spec.Proper().NamedPatterns = nil
	p.Spec.Proper().Predicates = nil
	p.Spec.Proper().AuthScheme = nil

	ensureNamedPatterns := func() {
		if p.Spec.Proper().NamedPatterns == nil {
			p.Spec.Proper().NamedPatterns = make(map[string]MergeablePatternExpressions)
		}
	}

	ensureAuthScheme := func() {
		if p.Spec.Proper().AuthScheme == nil {
			p.Spec.Proper().AuthScheme = &AuthSchemeSpec{}
		}
	}

	ensureAuthentication := func() {
		ensureAuthScheme()
		if p.Spec.Proper().AuthScheme.Authentication == nil {
			p.Spec.Proper().AuthScheme.Authentication = make(map[string]MergeableAuthenticationSpec)
		}
	}

	ensureMetadata := func() {
		ensureAuthScheme()
		if p.Spec.Proper().AuthScheme.Metadata == nil {
			p.Spec.Proper().AuthScheme.Metadata = make(map[string]MergeableMetadataSpec)
		}
	}

	ensureAuthorization := func() {
		ensureAuthScheme()
		if p.Spec.Proper().AuthScheme.Authorization == nil {
			p.Spec.Proper().AuthScheme.Authorization = make(map[string]MergeableAuthorizationSpec)
		}
	}

	ensureResponse := func() {
		ensureAuthScheme()
		if p.Spec.Proper().AuthScheme.Response == nil {
			p.Spec.Proper().AuthScheme.Response = &MergeableResponseSpec{}
		}
	}

	ensureResponseSuccessHeaders := func() {
		ensureResponse()
		if p.Spec.Proper().AuthScheme.Response.Success.Headers == nil {
			p.Spec.Proper().AuthScheme.Response.Success.Headers = make(map[string]MergeableHeaderSuccessResponseSpec)
		}
	}

	ensureResponseSuccessDynamicMetadata := func() {
		ensureResponse()
		if p.Spec.Proper().AuthScheme.Response.Success.DynamicMetadata == nil {
			p.Spec.Proper().AuthScheme.Response.Success.DynamicMetadata = make(map[string]MergeableSuccessResponseSpec)
		}
	}

	ensureCallbacks := func() {
		ensureAuthScheme()
		if p.Spec.Proper().AuthScheme.Callbacks == nil {
			p.Spec.Proper().AuthScheme.Callbacks = make(map[string]MergeableCallbackSpec)
		}
	}

	for id := range rules {
		rule := rules[id]
		parts := strings.SplitN(id, "#", 2)
		group := parts[0]
		ruleID := parts[len(parts)-1]

		if strings.HasPrefix(group, "response.") {
			ensureResponse()
		}

		switch group {
		case "patterns":
			ensureNamedPatterns()
			p.Spec.Proper().NamedPatterns[ruleID] = *rule.(*MergeablePatternExpressions)
		case "conditions":
			p.Spec.Proper().MergeableWhenPredicates = *rule.(*MergeableWhenPredicates)
		case "authentication":
			ensureAuthentication()
			p.Spec.Proper().AuthScheme.Authentication[ruleID] = *rule.(*MergeableAuthenticationSpec)
		case "metadata":
			ensureMetadata()
			p.Spec.Proper().AuthScheme.Metadata[ruleID] = *rule.(*MergeableMetadataSpec)
		case "authorization":
			ensureAuthorization()
			p.Spec.Proper().AuthScheme.Authorization[ruleID] = *rule.(*MergeableAuthorizationSpec)
		case "response.unauthenticated":
			ensureResponse()
			p.Spec.Proper().AuthScheme.Response.Unauthenticated = rule.(*MergeableDenyWithSpec)
		case "response.unauthorized":
			ensureResponse()
			p.Spec.Proper().AuthScheme.Response.Unauthorized = rule.(*MergeableDenyWithSpec)
		case "response.success.headers":
			ensureResponseSuccessHeaders()
			p.Spec.Proper().AuthScheme.Response.Success.Headers[ruleID] = *rule.(*MergeableHeaderSuccessResponseSpec)
		case "response.success.metadata":
			ensureResponseSuccessDynamicMetadata()
			p.Spec.Proper().AuthScheme.Response.Success.DynamicMetadata[ruleID] = *rule.(*MergeableSuccessResponseSpec)
		case "callbacks":
			ensureCallbacks()
			p.Spec.Proper().AuthScheme.Callbacks[ruleID] = *rule.(*MergeableCallbackSpec)
		}
	}
}

func (p *AuthPolicy) GetStatus() kuadrantgatewayapi.PolicyStatus {
	return &p.Status
}

func (p *AuthPolicy) Kind() string {
	return AuthPolicyGroupKind.Kind
}

// +kubebuilder:validation:XValidation:rule="!(has(self.defaults) && (has(self.patterns) || has(self.when) || has(self.rules)))",message="Implicit and explicit defaults are mutually exclusive"
// +kubebuilder:validation:XValidation:rule="!(has(self.overrides) && (has(self.patterns) || has(self.when) || has(self.rules)))",message="Implicit defaults and explicit overrides are mutually exclusive"
// +kubebuilder:validation:XValidation:rule="!(has(self.overrides) && has(self.defaults))",message="Explicit overrides and explicit defaults are mutually exclusive"
// +kubebuilder:validation:XValidation:rule="!(has(self.overrides) || has(self.defaults)) ? has(self.rules) && ((has(self.rules.authentication) && size(self.rules.authentication) > 0) || (has(self.rules.metadata) && size(self.rules.metadata) > 0) || (has(self.rules.authorization) && size(self.rules.authorization) > 0) || (has(self.rules.response) && (has(self.rules.response.unauthenticated) || has(self.rules.response.unauthorized) || (has(self.rules.response.success) && (size(self.rules.response.success.headers) > 0 ||  size(self.rules.response.success.filters) > 0)))) || (has(self.rules.callbacks) && size(self.rules.callbacks) > 0)) : true",message="At least one spec.rules must be defined"
// +kubebuilder:validation:XValidation:rule="has(self.defaults) ? has(self.defaults.rules) && ((has(self.defaults.rules.authentication) && size(self.defaults.rules.authentication) > 0) || (has(self.defaults.rules.metadata) && size(self.defaults.rules.metadata) > 0) || (has(self.defaults.rules.authorization) && size(self.defaults.rules.authorization) > 0) || (has(self.defaults.rules.response) && (has(self.defaults.rules.response.unauthenticated) || has(self.defaults.rules.response.unauthorized) || (has(self.defaults.rules.response.success) && (size(self.defaults.rules.response.success.headers) > 0 ||  size(self.defaults.rules.response.success.filters) > 0)))) || (has(self.defaults.rules.callbacks) && size(self.defaults.rules.callbacks) > 0)) : true",message="At least one spec.defaults.rules must be defined"
// +kubebuilder:validation:XValidation:rule="has(self.overrides) ? has(self.overrides.rules) && ((has(self.overrides.rules.authentication) && size(self.overrides.rules.authentication) > 0) || (has(self.overrides.rules.metadata) && size(self.overrides.rules.metadata) > 0) || (has(self.overrides.rules.authorization) && size(self.overrides.rules.authorization) > 0) || (has(self.overrides.rules.response) && (has(self.overrides.rules.response.unauthenticated) || has(self.overrides.rules.response.unauthorized) || (has(self.overrides.rules.response.success) && (size(self.overrides.rules.response.success.headers) > 0 ||  size(self.overrides.rules.response.success.filters) > 0)))) || (has(self.overrides.rules.callbacks) && size(self.overrides.rules.callbacks) > 0)) : true",message="At least one spec.overrides.rules must be defined"
type AuthPolicySpec struct {
	// Reference to the object to which this policy applies.
	// +kubebuilder:validation:XValidation:rule="self.group == 'gateway.networking.k8s.io'",message="Invalid targetRef.group. The only supported value is 'gateway.networking.k8s.io'"
	// +kubebuilder:validation:XValidation:rule="self.kind == 'HTTPRoute' || self.kind == 'Gateway'",message="Invalid targetRef.kind. The only supported values are 'HTTPRoute' and 'Gateway'"
	TargetRef gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName `json:"targetRef"`

	// Rules to apply as defaults. Can be overridden by more specific policiy rules lower in the hierarchy and by less specific policy overrides.
	// Use one of: defaults, overrides, or bare set of policy rules (implicit defaults).
	// +optional
	Defaults *MergeableAuthPolicySpec `json:"defaults,omitempty"`

	// Rules to apply as overrides. Override all policy rules lower in the hierarchy. Can be overridden by less specific policy overrides.
	// Use one of: defaults, overrides, or bare set of policy rules (implicit defaults).
	// +optional
	Overrides *MergeableAuthPolicySpec `json:"overrides,omitempty"`

	// Bare set of policy rules (implicit defaults).
	// Use one of: defaults, overrides, or bare set of policy rules (implicit defaults).
	AuthPolicySpecProper `json:""`
}

func (s *AuthPolicySpec) Proper() *AuthPolicySpecProper {
	if s.Defaults != nil {
		return &s.Defaults.AuthPolicySpecProper
	}

	if s.Overrides != nil {
		return &s.Overrides.AuthPolicySpecProper
	}

	return &s.AuthPolicySpecProper
}

type MergeableAuthPolicySpec struct {
	// Strategy defines the merge strategy to apply when merging this policy with other policies.
	// +kubebuilder:validation:Enum=atomic;merge
	// +kubebuilder:default=atomic
	Strategy string `json:"strategy,omitempty"`

	AuthPolicySpecProper `json:""`
}

// AuthPolicySpecProper contains common shared fields for defaults and overrides
type AuthPolicySpecProper struct {
	// Named sets of patterns that can be referred in `when` conditions and in pattern-matching authorization policy rules.
	// +optional
	NamedPatterns map[string]MergeablePatternExpressions `json:"patterns,omitempty"`

	// Overall conditions for the AuthPolicy to be enforced.
	// If omitted, the AuthPolicy will be enforced at all requests to the protected routes.
	// If present, all conditions must match for the AuthPolicy to be enforced; otherwise, the authorization service skips the AuthPolicy and returns to the auth request with status OK.
	// +optional
	MergeableWhenPredicates `json:""`

	// The auth rules of the policy.
	// See Authorino's AuthConfig CRD for more details.
	AuthScheme *AuthSchemeSpec `json:"rules,omitempty"`
}

type AuthSchemeSpec struct {
	// Authentication configs.
	// At least one config MUST evaluate to a valid identity object for the auth request to be successful.
	// +optional
	Authentication map[string]MergeableAuthenticationSpec `json:"authentication,omitempty"`

	// Metadata sources.
	// Authorino fetches auth metadata as JSON from sources specified in this config.
	// +optional
	Metadata map[string]MergeableMetadataSpec `json:"metadata,omitempty"`

	// Authorization policies.
	// All policies MUST evaluate to "allowed = true" for the auth request be successful.
	// +optional
	Authorization map[string]MergeableAuthorizationSpec `json:"authorization,omitempty"`

	// Response items.
	// Authorino builds custom responses to the client of the auth request.
	// +optional
	Response *MergeableResponseSpec `json:"response,omitempty"`

	// Callback functions.
	// Authorino sends callbacks at the end of the auth pipeline to the endpoints specified in this config.
	// +optional
	Callbacks map[string]MergeableCallbackSpec `json:"callbacks,omitempty"`
}

type MergeablePatternExpressions struct {
	authorinov1beta3.PatternExpressions `json:"allOf"`
	Source                              string `json:"-"`
}

func (r *MergeablePatternExpressions) GetSpec() any      { return r.PatternExpressions }
func (r *MergeablePatternExpressions) GetSource() string { return r.Source }
func (r *MergeablePatternExpressions) WithSource(source string) MergeableRule {
	r.Source = source
	return r
}

type MergeableAuthenticationSpec struct {
	authorinov1beta3.AuthenticationSpec `json:",inline"`
	Source                              string `json:"-"`
}

func (r *MergeableAuthenticationSpec) GetSpec() any      { return r.AuthenticationSpec }
func (r *MergeableAuthenticationSpec) GetSource() string { return r.Source }
func (r *MergeableAuthenticationSpec) WithSource(source string) MergeableRule {
	r.Source = source
	return r
}

type MergeableMetadataSpec struct {
	authorinov1beta3.MetadataSpec `json:",inline"`
	Source                        string `json:"-"`
}

func (r *MergeableMetadataSpec) GetSpec() any      { return r.MetadataSpec }
func (r *MergeableMetadataSpec) GetSource() string { return r.Source }
func (r *MergeableMetadataSpec) WithSource(source string) MergeableRule {
	r.Source = source
	return r
}

type MergeableAuthorizationSpec struct {
	authorinov1beta3.AuthorizationSpec `json:",inline"`
	Source                             string `json:"-"`
}

func (r *MergeableAuthorizationSpec) GetSpec() any      { return r.AuthorizationSpec }
func (r *MergeableAuthorizationSpec) GetSource() string { return r.Source }
func (r *MergeableAuthorizationSpec) WithSource(source string) MergeableRule {
	r.Source = source
	return r
}

// Settings of the custom auth response.
type MergeableResponseSpec struct {
	// Customizations on the denial status attributes when the request is unauthenticated.
	// For integration of Authorino via proxy, the proxy must honour the response status attributes specified in this config.
	// Default: 401 Unauthorized
	// +optional
	Unauthenticated *MergeableDenyWithSpec `json:"unauthenticated,omitempty"`

	// Customizations on the denial status attributes when the request is unauthorized.
	// For integration of Authorino via proxy, the proxy must honour the response status attributes specified in this config.
	// Default: 403 Forbidden
	// +optional
	Unauthorized *MergeableDenyWithSpec `json:"unauthorized,omitempty"`

	// Response items to be included in the auth response when the request is authenticated and authorized.
	// For integration of Authorino via proxy, the proxy must use these settings to propagate dynamic metadata and/or inject data in the request.
	// +optional
	Success MergeableWrappedSuccessResponseSpec `json:"success,omitempty"`
}

type MergeableDenyWithSpec struct {
	authorinov1beta3.DenyWithSpec `json:",inline"`
	Source                        string `json:"-"`
}

func (r *MergeableDenyWithSpec) GetSpec() any      { return r.DenyWithSpec }
func (r *MergeableDenyWithSpec) GetSource() string { return r.Source }
func (r *MergeableDenyWithSpec) WithSource(source string) MergeableRule {
	r.Source = source
	return r
}

type MergeableWrappedSuccessResponseSpec struct {
	// Custom headers to inject in the request.
	Headers map[string]MergeableHeaderSuccessResponseSpec `json:"headers,omitempty"`

	// Custom data made available to other filters managed by Kuadrant (i.e. Rate Limit)
	DynamicMetadata map[string]MergeableSuccessResponseSpec `json:"filters,omitempty"`
}

type MergeableHeaderSuccessResponseSpec struct {
	authorinov1beta3.HeaderSuccessResponseSpec `json:",inline"`
	Source                                     string `json:"-"`
}

func (r *MergeableHeaderSuccessResponseSpec) GetSpec() any      { return r.HeaderSuccessResponseSpec }
func (r *MergeableHeaderSuccessResponseSpec) GetSource() string { return r.Source }
func (r *MergeableHeaderSuccessResponseSpec) WithSource(source string) MergeableRule {
	r.Source = source
	return r
}

type MergeableSuccessResponseSpec struct {
	authorinov1beta3.SuccessResponseSpec `json:",inline"`
	Source                               string `json:"-"`
}

func (r *MergeableSuccessResponseSpec) GetSpec() any      { return r.SuccessResponseSpec }
func (r *MergeableSuccessResponseSpec) GetSource() string { return r.Source }
func (r *MergeableSuccessResponseSpec) WithSource(source string) MergeableRule {
	r.Source = source
	return r
}

type MergeableCallbackSpec struct {
	authorinov1beta3.CallbackSpec `json:",inline"`
	Source                        string `json:"-"`
}

func (r *MergeableCallbackSpec) GetSpec() any      { return r.CallbackSpec }
func (r *MergeableCallbackSpec) GetSource() string { return r.Source }
func (r *MergeableCallbackSpec) WithSource(source string) MergeableRule {
	r.Source = source
	return r
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
