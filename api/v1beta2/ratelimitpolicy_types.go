/*
Copyright 2021 Red Hat, Inc.

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

package v1beta2

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

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

const (
	EqualOperator      WhenConditionOperator = "eq"
	NotEqualOperator   WhenConditionOperator = "neq"
	StartsWithOperator WhenConditionOperator = "startswith"
	EndsWithOperator   WhenConditionOperator = "endswith"
	IncludeOperator    WhenConditionOperator = "incl"
	ExcludeOperator    WhenConditionOperator = "excl"
	MatchesOperator    WhenConditionOperator = "matches"

	RateLimitPolicyBackReferenceAnnotationName   = "kuadrant.io/ratelimitpolicies"
	RateLimitPolicyDirectReferenceAnnotationName = "kuadrant.io/ratelimitpolicy"
)

// +kubebuilder:validation:Enum:=second;minute;hour;day
type TimeUnit string

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

// RouteSelector defines semantics for matching an HTTP request based on conditions
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

// Limit represents a complete rate limit configuration
type Limit struct {
	// RouteSelectors defines semantics for matching an HTTP request based on conditions
	// +optional
	// +kubebuilder:validation:MaxItems=15
	RouteSelectors []RouteSelector `json:"routeSelectors,omitempty"`

	// When holds the list of conditions for the policy to be enforced.
	// Called also "soft" conditions as route selectors must also match
	// +optional
	When []WhenCondition `json:"when,omitempty"`

	// Counters defines additional rate limit counters based on context qualifiers and well known selectors
	// TODO Document properly "Well-known selector" https://github.com/Kuadrant/architecture/blob/main/rfcs/0001-rlp-v2.md#well-known-selectors
	// +optional
	Counters []ContextSelector `json:"counters,omitempty"`

	// Rates holds the list of limit rates
	// +optional
	Rates []Rate `json:"rates,omitempty"`
}

func (l Limit) CountersAsStringList() []string {
	if len(l.Counters) == 0 {
		return nil
	}
	return utils.Map(l.Counters, func(counter ContextSelector) string { return string(counter) })
}

// RateLimitPolicySpec defines the desired state of RateLimitPolicy
// +kubebuilder:validation:XValidation:rule="self.targetRef.kind != 'Gateway' || !has(self.limits) || !self.limits.exists(x, has(self.limits[x].routeSelectors))",message="route selectors not supported when targeting a Gateway"
// +kubebuilder:validation:XValidation:rule="!(has(self.defaults) && has(self.limits))",message="Implicit and explicit defaults are mutually exclusive"
// +kubebuilder:validation:XValidation:rule="!(has(self.defaults) && has(self.overrides))",message="Overrides and explicit defaults are mutually exclusive"
// +kubebuilder:validation:XValidation:rule="!(has(self.overrides) && has(self.limits))",message="Overrides and implicit defaults are mutually exclusive"
// +kubebuilder:validation:XValidation:rule="!(has(self.overrides) && self.targetRef.kind != 'Gateway')",message="Overrides are only allowed for policies targeting a Gateway resource"
type RateLimitPolicySpec struct {
	// TargetRef identifies an API object to apply policy to.
	// +kubebuilder:validation:XValidation:rule="self.group == 'gateway.networking.k8s.io'",message="Invalid targetRef.group. The only supported value is 'gateway.networking.k8s.io'"
	// +kubebuilder:validation:XValidation:rule="self.kind == 'HTTPRoute' || self.kind == 'Gateway'",message="Invalid targetRef.kind. The only supported values are 'HTTPRoute' and 'Gateway'"
	TargetRef gatewayapiv1alpha2.PolicyTargetReference `json:"targetRef"`

	// Defaults define explicit default values for this policy and for policies inheriting this policy.
	// Defaults are mutually exclusive with implicit defaults defined by RateLimitPolicyCommonSpec.
	// +optional
	Defaults *RateLimitPolicyCommonSpec `json:"defaults,omitempty"`

	// Overrides define override values for this policy and for policies inheriting this policy.
	// Overrides are mutually exclusive with implicit defaults and explicit Defaults defined by RateLimitPolicyCommonSpec.
	// +optional
	Overrides *RateLimitPolicyCommonSpec `json:"overrides,omitempty"`

	// RateLimitPolicyCommonSpec defines implicit default values for this policy and for policies inheriting this policy.
	// RateLimitPolicyCommonSpec is mutually exclusive with explicit defaults defined by Defaults.
	RateLimitPolicyCommonSpec `json:""`
}

// RateLimitPolicyCommonSpec contains common shared fields.
type RateLimitPolicyCommonSpec struct {
	// Limits holds the struct of limits indexed by a unique name
	// +optional
	// +kubebuilder:validation:MaxProperties=14
	Limits map[string]Limit `json:"limits,omitempty"`
}

// RateLimitPolicyStatus defines the observed state of RateLimitPolicy
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

func (s *RateLimitPolicyStatus) Equals(other *RateLimitPolicyStatus, logger logr.Logger) bool {
	if s.ObservedGeneration != other.ObservedGeneration {
		diff := cmp.Diff(s.ObservedGeneration, other.ObservedGeneration)
		logger.V(1).Info("ObservedGeneration not equal", "difference", diff)
		return false
	}

	// Marshalling sorts by condition type
	currentMarshaledJSON, _ := kuadrant.ConditionMarshal(s.Conditions)
	otherMarshaledJSON, _ := kuadrant.ConditionMarshal(other.Conditions)
	if string(currentMarshaledJSON) != string(otherMarshaledJSON) {
		if logger.V(1).Enabled() {
			diff := cmp.Diff(string(currentMarshaledJSON), string(otherMarshaledJSON))
			logger.V(1).Info("Conditions not equal", "difference", diff)
		}
		return false
	}

	return true
}

func (s *RateLimitPolicyStatus) GetConditions() []metav1.Condition {
	return s.Conditions
}

var _ kuadrant.Policy = &RateLimitPolicy{}
var _ kuadrant.Referrer = &RateLimitPolicy{}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:metadata:labels="gateway.networking.k8s.io/policy=inherited"
// +kubebuilder:printcolumn:name="Accepted",type=string,JSONPath=`.status.conditions[?(@.type=="Accepted")].status`,description="RateLimitPolicy Accepted",priority=2
// +kubebuilder:printcolumn:name="Enforced",type=string,JSONPath=`.status.conditions[?(@.type=="Enforced")].status`,description="RateLimitPolicy Enforced",priority=2
// +kubebuilder:printcolumn:name="TargetRefKind",type="string",JSONPath=".spec.targetRef.kind",description="Type of the referenced Gateway API resource",priority=2
// +kubebuilder:printcolumn:name="TargetRefName",type="string",JSONPath=".spec.targetRef.name",description="Name of the referenced Gateway API resource",priority=2
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// RateLimitPolicy enables rate limiting for service workloads in a Gateway API network
type RateLimitPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RateLimitPolicySpec   `json:"spec,omitempty"`
	Status RateLimitPolicyStatus `json:"status,omitempty"`
}

var _ kuadrantgatewayapi.Policy = &RateLimitPolicy{}

func (r *RateLimitPolicy) Validate() error {
	if r.Spec.TargetRef.Namespace != nil && string(*r.Spec.TargetRef.Namespace) != r.Namespace {
		return fmt.Errorf("invalid targetRef.Namespace %s. Currently only supporting references to the same namespace", *r.Spec.TargetRef.Namespace)
	}

	return nil
}

//+kubebuilder:object:root=true

// RateLimitPolicyList contains a list of RateLimitPolicy
type RateLimitPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RateLimitPolicy `json:"items"`
}

func (l *RateLimitPolicyList) GetItems() []kuadrant.Policy {
	return utils.Map(l.Items, func(item RateLimitPolicy) kuadrant.Policy {
		return &item
	})
}

func (r *RateLimitPolicy) GetTargetRef() gatewayapiv1alpha2.PolicyTargetReference {
	return r.Spec.TargetRef
}

func (r *RateLimitPolicy) GetStatus() kuadrantgatewayapi.PolicyStatus {
	return &r.Status
}

func (r *RateLimitPolicy) GetWrappedNamespace() gatewayapiv1.Namespace {
	return gatewayapiv1.Namespace(r.Namespace)
}

func (r *RateLimitPolicy) GetRulesHostnames() (ruleHosts []string) {
	ruleHosts = make([]string, 0)
	for _, limit := range r.Spec.CommonSpec().Limits {
		for _, routeSelector := range limit.RouteSelectors {
			convertHostnamesToString := func(gwHostnames []gatewayapiv1.Hostname) []string {
				hostnames := make([]string, 0, len(gwHostnames))
				for _, gwHostName := range gwHostnames {
					hostnames = append(hostnames, string(gwHostName))
				}
				return hostnames
			}
			ruleHosts = append(ruleHosts, convertHostnamesToString(routeSelector.Hostnames)...)
		}
	}
	return
}

func (r *RateLimitPolicy) Kind() string {
	return r.TypeMeta.Kind
}

func (r *RateLimitPolicy) List(ctx context.Context, c client.Client, namespace string) []kuadrantgatewayapi.Policy {
	policyList := &RateLimitPolicyList{}
	err := c.List(ctx, policyList, client.InNamespace(namespace))
	if err != nil {
		return []kuadrantgatewayapi.Policy{}
	}
	policies := make([]kuadrantgatewayapi.Policy, 0, len(policyList.Items))
	for i := range policyList.Items {
		policies = append(policies, &policyList.Items[i])
	}

	return policies
}

func (r *RateLimitPolicy) PolicyClass() kuadrantgatewayapi.PolicyClass {
	return kuadrantgatewayapi.InheritedPolicy
}

func (r *RateLimitPolicy) BackReferenceAnnotationName() string {
	return RateLimitPolicyBackReferenceAnnotationName
}

func (r *RateLimitPolicy) DirectReferenceAnnotationName() string {
	return RateLimitPolicyDirectReferenceAnnotationName
}

// CommonSpec returns the Default RateLimitPolicyCommonSpec if it is defined.
// Otherwise, it returns the RateLimitPolicyCommonSpec from the spec.
// This function should be used instead of accessing the fields directly, so that either the explicit or implicit default
// is returned.
func (r *RateLimitPolicySpec) CommonSpec() *RateLimitPolicyCommonSpec {
	if r.Defaults != nil {
		return r.Defaults
	}

	if r.Overrides != nil {
		return r.Overrides
	}

	return &r.RateLimitPolicyCommonSpec
}

func init() {
	SchemeBuilder.Register(&RateLimitPolicy{}, &RateLimitPolicyList{})
}
