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
	"github.com/kuadrant/policy-machinery/machinery"
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
	EqualOperator      WhenConditionOperator = "eq"
	NotEqualOperator   WhenConditionOperator = "neq"
	StartsWithOperator WhenConditionOperator = "startsWith"
	EndsWithOperator   WhenConditionOperator = "endsWith"
	IncludeOperator    WhenConditionOperator = "incl"
	ExcludeOperator    WhenConditionOperator = "excl"
	MatchesOperator    WhenConditionOperator = "matches"

	// TODO: remove after fixing the integration tests that still depend on these
	RateLimitPolicyBackReferenceAnnotationName   = "kuadrant.io/ratelimitpolicies"
	RateLimitPolicyDirectReferenceAnnotationName = "kuadrant.io/ratelimitpolicy"
)

var (
	RateLimitPolicyGroupKind  = schema.GroupKind{Group: SchemeGroupVersion.Group, Kind: "RateLimitPolicy"}
	RateLimitPoliciesResource = SchemeGroupVersion.WithResource("ratelimitpolicies")
	// Top level predicate rules key starting with # to prevent conflict with limit names
	// TODO(eastizle): this coupling between limit names and rule IDs is a bad smell. Merging implementation should be enhanced.
	RulesKeyTopLevelPredicates = "###_TOP_LEVEL_PREDICATES_###"
)

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:metadata:labels="gateway.networking.k8s.io/policy=inherited"
// +kubebuilder:printcolumn:name="Accepted",type=string,JSONPath=`.status.conditions[?(@.type=="Accepted")].status`,description="RateLimitPolicy Accepted",priority=2
// +kubebuilder:printcolumn:name="Enforced",type=string,JSONPath=`.status.conditions[?(@.type=="Enforced")].status`,description="RateLimitPolicy Enforced",priority=2
// +kubebuilder:printcolumn:name="TargetKind",type="string",JSONPath=".spec.targetRef.kind",description="Kind of the object to which the policy aaplies",priority=2
// +kubebuilder:printcolumn:name="TargetName",type="string",JSONPath=".spec.targetRef.name",description="Name of the object to which the policy applies",priority=2
// +kubebuilder:printcolumn:name="TargetSection",type="string",JSONPath=".spec.targetRef.sectionName",description="Name of the section within the object to which the policy applies ",priority=2
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// RateLimitPolicy enables rate limiting for service workloads in a Gateway API network
type RateLimitPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RateLimitPolicySpec   `json:"spec,omitempty"`
	Status RateLimitPolicyStatus `json:"status,omitempty"`
}

var _ machinery.Policy = &RateLimitPolicy{}

func (p *RateLimitPolicy) GetNamespace() string {
	return p.Namespace
}

func (p *RateLimitPolicy) GetName() string {
	return p.Name
}

func (p *RateLimitPolicy) GetLocator() string {
	return machinery.LocatorFromObject(p)
}

// DEPRECATED: Use GetTargetRefs instead
func (p *RateLimitPolicy) GetTargetRef() gatewayapiv1alpha2.LocalPolicyTargetReference {
	return p.Spec.TargetRef.LocalPolicyTargetReference
}

func (p *RateLimitPolicy) GetTargetRefs() []machinery.PolicyTargetReference {
	return []machinery.PolicyTargetReference{
		machinery.LocalPolicyTargetReferenceWithSectionName{
			LocalPolicyTargetReferenceWithSectionName: p.Spec.TargetRef,
			PolicyNamespace: p.Namespace,
		},
	}
}

func (p *RateLimitPolicy) GetMergeStrategy() machinery.MergeStrategy {
	if spec := p.Spec.Defaults; spec != nil {
		return kuadrantv1.DefaultsMergeStrategy(spec.Strategy)
	}
	if spec := p.Spec.Overrides; spec != nil {
		return kuadrantv1.OverridesMergeStrategy(spec.Strategy)
	}
	return kuadrantv1.AtomicDefaultsMergeStrategy
}

