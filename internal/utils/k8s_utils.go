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

package utils

import (
	"context"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	DeleteTagAnnotation = "kuadrant.io/delete"
	ClusterIDLength     = 6
	clusterIDNamespace  = "kube-system"
)

var clusterUID string

// TagObjectToDelete adds a special DeleteTagAnnotation to the object's annotations.
// If the object's annotations are nil, it first initializes the Annotations field with an empty map.
func TagObjectToDelete(obj client.Object) {
	// Add custom annotation
	annotations := obj.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
		obj.SetAnnotations(annotations)
	}
	annotations[DeleteTagAnnotation] = "true"
}

// IsObjectTaggedToDelete checks if the given object is tagged for deletion.
// It looks for the DeleteTagAnnotation in the object's annotations
// and returns true if the annotation value is set to "true", false otherwise.
func IsObjectTaggedToDelete(obj client.Object) bool {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		return false
	}

	annotation, ok := annotations[DeleteTagAnnotation]
	return ok && annotation == "true"
}

// IsOwnedBy checks if the provided owned object is owned by the given owner object.
// Ownership is determined based on matching the owner reference's group, kind, and name.
// The version of the owner reference is not checked in this implementation.
// Returns true if the owned object is owned by the owner object, false otherwise.
func IsOwnedBy(owned, owner client.Object) bool {
	if owned.GetNamespace() != owner.GetNamespace() {
		return false
	}

	ownerGVK := owner.GetObjectKind().GroupVersionKind()

	for _, o := range owned.GetOwnerReferences() {
		oGV, err := schema.ParseGroupVersion(o.APIVersion)
		if err != nil {
			return false
		}

		// Version needs to be checked???
		if oGV.Group == ownerGVK.Group && o.Kind == ownerGVK.Kind && owner.GetName() == o.Name {
			return true
		}
	}

	return false
}

func GetClusterUID(ctx context.Context, c dynamic.Interface) (string, error) {
	//Already calculated? return it
	if clusterUID != "" {
		return clusterUID, nil
	}

	un, err := c.Resource(corev1.SchemeGroupVersion.WithResource("namespaces")).Get(ctx, clusterIDNamespace, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	clusterUID = string(un.GetUID())
	return clusterUID, nil
}
