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

	certmanv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	certmanmetav1 "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
)

const (
	TLSPolicyBackReferenceAnnotationName   = "kuadrant.io/tlspolicies"
	TLSPolicyDirectReferenceAnnotationName = "kuadrant.io/tlspolicy"
)

// TLSPolicySpec defines the desired state of TLSPolicy
type TLSPolicySpec struct {
	// TargetRef identifies an API object to apply policy to.
	// +kubebuilder:validation:XValidation:rule="self.group == 'gateway.networking.k8s.io'",message="Invalid targetRef.group. The only supported value is 'gateway.networking.k8s.io'"
	// +kubebuilder:validation:XValidation:rule="self.kind == 'Gateway'",message="Invalid targetRef.kind. The only supported values are 'Gateway'"
	TargetRef gatewayapiv1alpha2.PolicyTargetReference `json:"targetRef"`

	CertificateSpec `json:",inline"`
}

// CertificateSpec defines the certificate manager certificate spec that can be set via the TLSPolicy.
// Rather than allowing the whole certmanv1.CertificateSpec to be inlined we are only including the same fields that are
// currently supported by the annotation approach to securing gateways as outlined here https://cert-manager.io/docs/usage/gateway/#supported-annotations
type CertificateSpec struct {
	// IssuerRef is a reference to the issuer for this certificate.
	// If the `kind` field is not set, or set to `Issuer`, an Issuer resource
	// with the given name in the same namespace as the Certificate will be used.
	// If the `kind` field is set to `ClusterIssuer`, a ClusterIssuer with the
	// provided name will be used.
	// The `name` field in this stanza is required at all times.
	IssuerRef certmanmetav1.ObjectReference `json:"issuerRef"`

	// CommonName is a common name to be used on the Certificate.
	// The CommonName should have a length of 64 characters or fewer to avoid
	// generating invalid CSRs.
	// This value is ignored by TLS clients when any subject alt name is set.
	// This is x509 behaviour: https://tools.ietf.org/html/rfc6125#section-6.4.4
	// +optional
	CommonName string `json:"commonName,omitempty"`

	// The requested 'duration' (i.e. lifetime) of the Certificate. This option
	// may be ignored/overridden by some issuer types. If unset this defaults to
	// 90 days. Certificate will be renewed either 2/3 through its duration or
	// `renewBefore` period before its expiry, whichever is later. Minimum
	// accepted duration is 1 hour. Value must be in units accepted by Go
	// time.ParseDuration https://golang.org/pkg/time/#ParseDuration
	// +optional
	Duration *metav1.Duration `json:"duration,omitempty"`

	// How long before the currently issued certificate's expiry
	// cert-manager should renew the certificate. The default is 2/3 of the
	// issued certificate's duration. Minimum accepted value is 5 minutes.
	// Value must be in units accepted by Go time.ParseDuration
	// https://golang.org/pkg/time/#ParseDuration
	// +optional
	RenewBefore *metav1.Duration `json:"renewBefore,omitempty"`

	// Usages is the set of x509 usages that are requested for the certificate.
	// Defaults to `digital signature` and `key encipherment` if not specified.
	// +optional
	Usages []certmanv1.KeyUsage `json:"usages,omitempty"`

	// RevisionHistoryLimit is the maximum number of CertificateRequest revisions
	// that are maintained in the Certificate's history. Each revision represents
	// a single `CertificateRequest` created by this Certificate, either when it
	// was created, renewed, or Spec was changed. Revisions will be removed by
	// oldest first if the number of revisions exceeds this number. If set,
	// revisionHistoryLimit must be a value of `1` or greater. If unset (`nil`),
	// revisions will not be garbage collected. Default value is `nil`.
	// +kubebuilder:validation:ExclusiveMaximum=false
	// +optional
	RevisionHistoryLimit *int32 `json:"revisionHistoryLimit,omitempty"`

	// Options to control private keys used for the Certificate.
	// +optional
	PrivateKey *certmanv1.CertificatePrivateKey `json:"privateKey,omitempty"`
}

