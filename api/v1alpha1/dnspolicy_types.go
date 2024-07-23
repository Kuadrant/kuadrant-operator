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

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	"k8s.io/apimachinery/pkg/api/equality"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	dnsv1alpha1 "github.com/kuadrant/dns-operator/api/v1alpha1"

	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
	"github.com/kuadrant/kuadrant-operator/pkg/library/reconcilers"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
)

var (
	DNSPolicyGVK schema.GroupVersionKind = schema.GroupVersionKind{
		Group:   GroupVersion.Group,
		Version: GroupVersion.Version,
		Kind:    "DNSPolicy",
	}
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
	// targetRef identifies an API object to apply policy to.
	// +kubebuilder:validation:XValidation:rule="self.group == 'gateway.networking.k8s.io'",message="Invalid targetRef.group. The only supported value is 'gateway.networking.k8s.io'"
	// +kubebuilder:validation:XValidation:rule="self.kind == 'Gateway'",message="Invalid targetRef.kind. The only supported values are 'Gateway'"
	TargetRef gatewayapiv1alpha2.LocalPolicyTargetReference `json:"targetRef"`

	// +optional
	HealthCheck *dnsv1alpha1.HealthCheckSpec `json:"healthCheck,omitempty"`

	// +optional
	LoadBalancing *LoadBalancingSpec `json:"loadBalancing,omitempty"`

	// +kubebuilder:validation:Enum=simple;loadbalanced
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="RoutingStrategy is immutable"
	// +kubebuilder:default=loadbalanced
	RoutingStrategy RoutingStrategy `json:"routingStrategy"`

	// providerRefs is a list of references to provider secrets. Max is one but intention is to allow this to be more in the future
	// +kubebuilder:validation:MaxItems=1
	// +kubebuilder:validation:MinItems=1
	ProviderRefs []dnsv1alpha1.ProviderRef `json:"providerRefs"`
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
	reconcilers.StatusMeta `json:",inline"`

	// conditions are any conditions associated with the policy
	//
	// If configuring the policy fails, the "Failed" condition will be set with a
	// reason and message describing the cause of the failure.
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// +optional
	HealthCheck *dnsv1alpha1.HealthCheckStatus `json:"healthCheck,omitempty"`

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

func (p *DNSPolicy) GetTargetRef() gatewayapiv1alpha2.LocalPolicyTargetReference {
	return p.Spec.TargetRef
}

func (p *DNSPolicy) GetStatus() kuadrantgatewayapi.PolicyStatus {
	return &p.Status
}

func (p *DNSPolicy) Kind() string {
	return NewDNSPolicyType().GetGVK().Kind
}

func (p *DNSPolicy) TargetProgrammedGatewaysOnly() bool {
	return true
}

func (p *DNSPolicy) PolicyClass() kuadrantgatewayapi.PolicyClass {
	return kuadrantgatewayapi.DirectPolicy
}

func (p *DNSPolicy) BackReferenceAnnotationName() string {
	return NewDNSPolicyType().BackReferenceAnnotationName()
}

func (p *DNSPolicy) DirectReferenceAnnotationName() string {
	return NewDNSPolicyType().DirectReferenceAnnotationName()
}

func DNSPolicyStatusMutator(desiredStatus *DNSPolicyStatus, logger logr.Logger) reconcilers.StatusMutatorFunc {
	return func(obj client.Object) (bool, error) {
		existing, ok := obj.(*DNSPolicy)
		if !ok {
			return false, fmt.Errorf("unsupported object type %T", obj)
		}

		if equality.Semantic.DeepEqual(*desiredStatus, existing.Status) {
			return false, nil
		}

		if logger.V(1).Enabled() {
			diff := cmp.Diff(*desiredStatus, existing.Status)
			logger.V(1).Info("status not equal", "difference", diff)
		}

		existing.Status = *desiredStatus

		return true, nil
	}
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

func (p *DNSPolicy) WithTargetRef(targetRef gatewayapiv1alpha2.LocalPolicyTargetReference) *DNSPolicy {
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

func (p *DNSPolicy) WithRoutingStrategy(strategy RoutingStrategy) *DNSPolicy {
	p.Spec.RoutingStrategy = strategy
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

//TargetRef

func (p *DNSPolicy) WithTargetGateway(gwName string) *DNSPolicy {
	return p.WithTargetRef(gatewayapiv1alpha2.LocalPolicyTargetReference{
		Group: gatewayapiv1.GroupName,
		Kind:  "Gateway",
		Name:  gatewayapiv1.ObjectName(gwName),
	})
}

//HealthCheck

func (p *DNSPolicy) WithHealthCheckFor(endpoint string, port int, protocol string, failureThreshold int) *DNSPolicy {
	return p.WithHealthCheck(dnsv1alpha1.HealthCheckSpec{
		Endpoint:         endpoint,
		Port:             &port,
		Protocol:         ptr.To(dnsv1alpha1.HealthProtocol(protocol)),
		FailureThreshold: &failureThreshold,
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

type dnsPolicyType struct{}

func NewDNSPolicyType() kuadrantgatewayapi.PolicyType {
	return &dnsPolicyType{}
}

func (d dnsPolicyType) GetGVK() schema.GroupVersionKind {
	return DNSPolicyGVK
}

func (d dnsPolicyType) GetInstance() client.Object {
	return &DNSPolicy{
		TypeMeta: metav1.TypeMeta{
			Kind:       DNSPolicyGVK.Kind,
			APIVersion: GroupVersion.String(),
		},
	}
}

func (d dnsPolicyType) GetList(ctx context.Context, cl client.Client, listOpts ...client.ListOption) ([]kuadrantgatewayapi.Policy, error) {
	list := &DNSPolicyList{}
	err := cl.List(ctx, list, listOpts...)
	if err != nil {
		return nil, err
	}
	return utils.Map(list.Items, func(p DNSPolicy) kuadrantgatewayapi.Policy { return &p }), nil
}

func (d dnsPolicyType) BackReferenceAnnotationName() string {
	return DNSPolicyBackReferenceAnnotationName
}

func (d dnsPolicyType) DirectReferenceAnnotationName() string {
	return DNSPolicyDirectReferenceAnnotationName
}
