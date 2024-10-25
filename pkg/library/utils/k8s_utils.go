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
	"encoding/json"
	"fmt"
	"sort"

	appsv1 "k8s.io/api/apps/v1"
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

// ObjectInfo generates a string representation of the provided Kubernetes object, including its kind and name.
// The generated string follows the format: "kind/name".
func ObjectInfo(obj client.Object) string {
	return fmt.Sprintf("%s/%s", obj.GetObjectKind().GroupVersionKind().Kind, obj.GetName())
}

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

// StatusConditionsMarshalJSON marshals the list of conditions as a JSON array, sorted by
// condition type.
func StatusConditionsMarshalJSON(input []metav1.Condition) ([]byte, error) {
	conds := make([]metav1.Condition, 0, len(input))
	conds = append(conds, input...)

	sort.Slice(conds, func(a, b int) bool {
		return conds[a].Type < conds[b].Type
	})

	return json.Marshal(conds)
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

// ObjectKeyListDifference computest a - b
func ObjectKeyListDifference(a, b []client.ObjectKey) []client.ObjectKey {
	target := map[client.ObjectKey]bool{}
	for _, x := range b {
		target[x] = true
	}

	result := make([]client.ObjectKey, 0)
	for _, x := range a {
		if _, ok := target[x]; !ok {
			result = append(result, x)
		}
	}

	return result
}

// FindObjectKey returns the smallest index i at which x == a[i],
// or len(a) if there is no such index.
func FindObjectKey(a []client.ObjectKey, x client.ObjectKey) int {
	for i, n := range a {
		if x == n {
			return i
		}
	}
	return len(a)
}

func FindDeploymentStatusCondition(conditions []appsv1.DeploymentCondition, conditionType string) *appsv1.DeploymentCondition {
	for i := range conditions {
		if conditions[i].Type == appsv1.DeploymentConditionType(conditionType) {
			return &conditions[i]
		}
	}

	return nil
}

func HasLabel(obj metav1.Object, key string) bool {
	labels := obj.GetLabels()
	_, ok := labels[key]
	return ok
}

func GetLabel(obj metav1.Object, key string) string {
	if !HasLabel(obj, key) {
		return ""
	}
	return obj.GetLabels()[key]
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