// TLSPolicyStatus defines the observed state of TLSPolicy
type TLSPolicyStatus struct {
	// conditions are any conditions associated with the policy
	//
	// If configuring the policy fails, the "Failed" condition will be set with a
	// reason and message describing the cause of the failure.
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// observedGeneration is the most recently observed generation of the
	// TLSPolicy.  When the TLSPolicy is updated, the controller updates the
	// corresponding configuration. If an update fails, that failure is
	// recorded in the status condition
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

func (s *TLSPolicyStatus) GetConditions() []metav1.Condition {
	return s.Conditions
}

var _ kuadrant.Policy = &TLSPolicy{}
var _ kuadrant.Referrer = &TLSPolicy{}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:metadata:labels="gateway.networking.k8s.io/policy=direct"
// +kubebuilder:printcolumn:name="Accepted",type=string,JSONPath=`.status.conditions[?(@.type=="Accepted")].status`,description="TLSPolicy Accepted",priority=2
// +kubebuilder:printcolumn:name="Enforced",type=string,JSONPath=`.status.conditions[?(@.type=="Enforced")].status`,description="TLSPolicy Enforced",priority=2
// +kubebuilder:printcolumn:name="TargetRefKind",type="string",JSONPath=".spec.targetRef.kind",description="Type of the referenced Gateway API resource",priority=2
// +kubebuilder:printcolumn:name="TargetRefName",type="string",JSONPath=".spec.targetRef.name",description="Name of the referenced Gateway API resource",priority=2
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// TLSPolicy is the Schema for the tlspolicies API
type TLSPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TLSPolicySpec   `json:"spec,omitempty"`
	Status TLSPolicyStatus `json:"status,omitempty"`
}

func (p *TLSPolicy) Kind() string { return p.TypeMeta.Kind }

func (p *TLSPolicy) List(ctx context.Context, c client.Client, namespace string) []kuadrantgatewayapi.Policy {
	policyList := &TLSPolicyList{}
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

func (p *TLSPolicy) PolicyClass() kuadrantgatewayapi.PolicyClass {
	return kuadrantgatewayapi.DirectPolicy
}

func (p *TLSPolicy) GetWrappedNamespace() gatewayapiv1.Namespace {
	return gatewayapiv1.Namespace(p.Namespace)
}

func (p *TLSPolicy) GetRulesHostnames() []string {
	return make([]string, 0)
}

func (p *TLSPolicy) GetTargetRef() gatewayapiv1alpha2.PolicyTargetReference {
	return p.Spec.TargetRef
}

func (p *TLSPolicy) GetStatus() kuadrantgatewayapi.PolicyStatus {
	return &p.Status
}

func (p *TLSPolicy) BackReferenceAnnotationName() string {
	return TLSPolicyBackReferenceAnnotationName
}

func (p *TLSPolicy) DirectReferenceAnnotationName() string {
	return TLSPolicyDirectReferenceAnnotationName
}

func (p *TLSPolicy) Validate() error {
	if p.Spec.TargetRef.Group != (gatewayapiv1.GroupName) {
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

// TLSPolicyList contains a list of TLSPolicy
type TLSPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TLSPolicy `json:"items"`
}

func (l *TLSPolicyList) GetItems() []kuadrant.Policy {
	return utils.Map(l.Items, func(item TLSPolicy) kuadrant.Policy {
		return &item
	})
}

func init() {
	SchemeBuilder.Register(&TLSPolicy{}, &TLSPolicyList{})
}

//API Helpers

func NewTLSPolicy(policyName, ns string) *TLSPolicy {
	return &TLSPolicy{
		TypeMeta: metav1.TypeMeta{
			Kind:       "TLSPolicy",
			APIVersion: GroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      policyName,
			Namespace: ns,
		},
		Spec: TLSPolicySpec{},
	}
}

func (p *TLSPolicy) WithTargetGateway(gwName string) *TLSPolicy {
	typedNamespace := gatewayapiv1.Namespace(p.GetNamespace())
	p.Spec.TargetRef = gatewayapiv1alpha2.PolicyTargetReference{
		Group:     gatewayapiv1.GroupName,
		Kind:      "Gateway",
		Name:      gatewayapiv1.ObjectName(gwName),
		Namespace: &typedNamespace,
	}
	return p
}

func (p *TLSPolicy) WithIssuerRef(issuerRef certmanmetav1.ObjectReference) *TLSPolicy {
	p.Spec.IssuerRef = issuerRef
	return p
}
