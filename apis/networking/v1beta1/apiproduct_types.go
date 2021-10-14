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

package v1beta1

import (
	"fmt"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/validation/field"

	"github.com/kuadrant/kuadrant-controller/pkg/common"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

const (
	APIProductKind = "APIProduct"
)

type OpenIDConnectAuth struct {
	URL string `json:"url"`
}

type APIKeyAuthCredentials struct {
	LabelSelectors map[string]string `json:"labelSelectors"`
}

type APIKeyAuth struct {
	Location         string                `json:"location"`
	Name             string                `json:"name"`
	CredentialSource APIKeyAuthCredentials `json:"credential_source"`
}

type SecurityScheme struct {
	Name              string             `json:"name"`
	APIKeyAuth        *APIKeyAuth        `json:"apiKeyAuth,omitempty"`
	OpenIDConnectAuth *OpenIDConnectAuth `json:"openIDConnectAuth,omitempty"`
}

type APIReference struct {
	// Kuadrant API object name
	Name string `json:"name"`

	// Kuadrant API object namespace
	Namespace string `json:"namespace"`

	// +optional
	Tag *string `json:"tag,omitempty"`

	// Public prefix path to be added to all paths exposed by the API
	// +optional
	Prefix *string `json:"prefix,omitempty"`
}

func (a *APIReference) APINamespacedName() types.NamespacedName {
	name := a.Name
	if a.Tag != nil {
		name = APIObjectName(a.Name, *a.Tag)
	}

	return types.NamespacedName{Namespace: a.Namespace, Name: name}
}

type RateLimitDefinitionSpec struct {
	// MaxValue represents the number of requests allowed per defined period of time.
	MaxValue int32 `json:"maxValue"`
	// Period represents the period of time in seconds.
	Period int32 `json:"period"`
}

type RateLimitSpec struct {
	// Global configures a single global rate limit for all requests.
	// +optional
	GlobalRateLimit *RateLimitDefinitionSpec `json:"global,omitempty"`

	// PerRemoteIPRateLimit configures the same rate limit parameters per each remote address
	// +optional
	PerRemoteIPRateLimit *RateLimitDefinitionSpec `json:"perRemoteIP,omitempty"`

	// AuthRateLimit configures the same rate limit parameters per each authenticated client
	// +optional
	AuthRateLimit *RateLimitDefinitionSpec `json:"authenticated,omitempty"`
}

// APIProductSpec defines the desired state of APIProduct
type APIProductSpec struct {
	// The destination hosts to which traffic is being sent. Could
	// be a DNS name with wildcard prefix or an IP address.  Depending on the
	// platform, short-names can also be used instead of a FQDN (i.e. has no
	// dots in the name). In such a scenario, the FQDN of the host would be
	// derived based on the underlying platform.
	Hosts []string `json:"hosts"`

	// Configure authentication mechanisms
	// +optional
	SecurityScheme []SecurityScheme `json:"securityScheme,omitempty"`

	// The list of kuadrant API to be protected
	// +kubebuilder:validation:MinItems=1
	APIs []APIReference `json:"APIs"`

	// RateLimit configures global rate limit parameters
	// +optional
	RateLimit *RateLimitSpec `json:"rateLimit,omitempty"`
}

// APIProductStatus defines the observed state of APIProduct
type APIProductStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// Conditions represent the latest available observations of an object's state
	// Known .status.conditions.type are: "Ready"
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`

	ObservedGen int64 `json:"observedgen"`
}

func (a *APIProductStatus) Equals(other *APIProductStatus, logger logr.Logger) bool {
	if a.ObservedGen != other.ObservedGen {
		diff := cmp.Diff(a.ObservedGen, other.ObservedGen)
		logger.V(1).Info("status.ObservedGen not equal", "difference", diff)
		return false
	}

	// Conditions
	currentMarshaledJSON, _ := common.StatusConditionsMarshalJSON(a.Conditions)
	otherMarshaledJSON, _ := common.StatusConditionsMarshalJSON(other.Conditions)
	if string(currentMarshaledJSON) != string(otherMarshaledJSON) {
		diff := cmp.Diff(string(currentMarshaledJSON), string(otherMarshaledJSON))
		logger.V(1).Info("Conditions not equal", "difference", diff)
		return false
	}

	return true
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// APIProduct is the Schema for the apiproducts API
type APIProduct struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   APIProductSpec   `json:"spec,omitempty"`
	Status APIProductStatus `json:"status,omitempty"`
}

func (a *APIProduct) Validate() error {
	fieldErrors := field.ErrorList{}
	apisFldPath := field.NewPath("spec").Child("APIs")

	// Look for duplicated mapping prefixes
	mappingPrefix := map[string]interface{}{}
	for idx := range a.Spec.APIs {
		apiRef := &a.Spec.APIs[idx]
		if apiRef.Prefix == nil {
			continue
		}

		apiField := apisFldPath.Index(idx)
		if _, ok := mappingPrefix[*apiRef.Prefix]; ok {
			fieldErrors = append(fieldErrors, field.Invalid(apiField, apiRef.APINamespacedName(), "duplicated prefix"))
		}

		mappingPrefix[*apiRef.Prefix] = nil
	}

	if len(fieldErrors) > 0 {
		return apierrors.NewInvalid(
			GroupVersion.WithKind(APIProductKind).GroupKind(),
			a.Name,
			fieldErrors,
		)
	}

	return nil
}

func (a *APIProduct) RateLimitDomainName() string {
	// APIProduct name/namespace should be unique in the cluster
	return fmt.Sprintf("%s.%s", a.Name, a.Namespace)
}

func (a *APIProduct) GlobalRateLimit() *RateLimitDefinitionSpec {
	if a.Spec.RateLimit == nil {
		return nil
	}

	return a.Spec.RateLimit.GlobalRateLimit
}

func (a *APIProduct) PerRemoteIPRateLimit() *RateLimitDefinitionSpec {
	if a.Spec.RateLimit == nil {
		return nil
	}

	return a.Spec.RateLimit.PerRemoteIPRateLimit
}

func (a *APIProduct) AuthRateLimit() *RateLimitDefinitionSpec {
	if a.Spec.RateLimit == nil {
		return nil
	}

	return a.Spec.RateLimit.AuthRateLimit
}

func (a *APIProduct) IsRateLimitEnabled() bool {
	return a.AuthRateLimit() != nil ||
		a.PerRemoteIPRateLimit() != nil ||
		a.GlobalRateLimit() != nil
}

func (a *APIProduct) IsPreAuthRateLimitEnabled() bool {
	return a.PerRemoteIPRateLimit() != nil ||
		a.GlobalRateLimit() != nil
}

func (a *APIProduct) HasSecurity() bool {
	return len(a.Spec.SecurityScheme) > 0
}

func (a *APIProduct) HasAPIKeyAuth() bool {
	for _, securityScheme := range a.Spec.SecurityScheme {
		if securityScheme.APIKeyAuth != nil {
			return true
		}
	}

	return false
}

func (a *APIProduct) HasOIDCAuth() bool {
	for _, securityScheme := range a.Spec.SecurityScheme {
		if securityScheme.OpenIDConnectAuth != nil {
			return true
		}
	}

	return false
}

//+kubebuilder:object:root=true

// APIProductList contains a list of APIProduct
type APIProductList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []APIProduct `json:"items"`
}

func init() {
	SchemeBuilder.Register(&APIProduct{}, &APIProductList{})
}