func (p *RateLimitPolicy) Merge(other machinery.Policy) machinery.Policy {
	source, ok := other.(*RateLimitPolicy)
	if !ok {
		return p
	}
	return source.GetMergeStrategy()(source, p)
}

var _ kuadrantv1.MergeablePolicy = &RateLimitPolicy{}

func (p *RateLimitPolicy) Empty() bool {
	return len(p.Spec.Proper().Limits) == 0
}

func (p *RateLimitPolicy) Rules() map[string]kuadrantv1.MergeableRule {
	rules := make(map[string]kuadrantv1.MergeableRule)
	policyLocator := p.GetLocator()

	if len(p.Spec.Proper().When) > 0 {
		rules[RulesKeyTopLevelPredicates] = kuadrantv1.NewMergeableRule(
			&WhenPredicatesMergeableRule{When: p.Spec.Proper().When, Source: policyLocator},
			policyLocator,
		)
	}

	for ruleID := range p.Spec.Proper().Limits {
		limit := p.Spec.Proper().Limits[ruleID]
		rules[ruleID] = kuadrantv1.NewMergeableRule(&limit, policyLocator)
	}

	return rules
}

func (p *RateLimitPolicy) SetRules(rules map[string]kuadrantv1.MergeableRule) {
	// clear all rules of the policy before setting new ones
	p.Spec.Proper().Limits = nil
	p.Spec.Proper().When = nil

	if len(rules) > 0 {
		p.Spec.Proper().Limits = make(map[string]Limit)
	}

	for ruleID := range rules {
		if ruleID == RulesKeyTopLevelPredicates {
			p.Spec.Proper().When = rules[ruleID].(*WhenPredicatesMergeableRule).When
		} else {
			p.Spec.Proper().Limits[ruleID] = *rules[ruleID].(*Limit)
		}
	}
}

// DEPRECATED. impl: kuadrant.Policy
func (p *RateLimitPolicy) GetStatus() kuadrantgatewayapi.PolicyStatus {
	return &p.Status
}

// DEPRECATED. impl: kuadrant.Policy
func (p *RateLimitPolicy) PolicyClass() kuadrantgatewayapi.PolicyClass {
	return kuadrantgatewayapi.InheritedPolicy
}

// DEPRECATED. impl: kuadrant.Policy
func (p *RateLimitPolicy) GetWrappedNamespace() gatewayapiv1.Namespace {
	return gatewayapiv1.Namespace(p.GetNamespace())
}

// DEPRECATED. impl: kuadrant.Policy
func (p *RateLimitPolicy) GetRulesHostnames() []string {
	return []string{}
}

// DEPRECATED. impl: kuadrant.Policy
func (p *RateLimitPolicy) Kind() string {
	return RateLimitPolicyGroupKind.Kind
}

// TODO: remove
func (p *RateLimitPolicy) DirectReferenceAnnotationName() string {
	return RateLimitPolicyDirectReferenceAnnotationName
}

// TODO: remove
func (p *RateLimitPolicy) BackReferenceAnnotationName() string {
	return RateLimitPolicyBackReferenceAnnotationName
}

