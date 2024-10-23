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

package v1beta3

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	authorinov1beta2 "github.com/kuadrant/authorino/api/v1beta2"
	"github.com/kuadrant/policy-machinery/machinery"
	"github.com/samber/lo"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
)

const (
	// TODO: remove after fixing the integration tests that still depend on these
	AuthPolicyBackReferenceAnnotationName   = "kuadrant.io/authpolicies"
	AuthPolicyDirectReferenceAnnotationName = "kuadrant.io/authpolicy"
)

var (
	AuthPolicyGroupKind  = schema.GroupKind{Group: SchemeGroupVersion.Group, Kind: "AuthPolicy"}
	AuthPoliciesResource = SchemeGroupVersion.WithResource("authpolicies")
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

// TODO: remove
func (p *AuthPolicy) IsAtomicOverride() bool {
	return p.Spec.Overrides != nil && p.Spec.Overrides.Strategy == kuadrantv1.AtomicMergeStrategy
}

// DEPRECATED: Use GetTargetRefs instead
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
		return kuadrantv1.DefaultsMergeStrategy(spec.Strategy)
	}
	if spec := p.Spec.Overrides; spec != nil {
		return kuadrantv1.OverridesMergeStrategy(spec.Strategy)
	}
	return kuadrantv1.AtomicDefaultsMergeStrategy
}

func (p *AuthPolicy) Merge(other machinery.Policy) machinery.Policy {
	source, ok := other.(*AuthPolicy)
	if !ok {
		return p
	}
	return source.GetMergeStrategy()(source, p)
}

var _ kuadrantv1.MergeablePolicy = &AuthPolicy{}

func (p *AuthPolicy) Empty() bool {
	return p.Spec.Proper().AuthScheme == nil
}

func (p *AuthPolicy) Rules() map[string]kuadrantv1.MergeableRule {
	rules := make(map[string]kuadrantv1.MergeableRule)
	policyLocator := p.GetLocator()
	spec := p.Spec.Proper()

	for ruleID := range spec.NamedPatterns {
		rule := spec.NamedPatterns[ruleID]
		rules[fmt.Sprintf("patterns#%s", ruleID)] = kuadrantv1.NewMergeableRule(&rule, policyLocator)
	}

	for ruleID := range spec.Conditions {
		rule := spec.Conditions[ruleID]
		rules[fmt.Sprintf("conditions#%d", ruleID)] = kuadrantv1.NewMergeableRule(&rule, policyLocator)
	}

	if spec.AuthScheme == nil {
		return rules
	}

	for ruleID := range spec.AuthScheme.Authentication {
		rule := spec.AuthScheme.Authentication[ruleID]
		rules[fmt.Sprintf("authentication#%s", ruleID)] = kuadrantv1.NewMergeableRule(&rule, policyLocator)
	}

	for ruleID := range spec.AuthScheme.Metadata {
		rule := spec.AuthScheme.Metadata[ruleID]
		rules[fmt.Sprintf("metadata#%s", ruleID)] = kuadrantv1.NewMergeableRule(&rule, policyLocator)
	}

	for ruleID := range spec.AuthScheme.Authorization {
		rule := spec.AuthScheme.Authorization[ruleID]
		rules[fmt.Sprintf("authorization#%s", ruleID)] = kuadrantv1.NewMergeableRule(&rule, policyLocator)
	}

	for ruleID := range spec.AuthScheme.Callbacks {
		rule := spec.AuthScheme.Callbacks[ruleID]
		rules[fmt.Sprintf("callbacks#%s", ruleID)] = kuadrantv1.NewMergeableRule(&rule, policyLocator)
	}

	if spec.AuthScheme.Response == nil {
		return rules
	}

	{
		rule := spec.AuthScheme.Response.Unauthenticated
		rules["response.unauthenticated#"] = kuadrantv1.NewMergeableRule(rule, policyLocator)
	}
	{
		rule := spec.AuthScheme.Response.Unauthorized
		rules["response.unauthorized#"] = kuadrantv1.NewMergeableRule(rule, policyLocator)
	}

	for ruleID := range spec.AuthScheme.Response.Success.Headers {
		rule := spec.AuthScheme.Response.Success.Headers[ruleID]
		rules[fmt.Sprintf("response.success.headers#%s", ruleID)] = kuadrantv1.NewMergeableRule(&rule, policyLocator)
	}

	for ruleID := range spec.AuthScheme.Response.Success.DynamicMetadata {
		rule := spec.AuthScheme.Response.Success.DynamicMetadata[ruleID]
		rules[fmt.Sprintf("response.success.metadata#%s", ruleID)] = kuadrantv1.NewMergeableRule(&rule, policyLocator)
	}

	return rules
}

