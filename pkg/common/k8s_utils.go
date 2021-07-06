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
	"encoding/json"
	"fmt"
	"sort"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	DeleteTagAnnotation      = "kuadrant.io/delete"
	ReadyStatusConditionType = "Ready"
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

// StatusConditionsMarshalJSON marshals the list of conditions as a JSON array, sorted by
// condition type.
func StatusConditionsMarshalJSON(input []metav1.Condition) ([]byte, error) {
	conds := make([]metav1.Condition, len(input))
	for idx := range input {
		conds = append(conds, input[idx])
	}

	sort.Slice(conds, func(a, b int) bool {
		return conds[a].Type < conds[b].Type
	})

	return json.Marshal(conds)
}