// +kubebuilder:validation:XValidation:rule="!(has(self.defaults) && has(self.limits))",message="Implicit and explicit defaults are mutually exclusive"
// +kubebuilder:validation:XValidation:rule="!(has(self.defaults) && has(self.overrides))",message="Overrides and explicit defaults are mutually exclusive"
// +kubebuilder:validation:XValidation:rule="!(has(self.overrides) && has(self.limits))",message="Overrides and implicit defaults are mutually exclusive"
type RateLimitPolicySpec struct {
	// Reference to the object to which this policy applies.
	// +kubebuilder:validation:XValidation:rule="self.group == 'gateway.networking.k8s.io'",message="Invalid targetRef.group. The only supported value is 'gateway.networking.k8s.io'"
	// +kubebuilder:validation:XValidation:rule="self.kind == 'HTTPRoute' || self.kind == 'Gateway'",message="Invalid targetRef.kind. The only supported values are 'HTTPRoute' and 'Gateway'"
	TargetRef gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName `json:"targetRef"`

	// Rules to apply as defaults. Can be overridden by more specific policiy rules lower in the hierarchy and by less specific policy overrides.
	// Use one of: defaults, overrides, or bare set of policy rules (implicit defaults).
	// +optional
	Defaults *MergeableRateLimitPolicySpec `json:"defaults,omitempty"`

	// Rules to apply as overrides. Override all policy rules lower in the hierarchy. Can be overridden by less specific policy overrides.
	// Use one of: defaults, overrides, or bare set of policy rules (implicit defaults).
	// +optional
	Overrides *MergeableRateLimitPolicySpec `json:"overrides,omitempty"`

	// Bare set of policy rules (implicit defaults).
	// Use one of: defaults, overrides, or bare set of policy rules (implicit defaults).
	RateLimitPolicySpecProper `json:""`
}

func (s *RateLimitPolicySpec) Proper() *RateLimitPolicySpecProper {
	if s.Defaults != nil {
		return &s.Defaults.RateLimitPolicySpecProper
	}

	if s.Overrides != nil {
		return &s.Overrides.RateLimitPolicySpecProper
	}

	return &s.RateLimitPolicySpecProper
}

type MergeableRateLimitPolicySpec struct {
	// Strategy defines the merge strategy to apply when merging this policy with other policies.
	// +kubebuilder:validation:Enum=atomic;merge
	// +kubebuilder:default=atomic
	Strategy string `json:"strategy,omitempty"`

	RateLimitPolicySpecProper `json:""`
}

// RateLimitPolicySpecProper contains common shared fields for defaults and overrides
type RateLimitPolicySpecProper struct {
	// When holds a list of "top-level" `Predicate`s
	// +optional
	When WhenPredicates `json:"when,omitempty"`

	// Limits holds the struct of limits indexed by a unique name
	// +optional
	Limits map[string]Limit `json:"limits,omitempty"`
}

// Predicate defines one CEL expression that must be evaluated to bool
// +kubebuilder:validation:MinLength=1
// +kubebuilder:validation:MaxLength=253
type Predicate string

type WhenPredicates []Predicate

func (w WhenPredicates) Extend(other WhenPredicates) WhenPredicates {
	return append(w, other...)
}

func (w WhenPredicates) EqualTo(other WhenPredicates) bool {
	if len(w) != len(other) {
		return false
	}

	for i := range w {
		if w[i] != other[i] {
			return false
		}
	}

	return true
}

type WhenPredicatesMergeableRule struct {
	When WhenPredicates

	// Source stores the locator of the policy where the limit is orignaly defined (internal use)
	Source string
}

var _ kuadrantv1.MergeableRule = &WhenPredicatesMergeableRule{}

func (w *WhenPredicatesMergeableRule) GetSpec() any {
	return w.When
}

func (w *WhenPredicatesMergeableRule) GetSource() string {
	return w.Source
}

func (w *WhenPredicatesMergeableRule) WithSource(source string) kuadrantv1.MergeableRule {
	w.Source = source
	return w
}

// Limit represents a complete rate limit configuration
type Limit struct {
	// When holds a list of "limist-level" `Predicate`s
	// Called also "soft" conditions as route selectors must also match
	// +optional
	When WhenPredicates `json:"when,omitempty"`

	// Counters defines additional rate limit counters based on context qualifiers and well known selectors
	// TODO Document properly "Well-known selector" https://github.com/Kuadrant/architecture/blob/main/rfcs/0001-rlp-v2.md#well-known-selectors
	// +optional
	Counters []ContextSelector `json:"counters,omitempty"`

	// Rates holds the list of limit rates
	// +optional
	Rates []Rate `json:"rates,omitempty"`

	// Source stores the locator of the policy where the limit is orignaly defined (internal use)
	Source string `json:"-"`
}

