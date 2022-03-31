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
	"github.com/kuadrant/kuadrant-controller/pkg/common"
	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	Rules []*Rule `json:"rules,omitempty"`
	// +optional
	RateLimits []*RateLimit                      `json:"rateLimits,omitempty"`
	Domain     string                            `json:"domain"`
	Limits     []limitadorv1alpha1.RateLimitSpec `json:"limits,omitempty"`
}

type RoutingResourceStatusEntry struct {
	Name string `json:"name"`

	// +optional
	Gateways []string `json:"gateways,omitempty"`
}

// RateLimitPolicyStatus defines the observed state of RateLimitPolicy
type RateLimitPolicyStatus struct {
	// VirtualServices represents the current VirtualService objects with reference to this ratelimitpolicy object
	// +optional
	VirtualServices []RoutingResourceStatusEntry `json:"virtualservices,omitempty"`
	HTTPRoutes      []RoutingResourceStatusEntry `json:"httproutes,omitempty"`
}

// AddEntry adds a new or update the existing entry in the status block.
func (r *RateLimitPolicyStatus) AddEntry(networkKind, networkName string, gateways []string) {
	if len(gateways) == 0 {
		return // without gateways it's same as doing nothing
	}

	entry := RoutingResourceStatusEntry{
		Name:     networkName,
		Gateways: gateways,
	}

	if networkKind == common.VirtualServiceKind {
		r.VirtualServices = append(r.VirtualServices, entry)
	} else {
		r.HTTPRoutes = append(r.HTTPRoutes, entry)
	}
}

// DeleteEntry removes the existing entry in the status block.
func (r *RateLimitPolicyStatus) DeleteEntry(networkKind, networkName string) {
	if networkKind == common.VirtualServiceKind {
		for idx := range r.VirtualServices {
			if r.VirtualServices[idx].Name == networkName {
				// remove the element at idx
				r.VirtualServices = append(r.VirtualServices[:idx], r.VirtualServices[idx+1:]...)
				break
			}
		}
	} else {
		for idx := range r.HTTPRoutes {
			if r.HTTPRoutes[idx].Name == networkName {
				r.HTTPRoutes = append(r.HTTPRoutes[:idx], r.HTTPRoutes[idx+1:]...)
				break
			}
		}
	}
}

func (r *RateLimitPolicyStatus) GetGateways(networkKind, networkName string) []string {
	var results []string
	if networkKind == common.VirtualServiceKind {
		for idx := range r.VirtualServices {
			if r.VirtualServices[idx].Name == networkName {
				gws := r.VirtualServices[idx].Gateways
				results = append(results, gws...)
			}
		}
	} else {
		for idx := range r.HTTPRoutes {
			if r.HTTPRoutes[idx].Name == networkName {
				gws := r.HTTPRoutes[idx].Gateways
				results = append(results, gws...)
			}
		}
	}

	return results
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
