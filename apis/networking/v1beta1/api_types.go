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

	apiextentionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	gatewayapiv1alpha1 "sigs.k8s.io/gateway-api/apis/v1alpha1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// TODO: API definition is missing kubebuilder annotations for validation, add them.
// TODO: Add proper comments for each of the API fields and Structs, so we can create proper docs.

const (
	APIKind = "API"
)

type Destination struct {
	Schema                           string `json:"schema,omitempty"`
	apiextentionsv1.ServiceReference `json:"serviceReference"`
}

func (d Destination) NamespacedName() types.NamespacedName {
	return types.NamespacedName{Namespace: d.Namespace, Name: d.Name}
}

type APIMappings struct {
	// Inline OAS
	// +optional
	OAS *string `json:"OAS,omitempty"`

	// Select a HTTP route by matching the HTTP request path.
	// +optional
	HTTPPathMatch *gatewayapiv1alpha1.HTTPPathMatch `json:"HTTPPathMatch,omitempty"`
}

// APISpec defines the desired state of API
type APISpec struct {
	Destination Destination `json:"destination"`
	Mappings    APIMappings `json:"mappings"`
}

// APIStatus defines the observed state of API
type APIStatus struct {
	Ready              bool  `json:"ready"`
	ObservedGeneration int64 `json:"observedGeneration"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// API is the Schema for the apis API
type API struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   APISpec   `json:"spec,omitempty"`
	Status APIStatus `json:"status,omitempty"`
}

func APIObjectName(base, tag string) string {
	return fmt.Sprintf("%s.%s", base, tag)
}

// +kubebuilder:object:root=true

// APIList contains a list of API
type APIList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []API `json:"items"`
}

func init() {
	SchemeBuilder.Register(&API{}, &APIList{})
}
