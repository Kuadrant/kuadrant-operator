//go:build unit

package common

import (
	goCmp "github.com/google/go-cmp/cmp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"testing"
)

func TestCopyCondition(t *testing.T) {
	now := metav1.Now()
	testCases := []struct {
		name               string
		expectedConditions []metav1.Condition
	}{
		{
			name: "default conditions",
			expectedConditions: []metav1.Condition{
				metav1.Condition{
					Type:               "Installed",
					Status:             metav1.ConditionTrue,
					ObservedGeneration: 1,
					LastTransitionTime: now,
					Reason:             "InstallSuccessful",
					Message:            "Object successfully installed",
				},
				metav1.Condition{
					Type:               "Reconciled",
					Status:             metav1.ConditionFalse,
					ObservedGeneration: 0,
					LastTransitionTime: now,
					Reason:             "ReconcileError",
					Message:            "Object failed to reconcile due to an error",
				},
				metav1.Condition{
					Type:               "Ready",
					Status:             metav1.ConditionFalse,
					ObservedGeneration: 2,
					LastTransitionTime: now,
					Reason:             "ComponentsNotReady",
					Message:            "Object components are not ready",
				},
				metav1.Condition{
					Type:               "Validated",
					Status:             metav1.ConditionUnknown,
					ObservedGeneration: 0,
					LastTransitionTime: now,
					Reason:             "ValidationError",
					Message:            "Object validation failed",
				},
			},
		},
		{
			name: "one condition",
			expectedConditions: []metav1.Condition{
				metav1.Condition{
					Type:               "Validated",
					Status:             metav1.ConditionUnknown,
					ObservedGeneration: 0,
					LastTransitionTime: now,
					Reason:             "ValidationError",
					Message:            "Object validation failed",
				},
			},
		},
		{
			name:               "empty slice",
			expectedConditions: []metav1.Condition{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(subT *testing.T) {
			actualConditions := CopyConditions(tc.expectedConditions)
			if diff := goCmp.Diff(tc.expectedConditions, actualConditions); diff != "" {
				subT.Errorf("Conditions mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestCopyCondition_OriginalConditionsNotModified(t *testing.T) {
	now := metav1.Now()
	expectedConditions := []metav1.Condition{
		metav1.Condition{
			Type:               "Installed",
			Status:             metav1.ConditionTrue,
			ObservedGeneration: 1,
			LastTransitionTime: now,
			Reason:             "InstallSuccessful",
			Message:            "Object successfully installed",
		},
		metav1.Condition{
			Type:               "Reconciled",
			Status:             metav1.ConditionFalse,
			ObservedGeneration: 0,
			LastTransitionTime: now,
			Reason:             "ReconcileError",
			Message:            "Object failed to reconcile due to an error",
		},
	}

	originalConditions := append([]metav1.Condition{}, expectedConditions...)
	actualConditions := CopyConditions(expectedConditions)

	// Check that the copied conditions are equal to the original conditions
	if diff := goCmp.Diff(expectedConditions, actualConditions); diff != "" {
		t.Errorf("Conditions mismatch (-want +got):\n%s", diff)
	}

	// Check that the original conditions were not modified
	if diff := goCmp.Diff(originalConditions, expectedConditions); diff != "" {
		t.Errorf("Original conditions were modified (-want +got):\n%s", diff)
	}
}
