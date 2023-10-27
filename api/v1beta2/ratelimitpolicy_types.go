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
	"fmt"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
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
	return common.Map(l.Counters, func(counter ContextSelector) string { return string(counter) })
}

// RateLimitPolicySpec defines the desired state of RateLimitPolicy
type RateLimitPolicySpec struct {
	// TargetRef identifies an API object to apply policy to.
	TargetRef gatewayapiv1alpha2.PolicyTargetReference `json:"targetRef"`

	// Limits holds the struct of limits indexed by a unique name
	// +optional
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
	currentMarshaledJSON, _ := common.ConditionMarshal(s.Conditions)
	otherMarshaledJSON, _ := common.ConditionMarshal(other.Conditions)
	if string(currentMarshaledJSON) != string(otherMarshaledJSON) {
		if logger.V(1).Enabled() {
			diff := cmp.Diff(string(currentMarshaledJSON), string(otherMarshaledJSON))
			logger.V(1).Info("Conditions not equal", "difference", diff)
		}
		return false
	}

	return true
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// RateLimitPolicy is the Schema for the ratelimitpolicies API
type RateLimitPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   RateLimitPolicySpec   `json:"spec,omitempty"`
	Status RateLimitPolicyStatus `json:"status,omitempty"`
}

func (r *RateLimitPolicy) Validate() error {
	if r.Spec.TargetRef.Group != ("gateway.networking.k8s.io") {
		return fmt.Errorf("invalid targetRef.Group %s. The only supported group is gateway.networking.k8s.io", r.Spec.TargetRef.Group)
	}

	if r.Spec.TargetRef.Kind != ("HTTPRoute") && r.Spec.TargetRef.Kind != ("Gateway") {
		return fmt.Errorf("invalid targetRef.Kind %s. The only supported kind types are HTTPRoute and Gateway", r.Spec.TargetRef.Kind)
	}

	if r.Spec.TargetRef.Namespace != nil && string(*r.Spec.TargetRef.Namespace) != r.Namespace {
		return fmt.Errorf("invalid targetRef.Namespace %s. Currently only supporting references to the same namespace", *r.Spec.TargetRef.Namespace)
	}

	// prevents usage of routeSelectors in a gateway RLP
	if r.Spec.TargetRef.Kind == ("Gateway") {
		for _, limit := range r.Spec.Limits {
			if len(limit.RouteSelectors) > 0 {
				return fmt.Errorf("route selectors not supported when targeting a Gateway")
			}
		}
	}

	return nil
}

func (r *RateLimitPolicy) TargetKey() client.ObjectKey {
	tmpNS := r.Namespace
	if r.Spec.TargetRef.Namespace != nil {
		tmpNS = string(*r.Spec.TargetRef.Namespace)
	}

	return client.ObjectKey{
		Name:      string(r.Spec.TargetRef.Name),
		Namespace: tmpNS,
	}
}

//+kubebuilder:object:root=true

// RateLimitPolicyList contains a list of RateLimitPolicy
type RateLimitPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RateLimitPolicy `json:"items"`
}

func (l *RateLimitPolicyList) GetItems() []common.KuadrantPolicy {
	return common.Map(l.Items, func(item RateLimitPolicy) common.KuadrantPolicy {
		return &item
	})
}

func (r *RateLimitPolicy) GetTargetRef() gatewayapiv1alpha2.PolicyTargetReference {
	return r.Spec.TargetRef
}

func (r *RateLimitPolicy) GetWrappedNamespace() gatewayapiv1.Namespace {
	return gatewayapiv1.Namespace(r.Namespace)
}

func (r *RateLimitPolicy) GetRulesHostnames() (ruleHosts []string) {
	ruleHosts = make([]string, 0)
	for _, limit := range r.Spec.Limits {
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

func init() {
	SchemeBuilder.Register(&RateLimitPolicy{}, &RateLimitPolicyList{})
}
