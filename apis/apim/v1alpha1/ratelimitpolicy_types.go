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
	limitadorv1alpha1 "github.com/kuadrant/limitador-operator/api/v1alpha1"
	istiov1alpha3 "istio.io/client-go/pkg/apis/networking/v1alpha3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

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

// +kubebuilder:validation:Enum=HTTPRoute;VirtualService
type NetworkingRefType string

const (
	RateLimitStagePREAUTH  RateLimitStage = "PREAUTH"
	RateLimitStagePOSTAUTH RateLimitStage = "POSTAUTH"
	RateLimitStageBOTH     RateLimitStage = "BOTH"

	NetworkingRefTypeHR NetworkingRefType = "HTTPRoute"
	NetworkingRefTypeVS NetworkingRefType = "VirtualService"
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

type Route struct {
	// name of the route present in the virutalservice
	Name string `json:"name"`
	// +optional
	RateLimits []*RateLimit `json:"rateLimits,omitempty"`
}

type NetworkingRef struct {
	Type NetworkingRefType `json:"type"`
	Name string            `json:"name"`
}

// RateLimitPolicySpec defines the desired state of RateLimitPolicy
type RateLimitPolicySpec struct {
	// route specific staging and actions
	//+listType=map
	//+listMapKey=name
	Routes []Route `json:"routes,omitempty"`

	// RateLimits are used for all of the matching rules
	// +optional
	RateLimits []*RateLimit                      `json:"rateLimits,omitempty"`
	Limits     []limitadorv1alpha1.RateLimitSpec `json:"limits,omitempty"`
}

type ObjectRefStatus struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

type VirtualServicesStatus struct {
	Name string `json:"name"`

	// +optional
	Gateways []ObjectRefStatus `json:"gateways,omitempty"`
}

// RateLimitPolicyStatus defines the observed state of RateLimitPolicy
type RateLimitPolicyStatus struct {
	// VirtualServices represents the current VirtualService objects with reference to this ratelimitpolicy object
	// +optional
	VirtualServices []VirtualServicesStatus `json:"virtualservices,omitempty"`
}

// AddVirtualService Adds virtualservice to the list of objects only if it does not exit. Returns true when added.
func (r *RateLimitPolicyStatus) AddVirtualService(vs *istiov1alpha3.VirtualService) bool {
	for idx := range r.VirtualServices {
		if r.VirtualServices[idx].Name == vs.Name {
			return false
		}
	}

	// Not found, add it

	newVSStatus := VirtualServicesStatus{
		Name: vs.Name,
	}

	for _, gw := range vs.Spec.Gateways {
		gwKey := common.NamespacedNameToObjectKey(gw, vs.Namespace)
		newVSStatus.Gateways = append(newVSStatus.Gateways, ObjectRefStatus{
			Name:      gwKey.Name,
			Namespace: gwKey.Namespace,
		})
	}

	r.VirtualServices = append(r.VirtualServices, newVSStatus)
	return true
}

// DeleteVirtualService removes virtualservice from the list of objects only if it does exit. Returns true when deleted from the list.
func (r *RateLimitPolicyStatus) DeleteVirtualService(vs *istiov1alpha3.VirtualService) bool {
	for idx := range r.VirtualServices {
		if r.VirtualServices[idx].Name == vs.Name {
			// remove the element at idx
			r.VirtualServices = append(r.VirtualServices[:idx], r.VirtualServices[idx+1:]...)
			return true
		}
	}
	return false
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
