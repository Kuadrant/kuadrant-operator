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

package v1

import (
	"fmt"
	"net"
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	dnsv1alpha1 "github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/policy-machinery/machinery"

	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/pkg/kuadrant"
)

const (
	DefaultGeo  GeoCode = "default"
	WildcardGeo GeoCode = "*"
)

var (
	DNSPoliciesResource = GroupVersion.WithResource("dnspolicies")
	DNSPolicyGroupKind  = schema.GroupKind{Group: GroupVersion.Group, Kind: "DNSPolicy"}
)

// DNSPolicySpec defines the desired state of DNSPolicy
type DNSPolicySpec struct {
	// targetRef identifies an API object to apply policy to.
	// +kubebuilder:validation:XValidation:rule="self.group == 'gateway.networking.k8s.io'",message="Invalid targetRef.group. The only supported value is 'gateway.networking.k8s.io'"
	// +kubebuilder:validation:XValidation:rule="self.kind == 'Gateway'",message="Invalid targetRef.kind. The only supported values are 'Gateway'"
	TargetRef gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName `json:"targetRef"`

	// +optional
	HealthCheck *dnsv1alpha1.HealthCheckSpec `json:"healthCheck,omitempty"`

	// +optional
	LoadBalancing *LoadBalancingSpec `json:"loadBalancing,omitempty"`

	// providerRefs is a list of references to provider secrets. Max is one but intention is to allow this to be more in the future
	// +kubebuilder:validation:MaxItems=1
	// +kubebuilder:validation:MinItems=1
	ProviderRefs []dnsv1alpha1.ProviderRef `json:"providerRefs"`

	// ExcludeAddresses is a list of addresses (either hostnames, CIDR or IPAddresses) that DNSPolicy should not use as values in the configured DNS provider records. The default is to allow all addresses configured in the Gateway DNSPolicy is targeting
	// +optional
	ExcludeAddresses ExcludeAddresses `json:"excludeAddresses,omitempty"`
}

// +kubebuilder:validation:MaxItems=20
type ExcludeAddresses []string

func (ea ExcludeAddresses) Validate() error {
	for _, exclude := range ea {
		//Only a CIDR will have  / in the address so attempt to parse fail if not valid
		if strings.Contains(exclude, "/") {
			_, _, err := net.ParseCIDR(exclude)
			if err != nil {
				return fmt.Errorf("could not parse the CIDR from the excludeAddresses field %w", err)
			}
		}
	}
	return nil
}

type LoadBalancingSpec struct {
	// weight value to apply to weighted endpoints.
	//
	// The maximum value accepted is determined by the target dns provider, please refer to the appropriate docs below.
	//
	// Route53: https://docs.aws.amazon.com/Route53/latest/DeveloperGuide/routing-policy-weighted.html
	// Google: https://cloud.google.com/dns/docs/overview/
	// Azure: https://learn.microsoft.com/en-us/azure/traffic-manager/traffic-manager-routing-methods#weighted-traffic-routing-method
	// +kubebuilder:default=120
	Weight int `json:"weight"`

	// geo value to apply to geo endpoints.
	//
	// The values accepted are determined by the target dns provider, please refer to the appropriate docs below.
	//
	// Route53: https://docs.aws.amazon.com/Route53/latest/DeveloperGuide/resource-record-sets-values-geo.html
	// Google: https://cloud.google.com/compute/docs/regions-zones
	// Azure: https://learn.microsoft.com/en-us/azure/traffic-manager/traffic-manager-geographic-regions
	// +kubebuilder:validation:MinLength=2
	Geo string `json:"geo"`

	// defaultGeo specifies if this is the default geo for providers that support setting a default catch all geo endpoint such as Route53.
	DefaultGeo bool `json:"defaultGeo"`
}

type GeoCode string

func (gc GeoCode) IsDefaultCode() bool {
	return gc == DefaultGeo
}

func (gc GeoCode) IsWildcard() bool {
	return gc == WildcardGeo
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
	HealthCheck *dnsv1alpha1.HealthCheckStatus `json:"healthCheck,omitempty"`

	// +optional
	RecordConditions map[string][]metav1.Condition `json:"recordConditions,omitempty"`
	// TotalRecords records the total number of individual DNSRecords managed by this DNSPolicy
	// +optional
	TotalRecords int32 `json:"totalRecords,omitempty"`
}

func (s *DNSPolicyStatus) GetConditions() []metav1.Condition {
	return s.Conditions
}