func (l Limit) CountersAsStringList() []string {
	if len(l.Counters) == 0 {
		return nil
	}
	return utils.Map(l.Counters, func(counter ContextSelector) string { return string(counter) })
}

var _ kuadrantv1.MergeableRule = &Limit{}

func (l *Limit) GetSpec() any {
	return l
}

func (l *Limit) GetSource() string {
	return l.Source
}

func (l *Limit) WithSource(source string) kuadrantv1.MergeableRule {
	l.Source = source
	return l
}

// +kubebuilder:validation:Enum:=second;minute;hour;day
type TimeUnit string

var timeUnitMap = map[TimeUnit]int{
	TimeUnit("second"): 1,
	TimeUnit("minute"): 60,
	TimeUnit("hour"):   60 * 60,
	TimeUnit("day"):    60 * 60 * 24,
}

// Rate defines the actual rate limit that will be used when there is a match
type Rate struct {
	// Limit defines the max value allowed for a given period of time
	Limit int `json:"limit"`

	// Duration defines the time period for which the Limit specified above applies.
	Duration int `json:"duration"`

	// Duration defines the time uni
	// Possible values are: "second", "minute", "hour", "day"
	Unit TimeUnit `json:"unit"`
}

// ToSeconds converts the rate to to Limitador's Limit format (maxValue, seconds)
func (r Rate) ToSeconds() (maxValue, seconds int) {
	maxValue = r.Limit
	seconds = 0

	if tmpSecs, ok := timeUnitMap[r.Unit]; ok && r.Duration > 0 {
		seconds = tmpSecs * r.Duration
	}

	if r.Duration < 0 {
		seconds = 0
	}

	if r.Limit < 0 {
		maxValue = 0
	}

	return
}

// WhenCondition defines semantics for matching an HTTP request based on conditions
// https://gateway-api.sigs.k8s.io/reference/spec/#gateway.networking.k8s.io/v1.HTTPRouteSpec
type WhenCondition struct {
	// Selector defines one item from the well known selectors
	// TODO Document properly "Well-known selector" https://github.com/Kuadrant/architecture/blob/main/rfcs/0001-rlp-v2.md#well-known-selectors
	Selector ContextSelector `json:"selector"`

	// The binary operator to be applied to the content fetched from the selector
	// Possible values are: "eq" (equal to), "neq" (not equal to)
	Operator WhenConditionOperator `json:"operator"`

	// The value of reference for the comparison.
	Value string `json:"value"`
}

// ContextSelector defines one item from the well known attributes
// Attributes: https://www.envoyproxy.io/docs/envoy/latest/intro/arch_overview/advanced/attributes
// Well-known selectors: https://github.com/Kuadrant/architecture/blob/main/rfcs/0001-rlp-v2.md#well-known-selectors
// They are named by a dot-separated path (e.g. request.path)
// Example: "request.path" -> The path portion of the URL
// +kubebuilder:validation:MinLength=1
// +kubebuilder:validation:MaxLength=253
type ContextSelector string

// +kubebuilder:validation:Enum:=eq;neq;startswith;endswith;incl;excl;matches
type WhenConditionOperator string

type RateLimitPolicyStatus struct {
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

func (s *RateLimitPolicyStatus) GetConditions() []metav1.Condition {
	return s.Conditions
}

//+kubebuilder:object:root=true

// RateLimitPolicyList contains a list of RateLimitPolicy
type RateLimitPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RateLimitPolicy `json:"items"`
}

// DEPRECATED. impl: kuadrant.PolicyList
func (l *RateLimitPolicyList) GetItems() []kuadrant.Policy {
	return utils.Map(l.Items, func(item RateLimitPolicy) kuadrant.Policy {
		return &item
	})
}

func init() {
	SchemeBuilder.Register(&RateLimitPolicy{}, &RateLimitPolicyList{})
}
