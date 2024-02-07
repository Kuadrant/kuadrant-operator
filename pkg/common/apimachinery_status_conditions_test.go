//go:build unit

package common

import (
	"fmt"
	"reflect"
	"testing"
	"time"

	goCmp "github.com/google/go-cmp/cmp"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

func TestConditionMarshal(t *testing.T) {
	now := metav1.Now()
	nowUTC := now.UTC().Format(time.RFC3339)

	testCases := []struct {
		name               string
		conditions         []metav1.Condition
		expectedJSONOutput string
		expectedError      error
	}{
		{
			name:               "when empty conditions slice then return empty slice",
			conditions:         []metav1.Condition{},
			expectedJSONOutput: "[]",
			expectedError:      nil,
		},
		{
			name: "when single condition then return JSON",
			conditions: []metav1.Condition{
				{
					Type:               "Installed",
					Status:             metav1.ConditionTrue,
					ObservedGeneration: 1,
					LastTransitionTime: now,
					Reason:             "InstallSuccessful",
					Message:            "Object successfully installed",
				},
			},
			expectedJSONOutput: `[{"type":"Installed","status":"True","observedGeneration":1,"lastTransitionTime":"` + nowUTC + `","reason":"InstallSuccessful","message":"Object successfully installed"}]`,
			expectedError:      nil,
		},
		{
			name: "when multiple conditions then return JSON",
			conditions: []metav1.Condition{
				{
					Type:               "Ready",
					Status:             metav1.ConditionFalse,
					ObservedGeneration: 2,
					LastTransitionTime: now,
					Reason:             "ComponentsNotReady",
					Message:            "Object components are not ready",
				},
				{
					Type:               "Installed",
					Status:             metav1.ConditionTrue,
					ObservedGeneration: 1,
					LastTransitionTime: now,
					Reason:             "InstallSuccessful",
					Message:            "Object successfully installed",
				},
				{
					Type:               "Validated",
					Status:             metav1.ConditionUnknown,
					ObservedGeneration: 3,
					LastTransitionTime: now,
					Reason:             "ValidationError",
					Message:            "Object validation failed",
				},
			},
			expectedJSONOutput: `[{"type":"Installed","status":"True","observedGeneration":1,"lastTransitionTime":"` + nowUTC + `","reason":"InstallSuccessful","message":"Object successfully installed"},{"type":"Ready","status":"False","observedGeneration":2,"lastTransitionTime":"` + nowUTC + `","reason":"ComponentsNotReady","message":"Object components are not ready"},{"type":"Validated","status":"Unknown","observedGeneration":3,"lastTransitionTime":"` + nowUTC + `","reason":"ValidationError","message":"Object validation failed"}]`,
			expectedError:      nil,
		},
		{
			name: "when empty ObservedGeneration field (EQ 0) then it is omitted in JSON",
			conditions: []metav1.Condition{
				{
					Type:               "Installed",
					Status:             metav1.ConditionTrue,
					ObservedGeneration: 0,
					LastTransitionTime: now,
					Reason:             "InstallSuccessful",
					Message:            "Object successfully installed",
				},
				{
					Type:               "Validated",
					Status:             metav1.ConditionUnknown,
					ObservedGeneration: 0,
					LastTransitionTime: now,
					Reason:             "ValidationError",
					Message:            "Object validation failed",
				},
			},
			expectedJSONOutput: `[{"type":"Installed","status":"True","lastTransitionTime":"` + nowUTC + `","reason":"InstallSuccessful","message":"Object successfully installed"},{"type":"Validated","status":"Unknown","lastTransitionTime":"` + nowUTC + `","reason":"ValidationError","message":"Object validation failed"}]`,
			expectedError:      nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(subT *testing.T) {
			actualJSONOutput, actualError := ConditionMarshal(tc.conditions)

			diff := goCmp.Diff(tc.expectedJSONOutput, string(actualJSONOutput))
			if diff != "" {
				subT.Errorf("Unexpected JSON output (-want +got):\n%s", diff)
			}

			diff = goCmp.Diff(tc.expectedError, actualError)
			if diff != "" {
				subT.Errorf("Unexpected error (-want +got):\n%s", diff)
			}
		})
	}
}

func TestAcceptedCondition(t *testing.T) {
	type args struct {
		policy KuadrantPolicy
		err    error
	}
	tests := []struct {
		name string
		args args
		want *metav1.Condition
	}{
		{
			name: "accepted reason",
			args: args{
				policy: &FakePolicy{},
			},
			want: &metav1.Condition{
				Type:    string(gatewayapiv1alpha2.PolicyConditionAccepted),
				Status:  metav1.ConditionTrue,
				Reason:  string(gatewayapiv1alpha2.PolicyReasonAccepted),
				Message: "FakePolicy has been accepted",
			},
		},
		{
			name: "target not found reason",
			args: args{
				policy: &FakePolicy{},
				err: NewErrTargetNotFound("FakePolicy", gatewayapiv1alpha2.PolicyTargetReference{
					Group: "gateway.networking.k8s.io",
					Kind:  "HTTPRoute",
					Name:  "my-target-ref",
				}, errors.NewNotFound(schema.GroupResource{}, "my-target-ref")),
			},
			want: &metav1.Condition{
				Type:    string(gatewayapiv1alpha2.PolicyConditionAccepted),
				Status:  metav1.ConditionFalse,
				Reason:  string(gatewayapiv1alpha2.PolicyReasonTargetNotFound),
				Message: "FakePolicy target my-target-ref was not found",
			},
		},
		{
			name: "target not found reason with err",
			args: args{
				policy: &FakePolicy{},
				err: NewErrTargetNotFound("FakePolicy", gatewayapiv1alpha2.PolicyTargetReference{
					Group: "gateway.networking.k8s.io",
					Kind:  "HTTPRoute",
					Name:  "my-target-ref",
				}, fmt.Errorf("deletion err")),
			},
			want: &metav1.Condition{
				Type:    string(gatewayapiv1alpha2.PolicyConditionAccepted),
				Status:  metav1.ConditionFalse,
				Reason:  string(gatewayapiv1alpha2.PolicyReasonTargetNotFound),
				Message: "FakePolicy target my-target-ref was not found: deletion err",
			},
		},
		{
			name: "invalid reason",
			args: args{
				policy: &FakePolicy{},
				err:    NewErrInvalid("FakePolicy", fmt.Errorf("invalid err")),
			},
			want: &metav1.Condition{
				Type:    string(gatewayapiv1alpha2.PolicyConditionAccepted),
				Status:  metav1.ConditionFalse,
				Reason:  string(gatewayapiv1alpha2.PolicyReasonInvalid),
				Message: "FakePolicy target is invalid: invalid err",
			},
		},
		{
			name: "conflicted reason",
			args: args{
				policy: &FakePolicy{},
				err:    NewErrConflict("FakePolicy", "testNs/policy1", fmt.Errorf("conflict err")),
			},
			want: &metav1.Condition{
				Type:    string(gatewayapiv1alpha2.PolicyConditionAccepted),
				Status:  metav1.ConditionFalse,
				Reason:  string(gatewayapiv1alpha2.PolicyReasonConflicted),
				Message: "FakePolicy is conflicted by testNs/policy1: conflict err",
			},
		},
		{
			name: "reconciliation error reason",
			args: args{
				policy: &FakePolicy{},
				err:    fmt.Errorf("reconcile err"),
			},
			want: &metav1.Condition{
				Type:    string(gatewayapiv1alpha2.PolicyConditionAccepted),
				Status:  metav1.ConditionFalse,
				Reason:  "ReconciliationError",
				Message: "reconcile err",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AcceptedCondition(tt.args.policy, tt.args.err); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("AcceptedCondition() = %v, want %v", got, tt.want)
			}
		})
	}
}
