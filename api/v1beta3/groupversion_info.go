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

// API schema definitions for the Kuadrant v1beta3 API group
// +kubebuilder:object:generate=true
// +groupName=kuadrant.io
package v1beta3

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime/pkg/scheme"
)

// GroupName specifies the group name used to register the objects.
const GroupName = "kuadrant.io"

// GroupVersion specifies the group and the version used to register the objects.
var GroupVersion = schema.GroupVersion{Group: GroupName, Version: "v1beta3"}

// SchemeGroupVersion is group version used to register these objects
// Deprecated: use GroupVersion instead.
var SchemeGroupVersion = schema.GroupVersion{Group: GroupName, Version: "v1beta3"}

// SchemeBuilder is used to add go types to the GroupVersionKind scheme
var SchemeBuilder = &ctrl.Builder{GroupVersion: GroupVersion}

// AddToScheme adds the types in this group-version to the given scheme.
var AddToScheme = SchemeBuilder.AddToScheme

// Resource takes an unqualified resource and returns a Group qualified GroupResource
func Resource(resource string) schema.GroupResource {
	return SchemeGroupVersion.WithResource(resource).GroupResource()
}
