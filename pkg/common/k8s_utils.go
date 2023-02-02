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
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	DeleteTagAnnotation      = "kuadrant.io/delete"
	ReadyStatusConditionType = "Ready"
)

func ObjectInfo(obj client.Object) string {
	return fmt.Sprintf("%s/%s", obj.GetObjectKind().GroupVersionKind().Kind, obj.GetName())
}

func ReadAnnotationsFromObject(obj client.Object) map[string]string {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	return annotations
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

// StatusConditionsMarshalJSON marshals the list of conditions as a JSON array, sorted by
// condition type.
func StatusConditionsMarshalJSON(input []metav1.Condition) ([]byte, error) {
	conds := make([]metav1.Condition, 0, len(input))
	for idx := range input {
		conds = append(conds, input[idx])
	}

	sort.Slice(conds, func(a, b int) bool {
		return conds[a].Type < conds[b].Type
	})

	return json.Marshal(conds)
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

// GetServicePortNumber returns the port number from the referenced key and port info
// the port info can be named port or already a number.
func GetServicePortNumber(ctx context.Context, k8sClient client.Client, serviceKey client.ObjectKey, servicePort string) (int32, error) {
	// check if the port is a number already.
	if num, err := strconv.ParseInt(servicePort, 10, 32); err == nil {
		return int32(num), nil
	}

	// As the port is name, resolv the port from the service
	service, err := GetService(ctx, k8sClient, serviceKey)
	if err != nil {
		// the service must exist
		return 0, err
	}

	for _, p := range service.Spec.Ports {
		if p.Name == servicePort {
			return int32(p.TargetPort.IntValue()), nil
		}
	}

	return 0, fmt.Errorf("service port %s was not found in %s", servicePort, serviceKey.String())
}

func GetServiceWorkloadSelector(ctx context.Context, k8sClient client.Client, serviceKey client.ObjectKey) (map[string]string, error) {
	service, err := GetService(ctx, k8sClient, serviceKey)
	if err != nil {
		return nil, err
	}
	return service.Spec.Selector, nil
}

func GetService(ctx context.Context, k8sClient client.Client, serviceKey client.ObjectKey) (*corev1.Service, error) {
	service := &corev1.Service{}
	if err := k8sClient.Get(ctx, serviceKey, service); err != nil {
		return nil, err
	}
	return service, nil
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

// ContainsObjectKey tells whether a contains x
func ContainsObjectKey(a []client.ObjectKey, x client.ObjectKey) bool {
	for _, n := range a {
		if x == n {
			return true
		}
	}
	return false
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
