/*
Copyright 2023.

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

	certmanv1 "github.com/jetstack/cert-manager/pkg/apis/certmanager/v1"
	certmanmetav1 "github.com/jetstack/cert-manager/pkg/apis/meta/v1"
	"github.com/kuadrant/kuadrant-operator/pkg/common"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

// TLSPolicySpec defines the desired state of TLSPolicy
type TLSPolicySpec struct {
	// +kubebuilder:validation:Required
	// +required
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

var _ common.KuadrantPolicy = &TLSPolicy{}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type==\"Ready\")].status",description="TLSPolicy ready."

// TLSPolicy is the Schema for the tlspolicies API
type TLSPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   TLSPolicySpec   `json:"spec,omitempty"`
	Status TLSPolicyStatus `json:"status,omitempty"`
}

func (p *TLSPolicy) GetWrappedNamespace() gatewayapiv1.Namespace {
	return gatewayapiv1.Namespace(p.Namespace)
}

func (p *TLSPolicy) GetRulesHostnames() []string {
	//TODO implement me
	panic("implement me")
}

func (p *TLSPolicy) GetTargetRef() gatewayapiv1alpha2.PolicyTargetReference {
	return p.Spec.TargetRef
}

func (p *TLSPolicy) Validate() error {
	if p.Spec.TargetRef.Group != ("gateway.networking.k8s.io") {
		return fmt.Errorf("invalid targetRef.Group %s. The only supported group is gateway.networking.k8s.io", p.Spec.TargetRef.Group)
	}

	if p.Spec.TargetRef.Kind != ("Gateway") {
		return fmt.Errorf("invalid targetRef.Kind %s. The only supported kind is Gateway", p.Spec.TargetRef.Kind)
	}

	if p.Spec.TargetRef.Namespace != nil && string(*p.Spec.TargetRef.Namespace) != p.Namespace {
		return fmt.Errorf("invalid targetRef.Namespace %s. Currently only supporting references to the same namespace", *p.Spec.TargetRef.Namespace)
	}

	return nil
}

func (r *TLSPolicy) Kind() string {
	return r.TypeMeta.Kind
}

//+kubebuilder:object:root=true

// TLSPolicyList contains a list of TLSPolicy
type TLSPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []TLSPolicy `json:"items"`
}

func init() {
	SchemeBuilder.Register(&TLSPolicy{}, &TLSPolicyList{})
}
