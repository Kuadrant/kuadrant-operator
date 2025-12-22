/*
Copyright 2025.

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

package controllers

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1 "github.com/kuadrant/kuadrant-operator/api/v1"
	"github.com/kuadrant/kuadrant-operator/internal/kuadrant"
)

func TestGetEnforcementStatus(t *testing.T) {
	reconciler := NewPolicyMetricsReconciler()

	tests := []struct {
		name           string
		policy         *kuadrantv1.AuthPolicy
		expectedStatus PolicyStatus
	}{
		{
			name: "enforced condition is true",
			policy: &kuadrantv1.AuthPolicy{
				Status: kuadrantv1.AuthPolicyStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(kuadrant.PolicyConditionEnforced),
							Status: metav1.ConditionTrue,
							Reason: string(kuadrant.PolicyReasonEnforced),
						},
					},
				},
			},
			expectedStatus: PolicyStatusTrue,
		},
		{
			name: "enforced condition is false",
			policy: &kuadrantv1.AuthPolicy{
				Status: kuadrantv1.AuthPolicyStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(kuadrant.PolicyConditionEnforced),
							Status: metav1.ConditionFalse,
							Reason: string(gatewayapiv1alpha2.PolicyReasonInvalid),
						},
					},
				},
			},
			expectedStatus: PolicyStatusFalse,
		},
		{
			name: "enforced condition is unknown - treated as false",
			policy: &kuadrantv1.AuthPolicy{
				Status: kuadrantv1.AuthPolicyStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(kuadrant.PolicyConditionEnforced),
							Status: metav1.ConditionUnknown,
							Reason: string(kuadrant.PolicyReasonUnknown),
						},
					},
				},
			},
			expectedStatus: PolicyStatusFalse,
		},
		{
			name: "no enforced condition",
			policy: &kuadrantv1.AuthPolicy{
				Status: kuadrantv1.AuthPolicyStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(gatewayapiv1alpha2.PolicyConditionAccepted),
							Status: metav1.ConditionTrue,
							Reason: string(gatewayapiv1alpha2.PolicyReasonAccepted),
						},
					},
				},
			},
			expectedStatus: PolicyStatusFalse,
		},
		{
			name:           "empty status",
			policy:         &kuadrantv1.AuthPolicy{},
			expectedStatus: PolicyStatusFalse,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status := reconciler.getEnforcementStatus(tt.policy)
			if status != tt.expectedStatus {
				t.Errorf("expected status %v, got %v", tt.expectedStatus, status)
			}
		})
	}
}

func TestPolicyStatusConstants(t *testing.T) {
	// Verify the policy status constants are correctly defined
	if PolicyStatusTrue != "true" {
		t.Errorf("expected PolicyStatusTrue to be 'true', got %s", PolicyStatusTrue)
	}
	if PolicyStatusFalse != "false" {
		t.Errorf("expected PolicyStatusFalse to be 'false', got %s", PolicyStatusFalse)
	}
}

func TestMetricLabels(t *testing.T) {
	// Verify the metric label constants
	if policyKindLabel != "kind" {
		t.Errorf("expected policyKindLabel to be 'kind', got %s", policyKindLabel)
	}
	if policyStatusLabel != "status" {
		t.Errorf("expected policyStatusLabel to be 'status', got %s", policyStatusLabel)
	}
}
