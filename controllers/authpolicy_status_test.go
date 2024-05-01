//go:build unit

package controllers

import (
	"context"
	"errors"
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	api "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
)

func TestAuthPolicyReconciler_calculateStatus(t *testing.T) {
	type args struct {
		ctx     context.Context
		ap      *api.AuthPolicy
		specErr error
	}
	tests := []struct {
		name string
		args args
		want *api.AuthPolicyStatus
	}{
		{
			name: "Enforced status block removed if policy not Accepted. (Regression test)", // https://github.com/Kuadrant/kuadrant-operator/issues/588
			args: args{
				ap: &api.AuthPolicy{
					Status: api.AuthPolicyStatus{
						Conditions: []metav1.Condition{
							{
								Message: "not accepted",
								Type:    string(gatewayapiv1alpha2.PolicyConditionAccepted),
								Status:  metav1.ConditionFalse,
								Reason:  string(gatewayapiv1alpha2.PolicyReasonTargetNotFound),
							},
							{
								Message: "AuthPolicy has been successfully enforced",
								Type:    string(kuadrant.PolicyConditionEnforced),
								Status:  metav1.ConditionTrue,
								Reason:  string(kuadrant.PolicyConditionEnforced),
							},
						},
					},
				},
				specErr: kuadrant.NewErrInvalid("AuthPolicy", errors.New("policy Error")),
			},
			want: &api.AuthPolicyStatus{
				Conditions: []metav1.Condition{
					{
						Message: "AuthPolicy target is invalid: policy Error",
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
			r := &AuthPolicyReconciler{}
			if got := r.calculateStatus(tt.args.ctx, tt.args.ap, tt.args.specErr); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("calculateStatus() = %v, want %v", got, tt.want)
			}
		})
	}
}
