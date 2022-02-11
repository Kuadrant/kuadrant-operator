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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

type RLGenericKey struct {
	DescriptorKey   string `json:"descriptor_key"`
	DescriptorValue string `json:"descriptor_value"`
}

type ActionSpecifier struct {
	GenericKey RLGenericKey `json:"generic_key"`
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

var RateLimitStageName = map[int32]string{
	0: "PREAUTH",
	1: "POSTAUTH",
	2: "BOTH",
}

var RateLimitStageValue = map[RateLimitStage]int32{
	"PREAUTH":  0,
	"POSTAUTH": 1,
	"BOTH":     2,
}

type Route struct {
	// name of the route present in the virutalservice
	Name string `json:"name"`
	// Definfing phase at which rate limits will be applied.
	// Valid values are: PREAUTH, POSTAUTH, BOTH
	Stage RateLimitStage `json:"stage"`
	// rule specific actions
	Actions []*ActionSpecifier `json:"actions,omitempty"`
}

type NetworkingRef struct {
	Type NetworkingRefType `json:"type"`
	Name string            `json:"name"`
}

// RateLimitPolicySpec defines the desired state of RateLimitPolicy
type RateLimitPolicySpec struct {
	//+listType=map
	//+listMapKey=type
	//+listMapKey=name
	NetworkingRef []NetworkingRef `json:"networkingRef,omitempty"`
	// route specific staging and actions
	//+listType=map
	//+listMapKey=name
	Routes []Route `json:"routes,omitempty"`
	// these actions are used for all of the matching rules
	Actions []*ActionSpecifier                `json:"actions,omitempty"`
	Limits  []limitadorv1alpha1.RateLimitSpec `json:"limits,omitempty"`
}

//+kubebuilder:object:root=true

// RateLimitPolicy is the Schema for the ratelimitpolicies API
type RateLimitPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec RateLimitPolicySpec `json:"spec,omitempty"`
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
