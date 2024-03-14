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

package v1alpha1

import (
	"fmt"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
)

type RoutingStrategy string

const (
	SimpleRoutingStrategy       RoutingStrategy = "simple"
	LoadBalancedRoutingStrategy RoutingStrategy = "loadbalanced"

	DefaultWeight Weight  = 120
	DefaultGeo    GeoCode = "default"
	WildcardGeo   GeoCode = "*"

	DNSPolicyBackReferenceAnnotationName   = "kuadrant.io/dnspolicies"
	DNSPolicyDirectReferenceAnnotationName = "kuadrant.io/dnspolicy"
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
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="RoutingStrategy is immutable"
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

var _ kuadrant.Policy = &DNSPolicy{}
var _ kuadrant.Referrer = &DNSPolicy{}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:metadata:labels="gateway.networking.k8s.io/policy=direct"
// +kubebuilder:printcolumn:name="Status",type=string,JSONPath=`.status.conditions[0].reason`,description="DNSPolicy Status",priority=2
// +kubebuilder:printcolumn:name="TargetRefKind",type="string",JSONPath=".spec.targetRef.kind",description="Type of the referenced Gateway API resource",priority=2
// +kubebuilder:printcolumn:name="TargetRefName",type="string",JSONPath=".spec.targetRef.name",description="Name of the referenced Gateway API resource",priority=2
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

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
	return make([]string, 0)
}

func (p *DNSPolicy) GetTargetRef() gatewayapiv1alpha2.PolicyTargetReference {
	return p.Spec.TargetRef
}

func (p *DNSPolicy) Kind() string { return p.TypeMeta.Kind }

func (p *DNSPolicy) BackReferenceAnnotationName() string {
	return DNSPolicyBackReferenceAnnotationName
}

func (p *DNSPolicy) DirectReferenceAnnotationName() string {
	return DNSPolicyDirectReferenceAnnotationName
}

// Validate ensures the resource is valid. Compatible with the validating interface
// used by webhooks
func (p *DNSPolicy) Validate() error {
	if p.Spec.TargetRef.Group != gatewayapiv1.GroupName {
		return fmt.Errorf("invalid targetRef.Group %s. The only supported group is %s", p.Spec.TargetRef.Group, gatewayapiv1.GroupName)
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
	Endpoint                  string           `json:"endpoint,omitempty"`
	Port                      *int             `json:"port,omitempty"`
	Protocol                  *string          `json:"protocol,omitempty"`
	FailureThreshold          *int             `json:"failureThreshold,omitempty"`
	AdditionalHeadersRef      *string          `json:"additionalHeadersRef,omitempty"`
	ExpectedResponses         []int            `json:"expectedResponses,omitempty"`
	AllowInsecureCertificates bool             `json:"allowInsecureCertificates,omitempty"`
	Interval                  *metav1.Duration `json:"interval,omitempty"`
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
		protocol := "HTTPS"
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

//API Helpers

func NewDNSPolicy(name, ns string) *DNSPolicy {
	return &DNSPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Spec: DNSPolicySpec{},
	}
}

func (p *DNSPolicy) WithTargetRef(targetRef gatewayapiv1alpha2.PolicyTargetReference) *DNSPolicy {
	p.Spec.TargetRef = targetRef
	return p
}

func (p *DNSPolicy) WithHealthCheck(healthCheck HealthCheckSpec) *DNSPolicy {
	p.Spec.HealthCheck = &healthCheck
	return p
}

func (p *DNSPolicy) WithLoadBalancing(loadBalancing LoadBalancingSpec) *DNSPolicy {
	p.Spec.LoadBalancing = &loadBalancing
	return p
}

func (p *DNSPolicy) WithRoutingStrategy(strategy RoutingStrategy) *DNSPolicy {
	p.Spec.RoutingStrategy = strategy
	return p
}

//TargetRef

func (p *DNSPolicy) WithTargetGateway(gwName string) *DNSPolicy {
	typedNamespace := gatewayapiv1.Namespace(p.GetNamespace())
	return p.WithTargetRef(gatewayapiv1alpha2.PolicyTargetReference{
		Group:     gatewayapiv1.GroupName,
		Kind:      "Gateway",
		Name:      gatewayapiv1.ObjectName(gwName),
		Namespace: &typedNamespace,
	})
}

//HealthCheck

func (p *DNSPolicy) WithHealthCheckFor(endpoint string, port *int, protocol string, failureThreshold *int) *DNSPolicy {
	return p.WithHealthCheck(HealthCheckSpec{
		Endpoint:                  endpoint,
		Port:                      port,
		Protocol:                  &protocol,
		FailureThreshold:          failureThreshold,
		AdditionalHeadersRef:      nil,
		ExpectedResponses:         nil,
		AllowInsecureCertificates: false,
		Interval:                  nil,
	})
}

//LoadBalancing

func (p *DNSPolicy) WithLoadBalancingWeighted(lbWeighted LoadBalancingWeighted) *DNSPolicy {
	if p.Spec.LoadBalancing == nil {
		p.WithLoadBalancing(LoadBalancingSpec{})
	}
	p.Spec.LoadBalancing.Weighted = &lbWeighted
	return p
}

func (p *DNSPolicy) WithLoadBalancingGeo(lbGeo LoadBalancingGeo) *DNSPolicy {
	if p.Spec.LoadBalancing == nil {
		p.Spec.LoadBalancing = &LoadBalancingSpec{}
	}
	p.Spec.LoadBalancing.Geo = &lbGeo
	return p
}

func (p *DNSPolicy) WithLoadBalancingWeightedFor(defaultWeight Weight, custom []*CustomWeight) *DNSPolicy {
	return p.WithLoadBalancingWeighted(LoadBalancingWeighted{
		DefaultWeight: defaultWeight,
		Custom:        custom,
	})
}

func (p *DNSPolicy) WithLoadBalancingGeoFor(defaultGeo string) *DNSPolicy {
	return p.WithLoadBalancingGeo(LoadBalancingGeo{
		DefaultGeo: defaultGeo,
	})
}
