package controller

import (
	"fmt"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	"github.com/kuadrant/kuadrant-operator/internal/kuadrant"
	exttypes "github.com/kuadrant/kuadrant-operator/pkg/extension/types"
)

type policyObjectKind struct {
	group   string
	kind    string
	version string
}

func (p policyObjectKind) SetGroupVersionKind(_ schema.GroupVersionKind) {

}

func (p policyObjectKind) GroupVersionKind() schema.GroupVersionKind {
	return schema.GroupVersionKind{
		Group:   p.group,
		Version: p.version,
		Kind:    p.kind,
	}
}

type fakePolicy struct {
	kind string
}

func (f *fakePolicy) GetName() string {
	return f.kind
}

func (f *fakePolicy) GetNamespace() string {
	return ""
}

func (f *fakePolicy) GetTargetRefs() []gatewayapiv1alpha2.LocalPolicyTargetReferenceWithSectionName {
	return nil
}

func (f *fakePolicy) GetObjectKind() schema.ObjectKind {
	return &policyObjectKind{
		group:   "",
		version: "",
		kind:    f.kind,
	}
}

func TestAcceptedCondition(t *testing.T) {
	testCases := []struct {
		name           string
		policy         exttypes.Policy
		errorInput     error
		expectedStatus metav1.ConditionStatus
		expectedType   string
		expectedReason string
	}{
		{
			name: "nil error creates accepted condition",
			policy: &fakePolicy{
				kind: "MyPolicy",
			},
			errorInput:     nil,
			expectedStatus: metav1.ConditionTrue,
			expectedType:   string(gatewayapiv1alpha2.PolicyConditionAccepted),
			expectedReason: string(gatewayapiv1alpha2.PolicyReasonAccepted),
		},
		{
			name: "error creates false condition",
			policy: &fakePolicy{
				kind: "MyPolicy",
			},
			errorInput:     fmt.Errorf("test error"),
			expectedStatus: metav1.ConditionFalse,
			expectedType:   string(gatewayapiv1alpha2.PolicyConditionAccepted),
			expectedReason: "Unknown",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			condition := AcceptedCondition(tc.policy, tc.errorInput)

			if condition.Type != tc.expectedType {
				t.Errorf("condition Type mismatch: got %q, want %q",
					condition.Type, tc.expectedType)
			}

			if condition.Status != tc.expectedStatus {
				t.Errorf("condition Status mismatch: got %q, want %q",
					condition.Status, tc.expectedStatus)
			}

			if condition.Reason != tc.expectedReason {
				t.Errorf("condition Reason mismatch: got %q, want %q",
					condition.Reason, tc.expectedReason)
			}

			if tc.errorInput == nil {
				expectedMessage := fmt.Sprintf("%s has been accepted",
					tc.policy.GetObjectKind().GroupVersionKind().Kind)
				if condition.Message != expectedMessage {
					t.Errorf("message mismatch: got %q, want %q",
						condition.Message, expectedMessage)
				}
			} else {
				if !strings.Contains(condition.Message, tc.errorInput.Error()) {
					t.Errorf("message does not contain error: got %q, want %q",
						condition.Message, tc.errorInput.Error())
				}
			}
		})
	}
}

func TestEnforcedCondition(t *testing.T) {
	testCases := []struct {
		name                    string
		policy                  exttypes.Policy
		errorInput              error
		fullyEnforced           bool
		expectedStatus          metav1.ConditionStatus
		expectedType            string
		expectedReason          string
		expectedMessageContains string
	}{
		{
			name: "fully enforced with no error",
			policy: &fakePolicy{
				kind: "MyPolicy",
			},
			errorInput:              nil,
			fullyEnforced:           true,
			expectedStatus:          metav1.ConditionTrue,
			expectedType:            string(kuadrant.PolicyConditionEnforced),
			expectedReason:          string(kuadrant.PolicyReasonEnforced),
			expectedMessageContains: "has been successfully enforced",
		},
		{
			name: "partially enforced with no error",
			policy: &fakePolicy{
				kind: "MyPolicy",
			},
			errorInput:              nil,
			fullyEnforced:           false,
			expectedStatus:          metav1.ConditionTrue,
			expectedType:            string(kuadrant.PolicyConditionEnforced),
			expectedReason:          string(kuadrant.PolicyReasonEnforced),
			expectedMessageContains: "has been partially enforced",
		},
		{
			name: "error condition",
			policy: &fakePolicy{
				kind: "MyPolicy",
			},
			errorInput:              fmt.Errorf("test error"),
			fullyEnforced:           true,
			expectedStatus:          metav1.ConditionFalse,
			expectedType:            string(kuadrant.PolicyConditionEnforced),
			expectedReason:          "Unknown",
			expectedMessageContains: "test error",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			condition := EnforcedCondition(tc.policy, tc.errorInput, tc.fullyEnforced)

			if condition.Type != tc.expectedType {
				t.Errorf("condition Type mismatch: got %q, want %q",
					condition.Type, tc.expectedType)
			}

			if condition.Status != tc.expectedStatus {
				t.Errorf("condition Status mismatch: got %q, want %q",
					condition.Status, tc.expectedStatus)
			}

			if condition.Reason != tc.expectedReason {
				t.Errorf("condition Reason mismatch: got %q, want %q",
					condition.Reason, tc.expectedReason)
			}

			if !strings.Contains(condition.Message, tc.expectedMessageContains) {
				t.Errorf("message does not contain expected text: got %q, want to contain %q",
					condition.Message, tc.expectedMessageContains)
			}
		})
	}
}
