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

package v1alpha1

import (
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type OPARef struct {
	URL       string                   `json:"URL,omitempty"`
	ConfigMap *v1.LocalObjectReference `json:"configMap,omitempty"`
}

type APIMetadata struct {
	Version     string `json:"version"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	OpenAPIRef  OPARef `json:"openAPIRef,omitempty"`
}

type APISpec struct {
	Info APIMetadata `json:"info"`
}

// +kubebuilder:object:root=true
// API is the Schema for the apis API
type API struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec APISpec `json:"spec"`
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
