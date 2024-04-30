//go:build unit

package controllers

import (
	"errors"
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
)

func TestRateLimitPolicyReconciler_calculateStatus(t *testing.T) {
	type args struct {
		rlp     *kuadrantv1beta2.RateLimitPolicy
		specErr error
	}
	tests := []struct {
		name string
		args args
		want *kuadrantv1beta2.RateLimitPolicyStatus
	}{
		{
			name: "Enforced status block removed if policy not Accepted. (Regression test)", // https://github.com/Kuadrant/kuadrant-operator/issues/588
			args: args{
				rlp: &kuadrantv1beta2.RateLimitPolicy{
					Status: kuadrantv1beta2.RateLimitPolicyStatus{
						Conditions: []metav1.Condition{
							{
								Message: "not accepted",
								Type:    string(gatewayapiv1alpha2.PolicyConditionAccepted),
								Status:  metav1.ConditionFalse,
								Reason:  string(gatewayapiv1alpha2.PolicyReasonTargetNotFound),
							},
							{
								Message: "RateLimitPolicy has been successfully enforced",
								Type:    string(kuadrant.PolicyConditionEnforced),
								Status:  metav1.ConditionTrue,
								Reason:  string(kuadrant.PolicyConditionEnforced),
							},
						},
					},
				},
				specErr: kuadrant.NewErrInvalid("RateLimitPolicy", errors.New("policy Error")),
			},
			want: &kuadrantv1beta2.RateLimitPolicyStatus{
				Conditions: []metav1.Condition{
					{
						Message: "RateLimitPolicy target is invalid: policy Error",
						Type:    string(gatewayapiv1alpha2.PolicyConditionAccepted),
						Status:  metav1.ConditionFalse,
						Reason:  string(gatewayapiv1alpha2.PolicyReasonInvalid),
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := &RateLimitPolicyReconciler{}
			if got := r.calculateStatus(tt.args.rlp, tt.args.specErr); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("calculateStatus() = %v, want %v", got, tt.want)
			}
		})
	}
}
