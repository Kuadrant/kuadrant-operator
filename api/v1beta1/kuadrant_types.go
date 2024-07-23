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

package v1beta1

import (
	"fmt"

	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kuadrant/kuadrant-operator/pkg/library/reconcilers"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// KuadrantSpec defines the desired state of Kuadrant
type KuadrantSpec struct {
}

// KuadrantStatus defines the observed state of Kuadrant
type KuadrantStatus struct {
	reconcilers.StatusMeta `json:",inline"`

	// Represents the observations of a foo's current state.
	// Known .status.conditions.type are: "Available"
	// +patchMergeKey=type
	// +patchStrategy=merge
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty" patchStrategy:"merge" patchMergeKey:"type" protobuf:"bytes,1,rep,name=conditions"`
}

func KuadrantStatusMutator(desiredStatus *KuadrantStatus, logger logr.Logger) reconcilers.StatusMutatorFunc {
	return func(obj client.Object) (bool, error) {
		existingK, ok := obj.(*Kuadrant)
		if !ok {
			return false, fmt.Errorf("unsupported object type %T", obj)
		}

		opts := cmp.Options{
			cmpopts.IgnoreFields(metav1.Condition{}, "LastTransitionTime"),
			cmpopts.IgnoreMapEntries(func(k string, _ any) bool {
				return k == "lastTransitionTime"
			}),
		}

		if cmp.Equal(*desiredStatus, existingK.Status, opts) {
			return false, nil
		}

		if logger.V(1).Enabled() {
			diff := cmp.Diff(*desiredStatus, existingK.Status, opts)
			logger.V(1).Info("status not equal", "difference", diff)
		}

		existingK.Status = *desiredStatus

		return true, nil
	}
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="Status",type=string,JSONPath=`.status.conditions[0].reason`,priority=2
//+kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"

// Kuadrant configures installations of Kuadrant Service Protection components
type Kuadrant struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   KuadrantSpec   `json:"spec,omitempty"`
	Status KuadrantStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// KuadrantList contains a list of Kuadrant
type KuadrantList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Kuadrant `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Kuadrant{}, &KuadrantList{})
}
