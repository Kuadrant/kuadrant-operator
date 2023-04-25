//go:build unit

package common

import (
	goCmp "github.com/google/go-cmp/cmp"
	authorinov1beta1 "github.com/kuadrant/authorino-operator/api/v1beta1"
	"testing"
)

func TestFindAuthorinoStatusCondition(t *testing.T) {
	testCases := []struct {
		name          string
		input         []authorinov1beta1.Condition
		conditionType string
		expected      *authorinov1beta1.Condition
	}{
		{
			name: "when no matching conditions then return nil",
			input: []authorinov1beta1.Condition{
				{Type: "Failed", Status: "NotReady"},
				{Type: "Active", Status: "Ready"},
			},
			conditionType: "Ready",
			expected:      nil,
		},
		{
			name: "when matching condition found then return matching condition",
			input: []authorinov1beta1.Condition{
				{Type: "Active", Status: "Ready"},
				{Type: "Pending", Status: "Unknown"},
				{Type: "Error", Status: "Ready"},
			},
			conditionType: "Error",
			expected:      &authorinov1beta1.Condition{Type: "Error", Status: "Ready"},
		},
		{
			name:          "when empty conditions slice then return nil",
			input:         []authorinov1beta1.Condition{},
			conditionType: "Ready",
			expected:      nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := FindAuthorinoStatusCondition(tc.input, tc.conditionType)

			if diff := goCmp.Diff(tc.expected, result); diff != "" {
				t.Errorf("Condition mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
