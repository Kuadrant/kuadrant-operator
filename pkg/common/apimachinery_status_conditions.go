package common

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

type ErrTargetNotFound struct {
	Kind      string
	TargetRef gatewayapiv1alpha2.PolicyTargetReference
}

func (e ErrTargetNotFound) Error() string {
	return fmt.Sprintf("%s target %s was not found", e.Kind, e.TargetRef.Name)
}

type ErrInvalid struct {
	Kind string
	Err  error
}

func (e ErrInvalid) Error() string {
	return fmt.Sprintf("%s target is invalid: %s", e.Kind, e.Err.Error())
}

// ConditionMarshal marshals the set of conditions as a JSON array, sorted by condition type.
func ConditionMarshal(conditions []metav1.Condition) ([]byte, error) {
	condCopy := slices.Clone(conditions)
	sort.Slice(condCopy, func(a, b int) bool {
		return condCopy[a].Type < condCopy[b].Type
	})
	return json.Marshal(condCopy)
}