func (p *AuthPolicy) SetRules(rules map[string]kuadrantv1.MergeableRule) {
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
			p.Spec.Proper().Conditions = append(p.Spec.Proper().Conditions, *rule.(*MergeablePatternExpressionOrRef))
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

// DEPRECATED. impl: kuadrant.Policy
func (p *AuthPolicy) GetStatus() kuadrantgatewayapi.PolicyStatus {
	return &p.Status
}

// DEPRECATED. impl: kuadrant.Policy
func (p *AuthPolicy) PolicyClass() kuadrantgatewayapi.PolicyClass {
	return kuadrantgatewayapi.InheritedPolicy
}

// DEPRECATED. impl: kuadrant.Policy
func (p *AuthPolicy) GetWrappedNamespace() gatewayapiv1.Namespace {
	return gatewayapiv1.Namespace(p.GetNamespace())
}

// DEPRECATED. impl: kuadrant.Policy
func (p *AuthPolicy) GetRulesHostnames() []string {
	return []string{}
}

// DEPRECATED. impl: kuadrant.Policy
func (p *AuthPolicy) Kind() string {
	return AuthPolicyGroupKind.Kind
}

// TODO: remove
func (p *AuthPolicy) BackReferenceAnnotationName() string {
	return AuthPolicyBackReferenceAnnotationName
}

// TODO: remove
func (p *AuthPolicy) DirectReferenceAnnotationName() string {
	return AuthPolicyDirectReferenceAnnotationName
}

// TODO: remove
func (p *AuthPolicy) TargetProgrammedGatewaysOnly() bool {
	return true
}

// +kubebuilder:validation:XValidation:rule="!(has(self.defaults) && has(self.rules))",message="Implicit and explicit defaults are mutually exclusive"
// +kubebuilder:validation:XValidation:rule="!(has(self.defaults) && has(self.overrides))",message="Overrides and explicit defaults are mutually exclusive"
// +kubebuilder:validation:XValidation:rule="!(has(self.overrides) && has(self.rules))",message="Overrides and implicit defaults are mutually exclusive"
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

// UnmarshalJSON unmarshals the AuthPolicySpec from JSON byte array.
// This should not be needed, but runtime.DefaultUnstructuredConverter.FromUnstructured does not work well with embedded structs.
func (s *AuthPolicySpec) UnmarshalJSON(j []byte) error {
	targetRef := struct {
		gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName `json:"targetRef"`
	}{}
	if err := json.Unmarshal(j, &targetRef); err != nil {
		return err
	}
	s.TargetRef = targetRef.LocalPolicyTargetReferenceWithSectionName

	defaults := &struct {
		*MergeableAuthPolicySpec `json:"defaults,omitempty"`
	}{}
	if err := json.Unmarshal(j, defaults); err != nil {
		return err
	}
	s.Defaults = defaults.MergeableAuthPolicySpec

	overrides := &struct {
		*MergeableAuthPolicySpec `json:"overrides,omitempty"`
	}{}
	if err := json.Unmarshal(j, overrides); err != nil {
		return err
	}
	s.Overrides = overrides.MergeableAuthPolicySpec

	proper := struct {
		AuthPolicySpecProper `json:""`
	}{}
	if err := json.Unmarshal(j, &proper); err != nil {
		return err
	}
	s.AuthPolicySpecProper = proper.AuthPolicySpecProper

	return nil
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
	Conditions []MergeablePatternExpressionOrRef `json:"when,omitempty"`

	// The auth rules of the policy.
	// See Authorino's AuthConfig CRD for more details.
	AuthScheme *AuthSchemeSpec `json:"rules,omitempty"`
}

type AuthSchemeSpec struct {
	// Authentication configs.
	// At least one config MUST evaluate to a valid identity object for the auth request to be successful.
	// +optional
	// +kubebuilder:validation:MaxProperties=10
	Authentication map[string]MergeableAuthenticationSpec `json:"authentication,omitempty"`

	// Metadata sources.
	// Authorino fetches auth metadata as JSON from sources specified in this config.
	// +optional
	// +kubebuilder:validation:MaxProperties=10
	Metadata map[string]MergeableMetadataSpec `json:"metadata,omitempty"`

	// Authorization policies.
	// All policies MUST evaluate to "allowed = true" for the auth request be successful.
	// +optional
	// +kubebuilder:validation:MaxProperties=10
	Authorization map[string]MergeableAuthorizationSpec `json:"authorization,omitempty"`

	// Response items.
	// Authorino builds custom responses to the client of the auth request.
	// +optional
	Response *MergeableResponseSpec `json:"response,omitempty"`

	// Callback functions.
	// Authorino sends callbacks at the end of the auth pipeline to the endpoints specified in this config.
	// +optional
	// +kubebuilder:validation:MaxProperties=10
	Callbacks map[string]MergeableCallbackSpec `json:"callbacks,omitempty"`
}

type MergeablePatternExpressions struct {
	authorinov1beta2.PatternExpressions `json:"allOf"`
	Source                              string `json:"-"`
}

func (r *MergeablePatternExpressions) GetSpec() any      { return r.PatternExpressions }
func (r *MergeablePatternExpressions) GetSource() string { return r.Source }
func (r *MergeablePatternExpressions) WithSource(source string) kuadrantv1.MergeableRule {
	r.Source = source
	return r
}

type MergeablePatternExpressionOrRef struct {
	authorinov1beta2.PatternExpressionOrRef `json:",inline"`
	Source                                  string `json:"-"`
}

func (r *MergeablePatternExpressionOrRef) GetSpec() any      { return r.PatternExpressionOrRef }
func (r *MergeablePatternExpressionOrRef) GetSource() string { return r.Source }
func (r *MergeablePatternExpressionOrRef) WithSource(source string) kuadrantv1.MergeableRule {
	r.Source = source
	return r
}
func (r *MergeablePatternExpressionOrRef) ToWhenConditions(namedPatterns map[string]MergeablePatternExpressions) []WhenCondition {
	if ref := r.PatternRef.Name; ref != "" {
		if pattern, ok := namedPatterns[ref]; ok {
			return lo.Map(pattern.PatternExpressions, func(p authorinov1beta2.PatternExpression, _ int) WhenCondition {
				return WhenCondition{
					Selector: ContextSelector(p.Selector),
					Operator: WhenConditionOperator(p.Operator),
					Value:    p.Value,
				}
			})
		}
	}

	if allOf := r.All; len(allOf) > 0 {
		return lo.Map(allOf, func(p authorinov1beta2.UnstructuredPatternExpressionOrRef, _ int) WhenCondition {
			return WhenCondition{
				Selector: ContextSelector(p.Selector),
				Operator: WhenConditionOperator(p.Operator),
				Value:    p.Value,
			}
		})
	}

	// FIXME: anyOf cannot be represented in the current schema of the wasm config

	return []WhenCondition{
		{
			Selector: ContextSelector(r.Selector),
			Operator: WhenConditionOperator(r.Operator),
			Value:    r.Value,
		},
	}
}

type MergeableAuthenticationSpec struct {
	authorinov1beta2.AuthenticationSpec `json:",inline"`
	Source                              string `json:"-"`
}

func (r *MergeableAuthenticationSpec) GetSpec() any      { return r.AuthenticationSpec }
func (r *MergeableAuthenticationSpec) GetSource() string { return r.Source }
func (r *MergeableAuthenticationSpec) WithSource(source string) kuadrantv1.MergeableRule {
	r.Source = source
	return r
}

type MergeableMetadataSpec struct {
	authorinov1beta2.MetadataSpec `json:",inline"`
	Source                        string `json:"-"`
}

func (r *MergeableMetadataSpec) GetSpec() any      { return r.MetadataSpec }
func (r *MergeableMetadataSpec) GetSource() string { return r.Source }
func (r *MergeableMetadataSpec) WithSource(source string) kuadrantv1.MergeableRule {
	r.Source = source
	return r
}

type MergeableAuthorizationSpec struct {
	authorinov1beta2.AuthorizationSpec `json:",inline"`
	Source                             string `json:"-"`
}

func (r *MergeableAuthorizationSpec) GetSpec() any      { return r.AuthorizationSpec }
func (r *MergeableAuthorizationSpec) GetSource() string { return r.Source }
func (r *MergeableAuthorizationSpec) WithSource(source string) kuadrantv1.MergeableRule {
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
	authorinov1beta2.DenyWithSpec `json:",inline"`
	Source                        string `json:"-"`
}

func (r *MergeableDenyWithSpec) GetSpec() any      { return r.DenyWithSpec }
func (r *MergeableDenyWithSpec) GetSource() string { return r.Source }
func (r *MergeableDenyWithSpec) WithSource(source string) kuadrantv1.MergeableRule {
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
	authorinov1beta2.HeaderSuccessResponseSpec `json:",inline"`
	Source                                     string `json:"-"`
}

func (r *MergeableHeaderSuccessResponseSpec) GetSpec() any      { return r.HeaderSuccessResponseSpec }
func (r *MergeableHeaderSuccessResponseSpec) GetSource() string { return r.Source }
func (r *MergeableHeaderSuccessResponseSpec) WithSource(source string) kuadrantv1.MergeableRule {
	r.Source = source
	return r
}

type MergeableSuccessResponseSpec struct {
	authorinov1beta2.SuccessResponseSpec `json:",inline"`
	Source                               string `json:"-"`
}

func (r *MergeableSuccessResponseSpec) GetSpec() any      { return r.SuccessResponseSpec }
func (r *MergeableSuccessResponseSpec) GetSource() string { return r.Source }
func (r *MergeableSuccessResponseSpec) WithSource(source string) kuadrantv1.MergeableRule {
	r.Source = source
	return r
}

type MergeableCallbackSpec struct {
	authorinov1beta2.CallbackSpec `json:",inline"`
	Source                        string `json:"-"`
}

func (r *MergeableCallbackSpec) GetSpec() any      { return r.CallbackSpec }
func (r *MergeableCallbackSpec) GetSource() string { return r.Source }
func (r *MergeableCallbackSpec) WithSource(source string) kuadrantv1.MergeableRule {
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

// DEPRECATED. impl: kuadrant.PolicyList
func (l *AuthPolicyList) GetItems() []kuadrant.Policy {
	return utils.Map(l.Items, func(item AuthPolicy) kuadrant.Policy {
		return &item
	})
}

func init() {
	SchemeBuilder.Register(&AuthPolicy{}, &AuthPolicyList{})
}
