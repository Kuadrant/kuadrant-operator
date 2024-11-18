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
	"time"

	"github.com/kuadrant/policy-machinery/machinery"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/pkg/utils"
)

var (
	RateLimitPolicyGroupKind  = schema.GroupKind{Group: GroupVersion.Group, Kind: "RateLimitPolicy"}
	RateLimitPoliciesResource = GroupVersion.WithResource("ratelimitpolicies")
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

// Deprecated: Use GetTargetRefs instead
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
		return DefaultsMergeStrategy(spec.Strategy)
	}
	if spec := p.Spec.Overrides; spec != nil {
		return OverridesMergeStrategy(spec.Strategy)
	}
	return AtomicDefaultsMergeStrategy
}

func (p *RateLimitPolicy) Merge(other machinery.Policy) machinery.Policy {
	source, ok := other.(*RateLimitPolicy)
	if !ok {
		return p
	}
	return source.GetMergeStrategy()(source, p)
}

var _ MergeablePolicy = &RateLimitPolicy{}

func (p *RateLimitPolicy) Empty() bool {
	return len(p.Spec.Proper().Limits) == 0
}

func (p *RateLimitPolicy) Rules() map[string]MergeableRule {
	rules := make(map[string]MergeableRule)
	policyLocator := p.GetLocator()
	spec := p.Spec.Proper()

	if whenPredicates := spec.MergeableWhenPredicates; len(whenPredicates.Predicates) > 0 {
		rules[RulesKeyTopLevelPredicates] = NewMergeableRule(&whenPredicates, policyLocator)
	}

	for ruleID := range spec.Limits {
		limit := spec.Limits[ruleID]
		rules[ruleID] = NewMergeableRule(&limit, policyLocator)
	}

	return rules
}

func (p *RateLimitPolicy) SetRules(rules map[string]MergeableRule) {
	// clear all rules of the policy before setting new ones
	p.Spec.Proper().Limits = nil
	p.Spec.Proper().Predicates = nil

	if len(rules) > 0 {
		p.Spec.Proper().Limits = make(map[string]Limit)
	}

	for ruleID := range rules {
		if ruleID == RulesKeyTopLevelPredicates {
			p.Spec.Proper().MergeableWhenPredicates = *rules[ruleID].(*MergeableWhenPredicates)
		} else {
			p.Spec.Proper().Limits[ruleID] = *rules[ruleID].(*Limit)
		}
	}
}

func (p *RateLimitPolicy) GetStatus() kuadrantgatewayapi.PolicyStatus {
	return &p.Status
}

func (p *RateLimitPolicy) Kind() string {
	return RateLimitPolicyGroupKind.Kind
}

// +kubebuilder:validation:XValidation:rule="!(has(self.defaults) && has(self.limits))",message="Implicit and explicit defaults are mutually exclusive"
// +kubebuilder:validation:XValidation:rule="!(has(self.defaults) && has(self.overrides))",message="Overrides and explicit defaults are mutually exclusive"
// +kubebuilder:validation:XValidation:rule="!(has(self.overrides) && has(self.limits))",message="Overrides and implicit defaults are mutually exclusive"
// +kubebuilder:validation:XValidation:rule="!(has(self.overrides) || has(self.defaults)) ? has(self.limits) && size(self.limits) > 0 : true",message="At least one spec.limits must be defined"
// +kubebuilder:validation:XValidation:rule="has(self.overrides) ? has(self.overrides.limits) && size(self.overrides.limits) > 0 : true",message="At least one spec.overrides.limits must be defined"
// +kubebuilder:validation:XValidation:rule="has(self.defaults) ? has(self.defaults.limits) && size(self.defaults.limits) > 0 : true",message="At least one spec.defaults.limits must be defined"
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
	MergeableWhenPredicates `json:""`

	// Limits holds the struct of limits indexed by a unique name
	// +optional
	Limits map[string]Limit `json:"limits,omitempty"`
}

type Counter struct {
	Expression Expression `json:"expression"`
}

// Limit represents a complete rate limit configuration
type Limit struct {
	// When holds a list of "limit-level" `Predicate`s
	// Called also "soft" conditions as route selectors must also match
	// +optional
	When WhenPredicates `json:"when,omitempty"`

	// Counters defines additional rate limit counters based on CEL expressions which can reference well known selectors
	// TODO Document properly "Well-known selector" https://github.com/Kuadrant/architecture/blob/main/rfcs/0001-rlp-v2.md#well-known-selectors
	// +optional
	Counters []Counter `json:"counters,omitempty"`

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
	return utils.Map(l.Counters, func(counter Counter) string { return string(counter.Expression) })
}

var _ MergeableRule = &Limit{}

func (l *Limit) GetSpec() any {
	return l
}

func (l *Limit) GetSource() string {
	return l.Source
}

func (l *Limit) WithSource(source string) MergeableRule {
	l.Source = source
	return l
}

// Duration follows Gateway API Duration format: https://gateway-api.sigs.k8s.io/geps/gep-2257/?h=duration#gateway-api-duration-format
// MUST match the regular expression ^([0-9]{1,5}(h|m|s|ms)){1,4}$
// MUST be interpreted as specified by Golang's time.ParseDuration
// +kubebuilder:validation:Pattern=`^([0-9]{1,5}(h|m|s|ms)){1,4}$`
type Duration string

func (d Duration) Seconds() int {
	duration, err := time.ParseDuration(string(d))
	if err != nil {
		return 0
	}

	return int(duration.Seconds())
}

// Rate defines the actual rate limit that will be used when there is a match
type Rate struct {
	// Limit defines the max value allowed for a given period of time
	Limit int `json:"limit"`

	// Window defines the time period for which the Limit specified above applies.
	Window Duration `json:"window"`
}

// ToSeconds converts the rate to to Limitador's Limit format (maxValue, seconds)
func (r Rate) ToSeconds() (maxValue, seconds int) {
	maxValue = r.Limit
	seconds = r.Window.Seconds()

	if r.Limit < 0 {
		maxValue = 0
	}

	return
}

// Expression defines one CEL expression
// Expression can use well known attributes
// Attributes: https://www.envoyproxy.io/docs/envoy/latest/intro/arch_overview/advanced/attributes
// Well-known selectors: https://github.com/Kuadrant/architecture/blob/main/rfcs/0001-rlp-v2.md#well-known-selectors
// They are named by a dot-separated path (e.g. request.path)
// Example: "request.path" -> The path portion of the URL
// +kubebuilder:validation:MinLength=1
type Expression string

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

func init() {
	SchemeBuilder.Register(&RateLimitPolicy{}, &RateLimitPolicyList{})
}
