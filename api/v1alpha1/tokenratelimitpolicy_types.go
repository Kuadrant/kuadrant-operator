/*
Copyright 2025.

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

package v1alpha1

import (
	"github.com/kuadrant/policy-machinery/machinery"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/intstr"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	transformer "github.com/kuadrant/kuadrant-operator/internal/cel"
	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/internal/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/internal/utils"
)

var (
	TokenRateLimitPolicyGroupKind  = schema.GroupKind{Group: GroupVersion.Group, Kind: "TokenRateLimitPolicy"}
	TokenRateLimitPoliciesResource = GroupVersion.WithResource("tokenratelimitpolicies")

	// RulesKeyDataExtraction is the key used to store the dataExtraction rule within the Rules() map,
	// following the same "not a limit name" trick as kuadrantv1.RulesKeyTopLevelPredicates.
	RulesKeyDataExtraction = "###_DATA_EXTRACTION_###"

	// DefaultTotalTokensPointers is the built-in, ordered list of JSON Pointer (RFC 6901) candidates used
	// to extract total token usage from the response body when dataExtraction.response.totalTokens is
	// unset. Order matters: the first candidate that resolves wins.
	DefaultTotalTokensPointers = []string{
		"/usage/total_tokens",            // OpenAI, Azure OpenAI, OpenAI-compatible servers, OpenAI Responses API (non-streaming)
		"/usageMetadata/totalTokenCount", // Google Gemini
		"/response/usage/total_tokens",   // OpenAI Responses API (streaming)
		"/usage/totalTokens",             // AWS Bedrock Converse API (non-streaming)
	}
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:metadata:labels="gateway.networking.k8s.io/policy=inherited"
// +kubebuilder:printcolumn:name="Accepted",type=string,JSONPath=`.status.conditions[?(@.type=="Accepted")].status`,description="TokenRateLimitPolicy Accepted",priority=2
// +kubebuilder:printcolumn:name="Enforced",type=string,JSONPath=`.status.conditions[?(@.type=="Enforced")].status`,description="TokenRateLimitPolicy Enforced",priority=2
// +kubebuilder:printcolumn:name="TargetKind",type="string",JSONPath=".spec.targetRef.kind",description="Kind of the object to which the policy applies",priority=2
// +kubebuilder:printcolumn:name="TargetName",type="string",JSONPath=".spec.targetRef.name",description="Name of the object to which the policy applies",priority=2
// +kubebuilder:printcolumn:name="TargetSection",type="string",JSONPath=".spec.targetRef.sectionName",description="Name of the section within the object to which the policy applies ",priority=2
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// TokenRateLimitPolicy enables token-based rate limiting for service workloads in a Gateway API network
type TokenRateLimitPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TokenRateLimitPolicySpec   `json:"spec,omitempty"`
	Status TokenRateLimitPolicyStatus `json:"status,omitempty"`
}

var _ machinery.Policy = &TokenRateLimitPolicy{}

func (p *TokenRateLimitPolicy) GetNamespace() string {
	return p.Namespace
}

func (p *TokenRateLimitPolicy) GetName() string {
	return p.Name
}

func (p *TokenRateLimitPolicy) GetLocator() string {
	return machinery.LocatorFromObject(p)
}

// Deprecated: Use GetTargetRefs instead
func (p *TokenRateLimitPolicy) GetTargetRef() gatewayapiv1alpha2.LocalPolicyTargetReference {
	return p.Spec.TargetRef.LocalPolicyTargetReference
}

func (p *TokenRateLimitPolicy) GetTargetRefs() []machinery.PolicyTargetReference {
	return []machinery.PolicyTargetReference{
		machinery.LocalPolicyTargetReferenceWithSectionName{
			LocalPolicyTargetReferenceWithSectionName: p.Spec.TargetRef,
			PolicyNamespace: p.Namespace,
		},
	}
}

func (p *TokenRateLimitPolicy) GetMergeStrategy() machinery.MergeStrategy {
	if spec := p.Spec.Defaults; spec != nil {
		return kuadrantv1.DefaultsMergeStrategy(spec.Strategy)
	}
	if spec := p.Spec.Overrides; spec != nil {
		return kuadrantv1.OverridesMergeStrategy(spec.Strategy)
	}
	return kuadrantv1.AtomicDefaultsMergeStrategy
}

func (p *TokenRateLimitPolicy) Merge(other machinery.Policy) machinery.Policy {
	source, ok := other.(*TokenRateLimitPolicy)
	if !ok {
		return p
	}
	return source.GetMergeStrategy()(source, p)
}

var _ kuadrantv1.MergeablePolicy = &TokenRateLimitPolicy{}

func (p *TokenRateLimitPolicy) Empty() bool {
	return len(p.Spec.Proper().Limits) == 0
}

func (p *TokenRateLimitPolicy) Rules() map[string]kuadrantv1.MergeableRule {
	rules := make(map[string]kuadrantv1.MergeableRule)
	policyLocator := p.GetLocator()
	spec := p.Spec.Proper()

	if whenPredicates := spec.MergeableWhenPredicates; len(whenPredicates.Predicates) > 0 {
		rules[kuadrantv1.RulesKeyTopLevelPredicates] = kuadrantv1.NewMergeableRule(&whenPredicates, policyLocator)
	}

	if spec.DataExtraction != nil {
		rules[RulesKeyDataExtraction] = kuadrantv1.NewMergeableRule(spec.DataExtraction, policyLocator)
	}

	for ruleID := range spec.Limits {
		limit := spec.Limits[ruleID]
		rules[ruleID] = kuadrantv1.NewMergeableRule(&limit, policyLocator)
	}

	return rules
}

func (p *TokenRateLimitPolicy) SetRules(rules map[string]kuadrantv1.MergeableRule) {
	// clear all rules of the policy before setting new ones
	p.Spec.Proper().Limits = nil
	p.Spec.Proper().MergeableWhenPredicates = kuadrantv1.MergeableWhenPredicates{}
	p.Spec.Proper().DataExtraction = nil

	if len(rules) > 0 {
		p.Spec.Proper().Limits = make(map[string]TokenLimit)
	}

	for ruleID := range rules {
		switch ruleID {
		case kuadrantv1.RulesKeyTopLevelPredicates:
			p.Spec.Proper().MergeableWhenPredicates = *rules[ruleID].(*kuadrantv1.MergeableWhenPredicates)
		case RulesKeyDataExtraction:
			p.Spec.Proper().DataExtraction = rules[ruleID].(*DataExtraction)
		default:
			p.Spec.Proper().Limits[ruleID] = *rules[ruleID].(*TokenLimit)
		}
	}
}

func (p *TokenRateLimitPolicy) GetStatus() kuadrantgatewayapi.PolicyStatus {
	return &p.Status
}

func (p *TokenRateLimitPolicy) Kind() string {
	return TokenRateLimitPolicyGroupKind.Kind
}

// +kubebuilder:validation:XValidation:rule="!(has(self.defaults) && has(self.limits))",message="Implicit and explicit defaults are mutually exclusive"
// +kubebuilder:validation:XValidation:rule="!(has(self.defaults) && has(self.overrides))",message="Overrides and explicit defaults are mutually exclusive"
// +kubebuilder:validation:XValidation:rule="!(has(self.overrides) && has(self.limits))",message="Overrides and implicit defaults are mutually exclusive"
// +kubebuilder:validation:XValidation:rule="!(has(self.overrides) || has(self.defaults)) ? has(self.limits) && size(self.limits) > 0 : true",message="At least one spec.limits must be defined"
// +kubebuilder:validation:XValidation:rule="has(self.overrides) ? has(self.overrides.limits) && size(self.overrides.limits) > 0 : true",message="At least one spec.overrides.limits must be defined"
// +kubebuilder:validation:XValidation:rule="has(self.defaults) ? has(self.defaults.limits) && size(self.defaults.limits) > 0 : true",message="At least one spec.defaults.limits must be defined"
// +kubebuilder:validation:XValidation:rule="!(has(self.defaults) && has(self.dataExtraction))",message="Implicit dataExtraction and explicit defaults are mutually exclusive"
// +kubebuilder:validation:XValidation:rule="!(has(self.overrides) && has(self.dataExtraction))",message="Implicit dataExtraction and overrides are mutually exclusive"
type TokenRateLimitPolicySpec struct {
	// Reference to the object to which this policy applies.
	// +kubebuilder:validation:XValidation:rule="self.group == 'gateway.networking.k8s.io'",message="Invalid targetRef.group. The only supported value is 'gateway.networking.k8s.io'"
	// +kubebuilder:validation:XValidation:rule="self.kind == 'HTTPRoute' || self.kind == 'Gateway'",message="Invalid targetRef.kind. The only supported values are 'HTTPRoute' and 'Gateway'"
	TargetRef gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName `json:"targetRef"`

	// Rules to apply as defaults. Can be overridden by more specific policy rules lower in the hierarchy and by less specific policy overrides.
	// Use one of: defaults, overrides, or bare set of policy rules (implicit defaults).
	// +optional
	Defaults *MergeableTokenRateLimitPolicySpec `json:"defaults,omitempty"`

	// Rules to apply as overrides. Override all policy rules lower in the hierarchy. Can be overridden by less specific policy overrides.
	// Use one of: defaults, overrides, or bare set of policy rules (implicit defaults).
	// +optional
	Overrides *MergeableTokenRateLimitPolicySpec `json:"overrides,omitempty"`

	// Bare set of policy rules (implicit defaults).
	// Use one of: defaults, overrides, or bare set of policy rules (implicit defaults).
	TokenRateLimitPolicySpecProper `json:""`
}

func (s *TokenRateLimitPolicySpec) Proper() *TokenRateLimitPolicySpecProper {
	if s.Defaults != nil {
		return &s.Defaults.TokenRateLimitPolicySpecProper
	}

	if s.Overrides != nil {
		return &s.Overrides.TokenRateLimitPolicySpecProper
	}

	return &s.TokenRateLimitPolicySpecProper
}

type MergeableTokenRateLimitPolicySpec struct {
	// Strategy defines the merge strategy to apply when merging this policy with other policies.
	// +kubebuilder:validation:Enum=atomic;merge
	// +kubebuilder:default=atomic
	Strategy string `json:"strategy,omitempty"`

	TokenRateLimitPolicySpecProper `json:""`
}

// TokenRateLimitPolicySpecProper contains common shared fields for defaults and overrides
type TokenRateLimitPolicySpecProper struct {
	// When holds a list of "top-level" `Predicate`s
	// +optional
	kuadrantv1.MergeableWhenPredicates `json:""`

	// Limits holds the struct of token-based limits indexed by a unique name
	// +optional
	Limits map[string]TokenLimit `json:"limits,omitempty"`

	// DataExtraction configures how token usage data is extracted from requests and responses.
	// If omitted, built-in defaults are used to extract total token usage from common LLM provider
	// response shapes.
	// +optional
	DataExtraction *DataExtraction `json:"dataExtraction,omitempty"`
}

// DataExtraction defines how the wasm data plane extracts application-level data (e.g. token usage)
// from requests and responses, for use in token-based rate limiting.
type DataExtraction struct {
	// Response configures data extraction from the response body.
	// +optional
	Response ResponseDataExtraction `json:"response,omitempty"`

	// Source stores the locator of the policy where this rule is originally defined (internal use)
	Source string `json:"-"`
}

// ResponseDataExtractionKeyTotalTokens is the key under which an ordered list of JSON Pointer
// candidates for total token usage extraction is stored in a ResponseDataExtraction map.
const ResponseDataExtractionKeyTotalTokens = "totalTokens"

// JSONPointerCandidates is an ordered list of JSON Pointer (RFC 6901) expressions evaluated against
// the response body. The first pointer that resolves to a numeric value is used. Reference tokens
// may not contain '"' or '\': these are legal, unescaped RFC 6901 characters, but this
// implementation embeds each pointer as a CEL string literal, so they are excluded here to avoid
// ambiguity between JSON Pointer content and CEL/wasm string escaping.
// +kubebuilder:validation:MinItems=1
// +kubebuilder:validation:MaxItems=8
// +kubebuilder:validation:items:Pattern=`^(/([^/~"\\]|~[01])*)+$`
type JSONPointerCandidates []string

// ResponseDataExtraction maps an extraction target name (e.g. "totalTokens", potentially
// contributed by an extension) to the JSONPointerCandidates used to resolve it.
// +kubebuilder:validation:MaxProperties=16
type ResponseDataExtraction map[string]JSONPointerCandidates

var _ kuadrantv1.MergeableRule = &DataExtraction{}

// ResponseTotalTokensPointers resolves the effective ordered list of JSON Pointer expressions used to
// extract total token usage from the response body, falling back to DefaultTotalTokensPointers when
// dataExtraction.response.totalTokens is unset. Safe to call on a nil receiver.
func (d *DataExtraction) ResponseTotalTokensPointers() []string {
	if d == nil {
		return DefaultTotalTokensPointers
	}
	if pointers := d.Response[ResponseDataExtractionKeyTotalTokens]; len(pointers) > 0 {
		return []string(pointers)
	}
	return DefaultTotalTokensPointers
}

func (d *DataExtraction) GetSpec() any {
	return d
}

func (d *DataExtraction) GetSource() string {
	return d.Source
}

func (d *DataExtraction) WithSource(source string) kuadrantv1.MergeableRule {
	d.Source = source
	return d
}

// TokenLimit represents a complete token-based rate limit configuration
type TokenLimit struct {
	// When holds a list of "limit-level" `Predicate`s for token-based conditions
	// Called also "soft" conditions as route selectors must also match
	// +optional
	When kuadrantv1.WhenPredicates `json:"when,omitempty"`

	// Rates holds the list of limit rates for token-based limiting
	// +optional
	Rates []kuadrantv1.Rate `json:"rates,omitempty"`

	// Counters defines additional rate limit counters based on CEL expressions which can reference well known selectors
	// TODO Document properly "Well-known selector" https://github.com/Kuadrant/architecture/blob/main/rfcs/0001-rlp-v2.md#well-known-selectors
	// +optional
	Counters []kuadrantv1.Counter `json:"counters,omitempty"`

	// Reservation configures token reservation for this limit. It only takes
	// effect when the Kuadrant CR spec.tokenRateLimiting.mode is Reservation:
	// an estimated token amount is reserved on request arrival and committed
	// with the actual usage once the upstream responds. When omitted, defaults
	// are generated (amount 0, meaning no capacity is reserved, and the route
	// backendRequest timeout for ttl).
	// +optional
	Reservation *Reservation `json:"reservation,omitempty"`

	// Source stores the locator of the policy where the limit is originally defined (internal use)
	Source string `json:"-"`
}

// Reservation configures token reservation behavior for a TokenLimit, used when
// the Kuadrant CR is in Reservation mode (see RFC 0021).
type Reservation struct {
	// Amount is either a literal integer number of tokens to reserve on request
	// arrival, or a CEL expression evaluating to the number of tokens (uint).
	// Defaults to 0 when omitted, which reserves no capacity: Limitador
	// short-circuits amount-0 reservations, so this limit behaves like
	// Optimistic unless amount is set explicitly to a non-zero estimate.
	// +optional
	// +kubebuilder:validation:XIntOrString
	Amount *intstr.IntOrString `json:"amount,omitempty"`

	// TTL is a CEL expression evaluating to the maximum duration
	// (google.protobuf.Duration) the reservation is held before it expires.
	// When omitted, the route's HTTPRoute.spec.rules[].timeouts.backendRequest
	// is used.
	// +optional
	TTL *kuadrantv1.Expression `json:"ttl,omitempty"`
}

func (l TokenLimit) CountersAsStringList() []string {
	if len(l.Counters) == 0 {
		return nil
	}
	return utils.Map(l.Counters, func(counter kuadrantv1.Counter) string {
		str := string(counter.Expression)
		if exp, err := transformer.TransformCounterVariable(str, false); err == nil {
			return *exp
		}
		return str
	})
}

var _ kuadrantv1.MergeableRule = &TokenLimit{}

func (l *TokenLimit) GetSpec() any {
	return l
}

func (l *TokenLimit) GetSource() string {
	return l.Source
}

func (l *TokenLimit) WithSource(source string) kuadrantv1.MergeableRule {
	l.Source = source
	return l
}

type TokenRateLimitPolicyStatus struct {
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

func (s *TokenRateLimitPolicyStatus) GetConditions() []metav1.Condition {
	return s.Conditions
}

//+kubebuilder:object:root=true

// TokenRateLimitPolicyList contains a list of TokenRateLimitPolicy
type TokenRateLimitPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TokenRateLimitPolicy `json:"items"`
}

