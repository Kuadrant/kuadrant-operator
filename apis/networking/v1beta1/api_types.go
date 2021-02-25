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

	v12 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// TODO: API definition is missing kubebuilder annotations for validation, add them.
// TODO: Add proper comments for each of the API fields and Structs, so we can create proper docs.

// ApiSpec defines the desired state of Api
type ApiSpec struct {
	Hosts          []string          `json:"hosts"`
	Operations     []*Operation      `json:"operations"`
	BackendServer  []*BackendServer  `json:"backendServers"`
	SecurityScheme []*SecurityScheme `json:"securityScheme,omitempty"`
}

type Operation struct {
	Name              string                `json:"name"`
	ID                string                `json:"id"`
	Path              string                `json:"path"`
	Method            string                `json:"method"`
	Security          []*SecurityParameters `json:"security,omitempty"`
	BackendServerName string                `json:"backendServerName"`
}

type SecurityParameters struct {
	ApiKeyAuth []string `json:"apiKeyAuth,omitempty"`
	Oauth2     []string `json:"Oauth2,omitempty"`
}

type BackendServer struct {
	Name       string               `json:"name"`
	ServiceRef v12.ServiceReference `json:"serviceRef"`
}

type SecurityScheme struct {
	Name              string             `json:"name"`
	APIKeyAuth        *APIKeyAuth        `json:"apiKeyAuth,omitempty"`
	OpenIDConnectAuth *OpenIDConnectAuth `json:"openIDConnectAuth,omitempty"`
}

type APIKeyAuth struct {
	Location       string               `json:"location"`
	Name           string               `json:"name"`
	APIKeySelector metav1.LabelSelector `json:"APIKeySelector"`
}

type OpenIDConnectAuth struct {
	URL string `json:"url"`
}

// ApiStatus defines the observed state of Api
type ApiStatus struct {
	Ready              bool  `json:"ready"`
	ObservedGeneration int64 `json:"observedGeneration"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// Api is the Schema for the apis API
type Api struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ApiSpec   `json:"spec,omitempty"`
	Status ApiStatus `json:"status,omitempty"`
}

func (api *Api) GetName() string {
	return fmt.Sprintf("%s/%s", api.Namespace, api.Name)
}

// +kubebuilder:object:root=true

// ApiList contains a list of Api
type ApiList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Api `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Api{}, &ApiList{})
}

func (api Api) GetHosts() []string {
	return api.Spec.Hosts
}
