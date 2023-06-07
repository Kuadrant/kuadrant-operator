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

package v1beta1

import (
	"fmt"
	"reflect"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
	gatewayapiv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// MetadataSource https://www.envoyproxy.io/docs/envoy/latest/api-v3/config/route/v3/route_components.proto#envoy-v3-api-enum-config-route-v3-ratelimit-action-metadata-source

// +kubebuilder:validation:Enum=DYNAMIC;ROUTE_ENTRY
type MetadataSource string

type GenericKeySpec struct {
	DescriptorValue string `json:"descriptor_value"`
	// +optional
	DescriptorKey *string `json:"descriptor_key,omitempty"`
}

type MetadataPathSegmentKey struct {
	Key string `json:"key"`
}

type MetadataPathSegment struct {
	Segment MetadataPathSegmentKey `json:"segment"`
}

type MetadataKeySpec struct {
	Key  string                `json:"key"`
	Path []MetadataPathSegment `json:"path"`
}

type MetadataSpec struct {
	DescriptorKey string          `json:"descriptor_key"`
	MetadataKey   MetadataKeySpec `json:"metadata_key"`
	// +optional
	DefaultValue *string `json:"default_value,omitempty"`
	// +kubebuilder:default=DYNAMIC
	Source MetadataSource `json:"source,omitempty"`
}

// RemoteAddressSpec no need to specify
// descriptor entry is populated using the trusted address from
// [x-forwarded-for](https://www.envoyproxy.io/docs/envoy/latest/configuration/http/http_conn_man/headers#config-http-conn-man-headers-x-forwarded-for)
type RemoteAddressSpec struct {
}

// RequestHeadersSpec Rate limit on request headers.
type RequestHeadersSpec struct {
	HeaderName    string `json:"header_name"`
	DescriptorKey string `json:"descriptor_key"`
	// +optional
	SkipIfAbsent *bool `json:"skip_if_absent,omitempty"`
}

// Action_Specifier defines one envoy rate limit action
type ActionSpecifier struct {
	// +optional
	GenericKey *GenericKeySpec `json:"generic_key,omitempty"`
	// +optional
	Metadata *MetadataSpec `json:"metadata,omitempty"`
	// +optional
	RemoteAddress *RemoteAddressSpec `json:"remote_address,omitempty"`
	// +optional
	RequestHeaders *RequestHeadersSpec `json:"request_headers,omitempty"`
}

// Rule defines a single condition for the rate limit configuration
// All defined fields within the rule must be met to have a rule match
type Rule struct {
	// +optional
	Paths []string `json:"paths,omitempty"`
	// +optional
	Methods []string `json:"methods,omitempty"`
	// +optional
	Hosts []string `json:"hosts,omitempty"`
}

// Configuration represents an action configuration.
// The equivalent of [config.route.v3.RateLimit](https://www.envoyproxy.io/docs/envoy/latest/api-v3/config/route/v3/route_components.proto#envoy-v3-api-msg-config-route-v3-ratelimit)
// envoy object.
// Each action configuration produces, at most, one descriptor.
// Depending on the incoming request, one configuration may or may not produce
// a rate limit descriptor.
type Configuration struct {
	// Actions holds list of action specifiers. Each action specifier can only define one action type.
	Actions []ActionSpecifier `json:"actions"`
}

// Limit represents partially a Limitador limit.
type Limit struct {
	MaxValue int `json:"maxValue"`
	Seconds  int `json:"seconds"`

	// +optional
	Conditions []string `json:"conditions,omitempty"`
	// +optional
	Variables []string `json:"variables,omitempty"`
}

func LimitFromLimitadorRateLimit(limit *limitadorv1alpha1.RateLimit) *Limit {
	if limit == nil {
		return nil
	}

	rlpLimit := &Limit{
		MaxValue:   limit.MaxValue,
		Seconds:    limit.Seconds,
		Conditions: nil,
		Variables:  nil,
	}

	if limit.Conditions != nil {
		// deep copy
		rlpLimit.Conditions = make([]string, len(limit.Conditions))
		copy(rlpLimit.Conditions, limit.Conditions)
	}

	if limit.Variables != nil {
		// deep copy
		rlpLimit.Variables = make([]string, len(limit.Variables))
		copy(rlpLimit.Variables, limit.Variables)
	}

	return rlpLimit
}

// RateLimit represents a complete rate limit configuration
type RateLimit struct {
	// Configurations holds list of (action) configuration.
	// +optional
	Configurations []Configuration `json:"configurations,omitempty"`

	// Rules represents the definition of the scope of the rate limit object
	// Defines a list of conditions for which rate limit configuration will apply
	// Matching occurs when at least one rule applies against the incoming request.
	// If rules are not set, or empty, it is equivalent to matching all the requests.
	// +optional
	Rules []Rule `json:"rules,omitempty"`

	// Limits holds a list of Limitador limits
	// +optional
	Limits []Limit `json:"limits,omitempty"`
}

type GatewayRateLimits struct {
	GatewayName string `json:"gatewayName"`

	// RateLimits holds the list of rate limit configurations
	// +optional
	RateLimits []RateLimit `json:"rateLimits,omitempty"`
}

// RateLimitPolicySpec defines the desired state of RateLimitPolicy
type RateLimitPolicySpec struct {
	// TargetRef identifies an API object to apply policy to.
	TargetRef gatewayapiv1alpha2.PolicyTargetReference `json:"targetRef"`
	// RateLimits holds the list of rate limit configurations
	// +optional
	RateLimits []RateLimit `json:"rateLimits,omitempty"`
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

	// GatewaysRateLimits shows the rate limit configuration applied by policies at the gateway level
	// this field is only meant for rate limit policies targeting a route
	// +optional
	GatewaysRateLimits []GatewayRateLimits `json:"gatewaysRateLimits,omitempty"`
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
		diff := cmp.Diff(string(currentMarshaledJSON), string(otherMarshaledJSON))
		logger.V(1).Info("Conditions not equal", "difference", diff)
		return false
	}

	// TODO(eastizle): reflect.DeepEqual does not work well with lists without order
	if !reflect.DeepEqual(s.GatewaysRateLimits, other.GatewaysRateLimits) {
		logger.V(1).Info("GatewaysRateLimits not equal")
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

// FlattenLimits returns Limit list from RateLimit list
func (r *RateLimitPolicy) FlattenLimits() []Limit {
	flattenLimits := make([]Limit, 0)

	for idx := range r.Spec.RateLimits {
		flattenLimits = append(flattenLimits, r.Spec.RateLimits[idx].Limits...)
	}

	return flattenLimits
}

//+kubebuilder:object:root=true

// RateLimitPolicyList contains a list of RateLimitPolicy
type RateLimitPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RateLimitPolicy `json:"items"`
}

func (r *RateLimitPolicy) GetTargetRef() gatewayapiv1alpha2.PolicyTargetReference {
	return r.Spec.TargetRef
}

func (r *RateLimitPolicy) GetWrappedNamespace() gatewayapiv1beta1.Namespace {
	return gatewayapiv1beta1.Namespace(r.Namespace)
}

func (r *RateLimitPolicy) GetRulesHostnames() (ruleHosts []string) {
	ruleHosts = make([]string, 0)
	for idx := range r.Spec.RateLimits {
		for ruleIdx := range r.Spec.RateLimits[idx].Rules {
			ruleHosts = append(ruleHosts, r.Spec.RateLimits[idx].Rules[ruleIdx].Hosts...)
		}
	}
	return
}

func init() {
	SchemeBuilder.Register(&RateLimitPolicy{}, &RateLimitPolicyList{})
}
