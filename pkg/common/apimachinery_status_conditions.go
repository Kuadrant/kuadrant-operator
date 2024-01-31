package common

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

const (
	PolicyConditionEnforced gatewayapiv1alpha2.PolicyConditionType = "Enforced"

	PolicyReasonEnforced gatewayapiv1alpha2.PolicyConditionReason = "Enforced"
	PolicyReasonUnknown  gatewayapiv1alpha2.PolicyConditionReason = "Unknown"
)

// ConditionMarshal marshals the set of conditions as a JSON array, sorted by condition type.
func ConditionMarshal(conditions []metav1.Condition) ([]byte, error) {
	condCopy := slices.Clone(conditions)
	sort.Slice(condCopy, func(a, b int) bool {
		return condCopy[a].Type < condCopy[b].Type
	})
	return json.Marshal(condCopy)
}

// AcceptedCondition returns an accepted conditions with common reasons for a kuadrant policy
func AcceptedCondition(policy KuadrantPolicy, err error) *metav1.Condition {
	// Accepted
	cond := &metav1.Condition{
		Type:    string(gatewayapiv1alpha2.PolicyConditionAccepted),
		Status:  metav1.ConditionTrue,
		Reason:  string(gatewayapiv1alpha2.PolicyReasonAccepted),
		Message: fmt.Sprintf("%s has been accepted", policy.Kind()),
	}
	if err == nil {
		return cond
	}

	// Wrap error into a PolicyError if it is not this type
	var policyErr PolicyError
	if !errors.As(err, &policyErr) {
		policyErr = NewErrUnknown(policy.Kind(), err)
	}

	cond.Status = metav1.ConditionFalse
	cond.Message = policyErr.Error()
	cond.Reason = string(policyErr.Reason())

	return cond
}

// EnforcedCondition returns an enforced conditions with common reasons for a kuadrant policy
func EnforcedCondition(policy KuadrantPolicy, err PolicyError) *metav1.Condition {
	// Enforced
	cond := &metav1.Condition{
		Type:    string(PolicyConditionEnforced),
		Status:  metav1.ConditionTrue,
		Reason:  string(PolicyReasonEnforced),
		Message: fmt.Sprintf("%s has been successfully enforced", policy.Kind()),
	}
	if err == nil {
		return cond
	}

	cond.Status = metav1.ConditionFalse
	cond.Message = err.Error()
	cond.Reason = string(err.Reason())

	return cond
}
