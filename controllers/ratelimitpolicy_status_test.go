//go:build unit

package controllers

import (
	"context"
	"testing"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
)

func TestRateLimitPolicyReconciler_calculateStatus(t *testing.T) {
	type args struct {
		rlp      *kuadrantv1beta2.RateLimitPolicy
		topology *kuadrantgatewayapi.Topology
	}
	tests := []struct {
		name string
		args args
	}{
		{
			name: "Enforced status block removed if policy not Accepted. (Regression test)", // https://github.com/Kuadrant/kuadrant-operator/issues/588
			args: args{
				// invalid policy targeting to some external namespace
				rlp: &kuadrantv1beta2.RateLimitPolicy{
					TypeMeta: metav1.TypeMeta{
						Kind:       "RateLimitPolicy",
						APIVersion: kuadrantv1beta2.GroupVersion.String(),
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:      "a",
						Namespace: "ns-rlp",
					},
					Spec: kuadrantv1beta2.RateLimitPolicySpec{
						TargetRef: gatewayapiv1alpha2.PolicyTargetReference{
							Group:     gatewayapiv1.GroupName,
							Kind:      "HTTPRoute",
							Name:      gatewayapiv1.ObjectName("some-route"),
							Namespace: ptr.To(gatewayapiv1.Namespace("ns-external")),
						},
					},
					Status: kuadrantv1beta2.RateLimitPolicyStatus{
						Conditions: []metav1.Condition{
							{
								Message: "RateLimitPolicy has been successfully enforced",
								Type:    string(kuadrant.PolicyConditionEnforced),
								Status:  metav1.ConditionTrue,
								Reason:  string(kuadrant.PolicyConditionEnforced),
							},
						},
					},
				},
				topology: nil,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(subT *testing.T) {
			r := &RateLimitPolicyStatusReconciler{}
			got := r.calculateStatus(context.TODO(), tt.args.rlp, tt.args.topology)

			if meta.IsStatusConditionTrue(
				got.Conditions, string(gatewayapiv1alpha2.PolicyConditionAccepted),
			) {
				subT.Error("accepted condition is true")
			}

			if meta.FindStatusCondition(got.Conditions, string(kuadrant.PolicyConditionEnforced)) != nil {
				subT.Error("enforced condition is still present")
			}
		})
	}
}
