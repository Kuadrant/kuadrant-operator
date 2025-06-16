/*
Copyright 2021.

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
	"net/url"
	"path"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	"github.com/kuadrant/limitador-operator/pkg/helpers"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

const (
	CallbackPath      = "/auth/callback"
	TokenExchangePath = "/oauth/token" //nolint:gosec
	AuthorizePath     = "/oauth/authorize"

	StatusConditionReady string = "Ready"
)

// OIDCPolicySpec defines the desired state of OIDCPolicy
type OIDCPolicySpec struct {
	// Reference to the object to which this policy applies.
	// +kubebuilder:validation:XValidation:rule="self.group == 'gateway.networking.k8s.io'",message="Invalid targetRef.group. The only supported value is 'gateway.networking.k8s.io'"
	// +kubebuilder:validation:XValidation:rule="self.kind == 'HTTPRoute' || self.kind == 'Gateway'",message="Invalid targetRef.kind. The only supported values are 'HTTPRoute' and 'Gateway'"
	TargetRef gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName `json:"targetRef"`

	// Bare set of policy rules (implicit defaults).
	OIDCPolicySpecProper `json:""`
}

// OIDCPolicySpecProper contains common shared fields for the future inclusion of defaults and overrides
type OIDCPolicySpecProper struct {
	// Provider holds the information for the OIDC provider
	Provider *Provider `json:"provider,omitempty"`
	// Claims contains the JWT Claims https://www.rfc-editor.org/rfc/rfc7519.html#section-4
	Claims map[string]string `json:"claims,omitempty"`
}

type Provider struct {
	IssuerURL string `json:"issuerURL"`
	ClientID  string `json:"clientID"`
}

// OIDCPolicyStatus defines the observed state of OIDCPolicy
type OIDCPolicyStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// ObservedGeneration reflects the generation of the most recently observed spec.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Represents the observations of a foo's current state.
	// Known .status.conditions.type are: "Ready"
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// OIDCPolicy is the Schema for the oidcpolicies API
type OIDCPolicy struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   OIDCPolicySpec   `json:"spec,omitempty"`
	Status OIDCPolicyStatus `json:"status,omitempty"`
}

func (p *OIDCPolicy) GetTargetRefs() []gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName {
	return []gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName{
		{
			LocalPolicyTargetReference: gatewayapiv1alpha2.LocalPolicyTargetReference{
				Group: p.Spec.TargetRef.Group,
				Kind:  p.Spec.TargetRef.Kind,
				Name:  p.Spec.TargetRef.Name,
			},
		},
	}
}

func (p *OIDCPolicy) GetRedirectURL(igwURL *url.URL) string {
	redirectURL := *igwURL
	redirectURL.Path = path.Join(redirectURL.Path, CallbackPath)
	return redirectURL.String()
}

func (p *OIDCPolicy) GetIssuerTokenExchangeURL() string {
	u, err := url.Parse(p.Spec.Provider.IssuerURL)
	if err != nil {
		panic(err)
	}
	u.Path = path.Join(u.Path, TokenExchangePath)
	return u.String()
}

func (p *OIDCPolicy) GetAuthorizeURL(igwURL *url.URL) string {
	authorizeURL, err := url.Parse(p.Spec.Provider.IssuerURL)
	authorizeURL.Path = AuthorizePath
	if err != nil {
		panic(err)
	}
	redirectURL := *igwURL
	redirectURL.Path = path.Join(redirectURL.Path, CallbackPath)

	query := url.Values{}
	query.Set("client_id", p.Spec.Provider.ClientID)
	query.Set("redirect_uri", redirectURL.String())
	query.Set("response_type", "code")
	query.Set("scope", "openid")
	authorizeURL.RawQuery = query.Encode()

	return authorizeURL.String()
}

func (p *OIDCPolicy) GetClaims() map[string]string {
	claims := make(map[string]string, len(p.Spec.Claims))
	for k, v := range p.Spec.Claims {
		claims[k] = v
	}
	return claims
}

// +kubebuilder:object:root=true

// OIDCPolicyList contains a list of OIDCPolicy
type OIDCPolicyList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []OIDCPolicy `json:"items"`
}

func (s *OIDCPolicyStatus) Equals(other *OIDCPolicyStatus, logger logr.Logger) bool {
	if s.ObservedGeneration != other.ObservedGeneration {
		diff := cmp.Diff(s.ObservedGeneration, other.ObservedGeneration)
		logger.Info("status observedGeneration not equal", "difference", diff)
		return false
	}

	// Marshalling sorts by condition type
	currentMarshaledJSON, _ := helpers.ConditionMarshal(s.Conditions)
	otherMarshaledJSON, _ := helpers.ConditionMarshal(other.Conditions)
	if string(currentMarshaledJSON) != string(otherMarshaledJSON) {
		diff := cmp.Diff(string(currentMarshaledJSON), string(otherMarshaledJSON))
		logger.Info("status conditions not equal", "difference", diff)
		return false
	}

	return true
}

func init() {
	SchemeBuilder.Register(&OIDCPolicy{}, &OIDCPolicyList{})
}
