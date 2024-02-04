/*
Copyright 2023 The MultiCluster Traffic Controller Authors.

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
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantdnsv1alpha1 "github.com/kuadrant/kuadrant-dns-operator/api/v1alpha1"
)

type RoutingStrategy string

const (
	SimpleRoutingStrategy       RoutingStrategy = "simple"
	LoadBalancedRoutingStrategy RoutingStrategy = "loadbalanced"

	DefaultWeight Weight  = 120
	DefaultGeo    GeoCode = "default"
	WildcardGeo   GeoCode = "*"
)

// DNSPolicySpec defines the desired state of DNSPolicy
type DNSPolicySpec struct {

	// +kubebuilder:validation:Required
	// +required
	TargetRef gatewayapiv1alpha2.PolicyTargetReference `json:"targetRef"`

	// +optional
	HealthCheck *HealthCheckSpec `json:"healthCheck,omitempty"`

	// +optional
	LoadBalancing *LoadBalancingSpec `json:"loadBalancing"`

	// +required
	// +kubebuilder:validation:Enum=simple;loadbalanced
	// +kubebuilder:default=loadbalanced
	RoutingStrategy RoutingStrategy `json:"routingStrategy"`
}

type LoadBalancingSpec struct {
	// +optional
	Weighted *LoadBalancingWeighted `json:"weighted,omitempty"`
	// +optional
	Geo *LoadBalancingGeo `json:"geo,omitempty"`
}

// +kubebuilder:validation:Minimum=0
type Weight int

type CustomWeight struct {
	// Label selector used by MGC to match resource storing custom weight attribute values e.g. kuadrant.io/lb-attribute-custom-weight: AWS
	// +required
	Selector *metav1.LabelSelector `json:"selector"`
	// +required
	Weight Weight `json:"weight,omitempty"`
}

type LoadBalancingWeighted struct {
	// defaultWeight is the record weight to use when no other can be determined for a dns target cluster.
	//
	// The maximum value accepted is determined by the target dns provider, please refer to the appropriate docs below.
	//
	// Route53: https://docs.aws.amazon.com/Route53/latest/DeveloperGuide/routing-policy-weighted.html
	// +kubebuilder:default=120
	DefaultWeight Weight `json:"defaultWeight,omitempty"`
	// +optional
	Custom []*CustomWeight `json:"custom,omitempty"`
}

type GeoCode string

func (gc GeoCode) IsDefaultCode() bool {
	return gc == DefaultGeo
}

func (gc GeoCode) IsWildcard() bool {
	return gc == WildcardGeo
}

type LoadBalancingGeo struct {
	// defaultGeo is the country/continent/region code to use when no other can be determined for a dns target cluster.
	//
	// The values accepted are determined by the target dns provider, please refer to the appropriate docs below.
	//
	// Route53: https://docs.aws.amazon.com/Route53/latest/DeveloperGuide/resource-record-sets-values-geo.html
	// +required
	DefaultGeo string `json:"defaultGeo,omitempty"`
}

// DNSPolicyStatus defines the observed state of DNSPolicy
type DNSPolicyStatus struct {

	// conditions are any conditions associated with the policy
	//
	// If configuring the policy fails, the "Failed" condition will be set with a
	// reason and message describing the cause of the failure.
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// observedGeneration is the most recently observed generation of the
	// DNSPolicy.  When the DNSPolicy is updated, the controller updates the
	// corresponding configuration. If an update fails, that failure is
	// recorded in the status condition
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	HealthCheck *HealthCheckStatus `json:"healthCheck,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type==\"Ready\")].status",description="DNSPolicy ready."

// DNSPolicy is the Schema for the dnspolicies API
type DNSPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DNSPolicySpec   `json:"spec,omitempty"`
	Status DNSPolicyStatus `json:"status,omitempty"`
}

func (p *DNSPolicy) GetWrappedNamespace() gatewayapiv1.Namespace {
	return gatewayapiv1.Namespace(p.Namespace)
}

func (p *DNSPolicy) GetRulesHostnames() []string {
	//TODO implement me
	panic("implement me")
}

func (p *DNSPolicy) GetTargetRef() gatewayapiv1alpha2.PolicyTargetReference {
	return p.Spec.TargetRef
}

func (p *DNSPolicy) Kind() string { return p.TypeMeta.Kind }

// Validate ensures the resource is valid. Compatible with the validating interface
// used by webhooks
func (p *DNSPolicy) Validate() error {
	if p.Spec.TargetRef.Group != "gateway.networking.k8s.io" {
		return fmt.Errorf("invalid targetRef.Group %s. The only supported group is gateway.networking.k8s.io", p.Spec.TargetRef.Group)
	}

	if p.Spec.TargetRef.Kind != ("Gateway") {
		return fmt.Errorf("invalid targetRef.Kind %s. The only supported kind is Gateway", p.Spec.TargetRef.Kind)
	}

	if p.Spec.TargetRef.Namespace != nil && string(*p.Spec.TargetRef.Namespace) != p.Namespace {
		return fmt.Errorf("invalid targetRef.Namespace %s. Currently only supporting references to the same namespace", *p.Spec.TargetRef.Namespace)
	}

	if p.Spec.HealthCheck != nil {
		return p.Spec.HealthCheck.Validate()
	}

	return nil
}

// Default sets default values for the fields in the resource. Compatible with
// the defaulting interface used by webhooks
func (p *DNSPolicy) Default() {
	if p.Spec.HealthCheck != nil {
		p.Spec.HealthCheck.Default()
	}
}

//+kubebuilder:object:root=true

// DNSPolicyList contains a list of DNSPolicy
type DNSPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DNSPolicy `json:"items"`
}

// HealthCheckSpec configures health checks in the DNS provider.
// By default, this health check will be applied to each unique DNS A Record for
// the listeners assigned to the target gateway
type HealthCheckSpec struct {
	Endpoint                  string                                    `json:"endpoint,omitempty"`
	Port                      *int                                      `json:"port,omitempty"`
	Protocol                  *kuadrantdnsv1alpha1.HealthProtocol       `json:"protocol,omitempty"`
	FailureThreshold          *int                                      `json:"failureThreshold,omitempty"`
	AdditionalHeadersRef      *kuadrantdnsv1alpha1.AdditionalHeadersRef `json:"additionalHeadersRef,omitempty"`
	ExpectedResponses         []int                                     `json:"expectedResponses,omitempty"`
	AllowInsecureCertificates bool                                      `json:"allowInsecureCertificates,omitempty"`
	Interval                  *metav1.Duration                          `json:"interval,omitempty"`
}

func (s *HealthCheckSpec) Validate() error {
	if s.Interval != nil {
		if s.Interval.Duration < (time.Second * 5) {
			return fmt.Errorf("invalid value for spec.healthCheckSpec.interval %v, it cannot be shorter than 5s", s.Interval.Duration)
		}
	}

	return nil
}

func (s *HealthCheckSpec) Default() {
	if s.Interval == nil {
		s.Interval = &metav1.Duration{
			Duration: time.Second * 30,
		}
	}

	if s.Protocol == nil {
		protocol := kuadrantdnsv1alpha1.HttpsProtocol
		s.Protocol = &protocol
	}
}

type HealthCheckStatus struct {
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

type DNSRecordRef struct {
	// +kubebuilder:validation:Required
	// +required
	Name string `json:"name"`
	// +kubebuilder:validation:Required
	// +required
	Namespace string `json:"namespace"`
}

func init() {
	SchemeBuilder.Register(&DNSPolicy{}, &DNSPolicyList{})
}