var _ kuadrant.Policy = &DNSPolicy{}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:metadata:labels="gateway.networking.k8s.io/policy=direct"
// +kubebuilder:printcolumn:name="Accepted",type=string,JSONPath=`.status.conditions[?(@.type=="Accepted")].status`,description="DNSPolicy Accepted",priority=2
// +kubebuilder:printcolumn:name="Enforced",type=string,JSONPath=`.status.conditions[?(@.type=="Enforced")].status`,description="DNSPolicy Enforced",priority=2
// +kubebuilder:printcolumn:name="SubResourcesHealthy",type=string,JSONPath=`.status.conditions[?(@.type=="SubResourcesHealthy")].status`,description="DNSPolicy SubResourcesHealthy",priority=2
// +kubebuilder:printcolumn:name="TargetRefKind",type="string",JSONPath=".spec.targetRef.kind",description="Type of the referenced Gateway API resource",priority=2
// +kubebuilder:printcolumn:name="TargetRefName",type="string",JSONPath=".spec.targetRef.name",description="Name of the referenced Gateway API resource",priority=2
// +kubebuilder:printcolumn:name="TargetSection",type="string",JSONPath=".spec.targetRef.sectionName",description="Name of the Listener section within the Gateway to which the policy applies ",priority=2
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// DNSPolicy is the Schema for the dnspolicies API
type DNSPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DNSPolicySpec   `json:"spec,omitempty"`
	Status DNSPolicyStatus `json:"status,omitempty"`
}

var _ machinery.Policy = &DNSPolicy{}

func (p *DNSPolicy) GetTargetRefs() []machinery.PolicyTargetReference {
	return []machinery.PolicyTargetReference{
		machinery.LocalPolicyTargetReferenceWithSectionName{
			LocalPolicyTargetReferenceWithSectionName: p.Spec.TargetRef,
			PolicyNamespace: p.Namespace,
		},
	}
}

func (p *DNSPolicy) GetMergeStrategy() machinery.MergeStrategy {
	return func(policy machinery.Policy, _ machinery.Policy) machinery.Policy {
		return policy
	}
}

func (p *DNSPolicy) Merge(other machinery.Policy) machinery.Policy {
	return other
}

func (p *DNSPolicy) GetLocator() string {
	return machinery.LocatorFromObject(p)
}

func (p *DNSPolicy) Validate() error {
	return p.Spec.ExcludeAddresses.Validate()
}

// Deprecated: Use GetTargetRefs instead
func (p *DNSPolicy) GetTargetRef() gatewayapiv1alpha2.LocalPolicyTargetReference {
	return p.Spec.TargetRef.LocalPolicyTargetReference
}

func (p *DNSPolicy) GetStatus() kuadrantgatewayapi.PolicyStatus {
	return &p.Status
}

func (p *DNSPolicy) Kind() string {
	return DNSPolicyGroupKind.Kind
}

//+kubebuilder:object:root=true

// DNSPolicyList contains a list of DNSPolicy
type DNSPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DNSPolicy `json:"items"`
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

func (p *DNSPolicy) WithTargetRef(targetRef gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName) *DNSPolicy {
	p.Spec.TargetRef = targetRef
	return p
}

func (p *DNSPolicy) WithHealthCheck(healthCheck dnsv1alpha1.HealthCheckSpec) *DNSPolicy {
	p.Spec.HealthCheck = &healthCheck
	return p
}

func (p *DNSPolicy) WithLoadBalancing(loadBalancing LoadBalancingSpec) *DNSPolicy {
	p.Spec.LoadBalancing = &loadBalancing
	return p
}

func (p *DNSPolicy) WithProviderRef(providerRef dnsv1alpha1.ProviderRef) *DNSPolicy {
	p.Spec.ProviderRefs = append(p.Spec.ProviderRefs, providerRef)
	return p
}

//ProviderRef

func (p *DNSPolicy) WithProviderSecret(s corev1.Secret) *DNSPolicy {
	return p.WithProviderRef(dnsv1alpha1.ProviderRef{
		Name: s.Name,
	})
}

//excludeAddresses

func (p *DNSPolicy) WithExcludeAddresses(excluded []string) *DNSPolicy {
	p.Spec.ExcludeAddresses = excluded
	return p
}

//TargetRef

func (p *DNSPolicy) WithTargetGateway(gwName string) *DNSPolicy {
	return p.WithTargetRef(gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
		LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
			Group: gatewayapiv1.GroupName,
			Kind:  "Gateway",
			Name:  gatewayapiv1.ObjectName(gwName),
		},
		SectionName: nil,
	})
}

func (p *DNSPolicy) WithTargetGatewayListener(gwName string, lName string) *DNSPolicy {
	p.WithTargetGateway(gwName)
	p.Spec.TargetRef.SectionName = ptr.To(gatewayapiv1.SectionName(lName))
	return p
}

//HealthCheck

func (p *DNSPolicy) WithHealthCheckFor(endpoint string, port int, protocol string, failureThreshold int) *DNSPolicy {
	return p.WithHealthCheck(dnsv1alpha1.HealthCheckSpec{
		Path:             endpoint,
		Port:             port,
		Protocol:         dnsv1alpha1.Protocol(protocol),
		FailureThreshold: failureThreshold,
	})
}

//LoadBalancing

func (p *DNSPolicy) WithLoadBalancingFor(weight int, geo string, isDefaultGeo bool) *DNSPolicy {
	return p.WithLoadBalancing(LoadBalancingSpec{
		Weight:     weight,
		Geo:        geo,
		DefaultGeo: isDefaultGeo,
	})
}
