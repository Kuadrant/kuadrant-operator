package v1alpha1

import (
	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	"github.com/kuadrant/kuadrant-controller/pkg/common"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Interface to be implemented by Policy's status struct
// +kubebuilder:object:generate=false
type PolicyStatus interface {
	GetObservedGeneration() int64
	GetConditions() []metav1.Condition
}

func (r RateLimitPolicyStatus) GetObservedGeneration() int64 {
	return r.ObservedGeneration
}

func (r RateLimitPolicyStatus) GetConditions() []metav1.Condition {
	return r.Conditions
}

func (r AuthPolicyStatus) GetObservedGeneration() int64 {
	return r.ObservedGeneration
}

func (r AuthPolicyStatus) GetConditions() []metav1.Condition {
	return r.Conditions
}

func StatusEquals(old, new PolicyStatus, logger logr.Logger) bool {
	if old.GetObservedGeneration() != new.GetObservedGeneration() {
		diff := cmp.Diff(old.GetObservedGeneration(), new.GetObservedGeneration())
		logger.V(1).Info("ObservedGeneration not equal", "difference", diff)
		return false
	}

	// Marshalling sorts by condition type
	currentMarshaledJSON, _ := common.ConditionMarshal(old.GetConditions())
	otherMarshaledJSON, _ := common.ConditionMarshal(new.GetConditions())
	if string(currentMarshaledJSON) != string(otherMarshaledJSON) {
		diff := cmp.Diff(string(currentMarshaledJSON), string(otherMarshaledJSON))
		logger.V(1).Info("Conditions not equal", "difference", diff)
		return false
	}

	return true
}
