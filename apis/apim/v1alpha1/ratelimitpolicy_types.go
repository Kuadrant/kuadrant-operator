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

package v1alpha1

import (
	"fmt"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	"github.com/kuadrant/kuadrant-controller/pkg/common"
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

type MetadataPathSegment struct {
	Key string `json:"key"`
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
	// +optional
	Source *MetadataSource `json:"source,omitempty"`
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

// +kubebuilder:validation:Enum=PREAUTH;POSTAUTH;BOTH
type RateLimitStage string

const (
	RateLimitStagePREAUTH  RateLimitStage = "PREAUTH"
	RateLimitStagePOSTAUTH RateLimitStage = "POSTAUTH"
	RateLimitStageBOTH     RateLimitStage = "BOTH"
)

var RateLimitStageValue = map[RateLimitStage]int32{
	"PREAUTH":  0,
	"POSTAUTH": 1,
}

type RateLimit struct {
	// Definfing phase at which rate limits will be applied.
	// Valid values are: PREAUTH, POSTAUTH, BOTH
	Stage RateLimitStage `json:"stage"`
	// +optional
	Actions []*ActionSpecifier `json:"actions,omitempty"`
}

// Each operation type has OR semantics and overall AND semantics for a match.
type Operation struct {
	Paths   []string `json:"paths,omitempty"`
	Methods []string `json:"methods,omitempty"`
}

type Rule struct {
	// Name supports regex for fetching operations from routing resources
	// For VirtualService, if route name matches, all the match requests are
	// converted to operations internally. But specific match request names
	// are also supported.
	Name string `json:"name,omitempty"`
	// Operation specifies the operations of a request
	// +optional
	Operations []*Operation `json:"operations,omitempty"`
	// +optional
	RateLimits []*RateLimit `json:"rateLimits,omitempty"`
}

// RateLimitPolicySpec defines the desired state of RateLimitPolicy
type RateLimitPolicySpec struct {
	// TargetRef identifies an API object to apply policy to.
	TargetRef gatewayapiv1alpha2.PolicyTargetReference `json:"targetRef"`
	// +optional
	Rules []*Rule `json:"rules,omitempty"`
	// +optional
	RateLimits []*RateLimit `json:"rateLimits,omitempty"`
	Domain     string       `json:"domain"`
	// +optional
	Limits []limitadorv1alpha1.RateLimitSpec `json:"limits,omitempty"`
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

func (r *RateLimitPolicyStatus) Equals(other *RateLimitPolicyStatus, logger logr.Logger) bool {
	if r.ObservedGeneration != other.ObservedGeneration {
		diff := cmp.Diff(r.ObservedGeneration, other.ObservedGeneration)
		logger.V(1).Info("ObservedGeneration not equal", "difference", diff)
		return false
	}

	// Marshalling sorts by condition type
	currentMarshaledJSON, _ := common.ConditionMarshal(r.Conditions)
	otherMarshaledJSON, _ := common.ConditionMarshal(other.Conditions)
	if string(currentMarshaledJSON) != string(otherMarshaledJSON) {
		diff := cmp.Diff(string(currentMarshaledJSON), string(otherMarshaledJSON))
		logger.V(1).Info("Conditions not equal", "difference", diff)
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
	if r.Spec.TargetRef.Group != gatewayapiv1alpha2.Group("gateway.networking.k8s.io") {
		return fmt.Errorf("invalid targetRef.Group %s. The only supported group is gateway.networking.k8s.io", r.Spec.TargetRef.Group)
	}

	if r.Spec.TargetRef.Kind != gatewayapiv1alpha2.Kind("HTTPRoute") {
		return fmt.Errorf("invalid targetRef.Kind %s. The only supported kind is HTTPRoute", r.Spec.TargetRef.Kind)
	}

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

func init() {
	SchemeBuilder.Register(&RateLimitPolicy{}, &RateLimitPolicyList{})
}
