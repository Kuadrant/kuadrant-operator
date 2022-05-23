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

package common

import (
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	DeleteTagAnnotation = "kuadrant.io/delete"
)

func ObjectInfo(obj client.Object) string {
	return fmt.Sprintf("%s/%s", obj.GetObjectKind().GroupVersionKind().Kind, obj.GetName())
}

func TagObjectToDelete(obj client.Object) {
	// Add custom annotation
	annotations := obj.GetAnnotations()
	if annotations == nil {
		annotations = make(map[string]string)
		obj.SetAnnotations(annotations)
	}
	annotations[DeleteTagAnnotation] = "true"
}

func IsObjectTaggedToDelete(obj client.Object) bool {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		return false
	}

	annotation, ok := annotations[DeleteTagAnnotation]
	return ok && annotation == "true"
}

func IsOwnedBy(owned, owner client.Object) bool {
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

func FindDeploymentStatusCondition(conditions []appsv1.DeploymentCondition, conditionType string) *appsv1.DeploymentCondition {
	for i := range conditions {
		if conditions[i].Type == appsv1.DeploymentConditionType(conditionType) {
			return &conditions[i]
		}
	}

	return nil
}
