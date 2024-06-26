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
	"context"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
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
// +kubebuilder:validation:XValidation:rule="!(self.routingStrategy == 'loadbalanced' && !has(self.loadBalancing))",message="spec.loadBalancing is a required field when spec.routingStrategy == 'loadbalanced'"
type DNSPolicySpec struct {
	// TargetRef identifies an API object to apply policy to.
	// +kubebuilder:validation:XValidation:rule="self.group == 'gateway.networking.k8s.io'",message="Invalid targetRef.group. The only supported value is 'gateway.networking.k8s.io'"
	// +kubebuilder:validation:XValidation:rule="self.kind == 'Gateway'",message="Invalid targetRef.kind. The only supported values are 'Gateway'"
	TargetRef gatewayapiv1alpha2.PolicyTargetReference `json:"targetRef"`

	// +optional
	HealthCheck *HealthCheckSpec `json:"healthCheck,omitempty"`

	// +optional
	LoadBalancing *LoadBalancingSpec `json:"loadBalancing,omitempty"`

	// +kubebuilder:validation:Enum=simple;loadbalanced
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="RoutingStrategy is immutable"
	// +kubebuilder:default=loadbalanced
	RoutingStrategy RoutingStrategy `json:"routingStrategy"`
}

type LoadBalancingSpec struct {
	Weighted LoadBalancingWeighted `json:"weighted"`

	Geo LoadBalancingGeo `json:"geo"`
}

// +kubebuilder:validation:Minimum=0
type Weight int

type CustomWeight struct {
	// Label selector to match resource storing custom weight attribute values e.g. kuadrant.io/lb-attribute-custom-weight: AWS.
	Selector *metav1.LabelSelector `json:"selector"`

	// The weight value to apply when the selector matches.
	Weight Weight `json:"weight"`
}

type LoadBalancingWeighted struct {
	// defaultWeight is the record weight to use when no other can be determined for a dns target cluster.
	//
	// The maximum value accepted is determined by the target dns provider, please refer to the appropriate docs below.
	//
	// Route53: https://docs.aws.amazon.com/Route53/latest/DeveloperGuide/routing-policy-weighted.html
	DefaultWeight Weight `json:"defaultWeight"`

	// custom list of custom weight selectors.
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
	// Google: https://cloud.google.com/compute/docs/regions-zones
	// +kubebuilder:validation:MinLength=2
	DefaultGeo string `json:"defaultGeo"`
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

	// +optional
	HealthCheck *HealthCheckStatus `json:"healthCheck,omitempty"`

	// +optional
	RecordConditions map[string][]metav1.Condition `json:"recordConditions,omitempty"`
}

func (s *DNSPolicyStatus) GetConditions() []metav1.Condition {
	return s.Conditions
}

var _ kuadrant.Policy = &DNSPolicy{}
var _ kuadrant.Referrer = &DNSPolicy{}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:metadata:labels="gateway.networking.k8s.io/policy=direct"
// +kubebuilder:printcolumn:name="Accepted",type=string,JSONPath=`.status.conditions[?(@.type=="Accepted")].status`,description="DNSPolicy Accepted",priority=2
// +kubebuilder:printcolumn:name="Enforced",type=string,JSONPath=`.status.conditions[?(@.type=="Enforced")].status`,description="DNSPolicy Enforced",priority=2
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

func (p *DNSPolicy) GetStatus() kuadrantgatewayapi.PolicyStatus {
	return &p.Status
}

func (p *DNSPolicy) Kind() string { return p.TypeMeta.Kind }

func (p *DNSPolicy) List(ctx context.Context, c client.Client, namespace string) []kuadrantgatewayapi.Policy {
	policyList := &DNSPolicyList{}
	listOptions := &client.ListOptions{Namespace: namespace}
	err := c.List(ctx, policyList, listOptions)
	if err != nil {
		return []kuadrantgatewayapi.Policy{}
	}
	policies := make([]kuadrantgatewayapi.Policy, 0, len(policyList.Items))
	for i := range policyList.Items {
		policies = append(policies, &policyList.Items[i])
	}

	return policies
}

func (p *DNSPolicy) PolicyClass() kuadrantgatewayapi.PolicyClass {
	return kuadrantgatewayapi.DirectPolicy
}

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

	return nil
}

//+kubebuilder:object:root=true

// DNSPolicyList contains a list of DNSPolicy
type DNSPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DNSPolicy `json:"items"`
}

func (l *DNSPolicyList) GetItems() []kuadrant.Policy {
	return utils.Map(l.Items, func(item DNSPolicy) kuadrant.Policy {
		return &item
	})
}

// HealthCheckSpec configures health checks in the DNS provider.
// By default, this health check will be applied to each unique DNS A Record for
// the listeners assigned to the target gateway
type HealthCheckSpec struct {
	// Endpoint is the path to append to the host to reach the expected health check.
	// For example "/" or "/healthz" are common
	// +kubebuilder:example:=/
	Endpoint string `json:"endpoint"`
	// Port to connect to the host on
	// +kubebuilder:validation:Minimum:=1
	Port int `json:"port"`
	// Protocol to use when connecting to the host, valid values are "HTTP" or "HTTPS"
	// +kubebuilder:validation:Enum:=HTTP;HTTPS
	Protocol string `json:"protocol"`
	// FailureThreshold is a limit of consecutive failures that must occur for a host
	// to be considered unhealthy
	// +kubebuilder:validation:Minimum:=1
	FailureThreshold int `json:"failureThreshold"`
}

type HealthCheckStatus struct {
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

func init() {
	SchemeBuilder.Register(&DNSPolicy{}, &DNSPolicyList{})
}

//API Helpers

func NewDNSPolicy(name, ns string) *DNSPolicy {
	return &DNSPolicy{
		TypeMeta: metav1.TypeMeta{
			Kind:       "DNSPolicy",
			APIVersion: GroupVersion.String(),
		},
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

func (p *DNSPolicy) WithHealthCheckFor(endpoint string, port int, protocol string, failureThreshold int) *DNSPolicy {
	return p.WithHealthCheck(HealthCheckSpec{
		Endpoint:         endpoint,
		Port:             port,
		Protocol:         protocol,
		FailureThreshold: failureThreshold,
	})
}

//LoadBalancing

func (p *DNSPolicy) WithLoadBalancingFor(defaultWeight Weight, custom []*CustomWeight, defaultGeo string) *DNSPolicy {
	return p.WithLoadBalancing(LoadBalancingSpec{
		Weighted: LoadBalancingWeighted{
			DefaultWeight: defaultWeight,
			Custom:        custom,
		},
		Geo: LoadBalancingGeo{
			DefaultGeo: defaultGeo,
		},
	})
}
