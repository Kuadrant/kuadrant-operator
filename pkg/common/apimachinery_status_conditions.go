package common

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ConditionMarshal marshals the set of conditions as a JSON array, sorted by condition type.
func ConditionMarshal(conditions []metav1.Condition) ([]byte, error) {
	condCopy := slices.Clone(conditions)
	sort.Slice(condCopy, func(a, b int) bool {
		return condCopy[a].Type < condCopy[b].Type
	})
	return json.Marshal(condCopy)
}
