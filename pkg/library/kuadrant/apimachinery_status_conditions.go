package kuadrant

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sort"
	"sync"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

const (
	PolicyConditionEnforced gatewayapiv1alpha2.PolicyConditionType = "Enforced"

	PolicyReasonEnforced   gatewayapiv1alpha2.PolicyConditionReason = "Enforced"
	PolicyReasonOverridden gatewayapiv1alpha2.PolicyConditionReason = "Overridden"
	PolicyReasonUnknown    gatewayapiv1alpha2.PolicyConditionReason = "Unknown"
)

func NewAffectedPolicyMap() *AffectedPolicyMap {
	return &AffectedPolicyMap{
		policies: make(map[types.UID][]client.ObjectKey),
	}
}

type AffectedPolicyMap struct {
	policies map[types.UID][]client.ObjectKey
	mu       sync.RWMutex
}

// SetAffectedPolicy sets the provided Policy as Affected in the tracking map.
func (o *AffectedPolicyMap) SetAffectedPolicy(p Policy, affectedBy []client.ObjectKey) {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.policies == nil {
		o.policies = make(map[types.UID][]client.ObjectKey)
	}
	o.policies[p.GetUID()] = affectedBy
}

// RemoveAffectedPolicy removes the provided Policy from the tracking map of Affected policies.
func (o *AffectedPolicyMap) RemoveAffectedPolicy(p Policy) {
	o.mu.Lock()
	defer o.mu.Unlock()

	delete(o.policies, p.GetUID())
}

// IsPolicyAffected checks if the provided Policy is affected based on the tracking map maintained.
func (o *AffectedPolicyMap) IsPolicyAffected(p Policy) bool {
	return o.policies[p.GetUID()] != nil
}

// IsPolicyOverridden checks if the provided Policy is affected based on the tracking map maintained.
// It is overridden if there is policies affecting it
func (o *AffectedPolicyMap) IsPolicyOverridden(p Policy) bool {
	return o.IsPolicyAffected(p) && len(o.policies[p.GetUID()]) > 0
}

// PolicyAffectedBy returns the clients keys that a policy is Affected by
func (o *AffectedPolicyMap) PolicyAffectedBy(p Policy) []client.ObjectKey {
	return o.policies[p.GetUID()]
}

// ConditionMarshal marshals the set of conditions as a JSON array, sorted by condition type.
func ConditionMarshal(conditions []metav1.Condition) ([]byte, error) {
	condCopy := slices.Clone(conditions)
	sort.Slice(condCopy, func(a, b int) bool {
		return condCopy[a].Type < condCopy[b].Type
	})
	return json.Marshal(condCopy)
}

// AcceptedCondition returns an accepted conditions with common reasons for a kuadrant policy
func AcceptedCondition(p Policy, err error) *metav1.Condition {
	// Accepted
	cond := &metav1.Condition{
		Type:    string(gatewayapiv1alpha2.PolicyConditionAccepted),
		Status:  metav1.ConditionTrue,
		Reason:  string(gatewayapiv1alpha2.PolicyReasonAccepted),
		Message: fmt.Sprintf("%s has been accepted", p.Kind()),
	}
	if err == nil {
		return cond
	}

	// Wrap error into a PolicyError if it is not this type
	var policyErr PolicyError
	if !errors.As(err, &policyErr) {
		policyErr = NewErrUnknown(p.Kind(), err)
	}

	cond.Status = metav1.ConditionFalse
	cond.Message = policyErr.Error()
	cond.Reason = string(policyErr.Reason())

	return cond
}

// EnforcedCondition returns an enforced conditions with common reasons for a kuadrant policy
func EnforcedCondition(policy Policy, err PolicyError, fully bool) *metav1.Condition {
	// Enforced
	message := fmt.Sprintf("%s has been successfully enforced", policy.Kind())
	if !fully {
		message = fmt.Sprintf("%s has been partially enforced", policy.Kind())
	}
	cond := &metav1.Condition{
		Type:    string(PolicyConditionEnforced),
		Status:  metav1.ConditionTrue,
		Reason:  string(PolicyReasonEnforced),
		Message: message,
	}
	if err == nil {
		return cond
	}

	cond.Status = metav1.ConditionFalse
	cond.Message = err.Error()
	cond.Reason = string(err.Reason())

	return cond
}
