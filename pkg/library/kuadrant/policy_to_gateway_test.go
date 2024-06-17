//go:build unit

package kuadrant

import (
	"context"
	"testing"

	"github.com/go-logr/logr"
	"github.com/kuadrant/kuadrant-operator/pkg/log"
	"gotest.tools/assert"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

func TestGatewaysFromPolicy(t *testing.T) {
	t.Run("policy targeting gateway", func(subT *testing.T) {
		rlp := &kuadrantv1beta2.RateLimitPolicy{
			TypeMeta: metav1.TypeMeta{
				Kind: "RateLimitPolicy", APIVersion: kuadrantv1beta2.GroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{Name: "a", Namespace: "ns"},
			Spec: kuadrantv1beta2.RateLimitPolicySpec{
				TargetRef: gatewayapiv1alpha2.PolicyTargetReference{
					Group: gatewayapiv1.GroupName,
					Kind:  "Gateway",
					Name:  gatewayapiv1.ObjectName("g"),
				},
			},
		}

		// Objects to track in the fake client.
		objs := []runtime.Object{}
		cl := fake.NewClientBuilder().WithRuntimeObjects(objs...).Build()

		ctx := logr.NewContext(context.TODO(), log.NewLogger())

		gwKeys, err := GatewaysFromPolicy(ctx, cl, rlp)
		assert.NilError(subT, err)
		assert.Equal(subT, len(gwKeys), 1)
		assert.DeepEqual(subT, gwKeys[0],
			client.Objectkey{Name: "g", Namespace: "ns"},
		)
	})

	t.Run("policy targeting accepted route", func(subT *testing.T) {
		rlp := &kuadrantv1beta2.RateLimitPolicy{
			TypeMeta: metav1.TypeMeta{
				Kind: "RateLimitPolicy", APIVersion: kuadrantv1beta2.GroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{Name: "a", Namespace: "ns"},
			Spec: kuadrantv1beta2.RateLimitPolicySpec{
				TargetRef: gatewayapiv1alpha2.PolicyTargetReference{
					Group: gatewayapiv1.GroupName,
					Kind:  "HTTPRoute",
					Name:  gatewayapiv1.ObjectName("myroute"),
				},
			},
		}

		route := &gatewayapiv1.HTTPRoute{
			TypeMeta: metav1.TypeMeta{
				Kind: "HTTPRoute", APIVersion: gatewayapiv1.GroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{Name: "myroute", Namespace: "ns"},
			Spec: gatewayapiv1.HTTPRouteSpec{
				CommonRouteSpec: gatewayapiv1.CommonRouteSpec{
					ParentRefs: []gatewayapiv1.ParentReference{
						{
							Kind:  ptr.To(gatewayapiv1.Kind("Gateway")),
							Group: ptr.To(gatewayapiv1.Group(gatewayapiv1.GroupName)),
							Name:  "mygateway",
						},
					},
				},
			},
			Status: gatewayapiv1.HTTPRouteStatus{
				RouteStatus: gatewayapiv1.RouteStatus{
					Parents: []gatewayapiv1.RouteParentStatus{
						{
							ParentRef: gatewayapiv1.ParentReference{
								Kind:  ptr.To(gatewayapiv1.Kind("Gateway")),
								Group: ptr.To(gatewayapiv1.Group(gatewayapiv1.GroupName)),
								Name:  "mygateway",
							},
							Conditions: []metav1.Condition{
								{
									Type:   "Accepted",
									Status: metav1.ConditionTrue,
								},
							},
						},
					},
				},
			},
		}

		s := runtime.NewScheme()
		assert.NilError(subT, gatewayapiv1.AddToScheme(s))

		// Objects to track in the fake client.
		objs := []runtime.Object{route}
		cl := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(objs...).Build()

		ctx := logr.NewContext(context.TODO(), log.NewLogger())

		gwKeys, err := GatewaysFromPolicy(ctx, cl, rlp)
		assert.NilError(subT, err)
		assert.Equal(subT, len(gwKeys), 1)
		assert.DeepEqual(subT, gwKeys[0],
			client.Objectkey{Name: "mygateway", Namespace: "ns"},
		)
	})

	t.Run("policy targeting not existing route", func(subT *testing.T) {
		rlp := &kuadrantv1beta2.RateLimitPolicy{
			TypeMeta: metav1.TypeMeta{
				Kind: "RateLimitPolicy", APIVersion: kuadrantv1beta2.GroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{Name: "a", Namespace: "ns"},
			Spec: kuadrantv1beta2.RateLimitPolicySpec{
				TargetRef: gatewayapiv1alpha2.PolicyTargetReference{
					Group: gatewayapiv1.GroupName,
					Kind:  "HTTPRoute",
					Name:  gatewayapiv1.ObjectName("notexistingroute"),
				},
			},
		}

		s := runtime.NewScheme()
		assert.NilError(subT, gatewayapiv1.AddToScheme(s))

		// Objects to track in the fake client.
		objs := []runtime.Object{}
		cl := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(objs...).Build()

		ctx := logr.NewContext(context.TODO(), log.NewLogger())
		gwKeys, err := GatewaysFromPolicy(ctx, cl, rlp)
		assert.NilError(subT, err)
		assert.Equal(subT, len(gwKeys), 0)
	})

	t.Run("policy targeting not accepted route", func(subT *testing.T) {
		rlp := &kuadrantv1beta2.RateLimitPolicy{
			TypeMeta: metav1.TypeMeta{
				Kind: "RateLimitPolicy", APIVersion: kuadrantv1beta2.GroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{Name: "a", Namespace: "ns"},
			Spec: kuadrantv1beta2.RateLimitPolicySpec{
				TargetRef: gatewayapiv1alpha2.PolicyTargetReference{
					Group: gatewayapiv1.GroupName,
					Kind:  "HTTPRoute",
					Name:  gatewayapiv1.ObjectName("myroute"),
				},
			},
		}

		route := &gatewayapiv1.HTTPRoute{
			TypeMeta: metav1.TypeMeta{
				Kind: "HTTPRoute", APIVersion: gatewayapiv1.GroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{Name: "myroute", Namespace: "ns"},
			Spec: gatewayapiv1.HTTPRouteSpec{
				CommonRouteSpec: gatewayapiv1.CommonRouteSpec{
					ParentRefs: []gatewayapiv1.ParentReference{
						{
							Kind:  ptr.To(gatewayapiv1.Kind("Gateway")),
							Group: ptr.To(gatewayapiv1.Group(gatewayapiv1.GroupName)),
							Name:  "mygateway",
						},
					},
				},
			},
			Status: gatewayapiv1.HTTPRouteStatus{
				RouteStatus: gatewayapiv1.RouteStatus{
					Parents: []gatewayapiv1.RouteParentStatus{
						{
							ParentRef: gatewayapiv1.ParentReference{
								Kind:  ptr.To(gatewayapiv1.Kind("Gateway")),
								Group: ptr.To(gatewayapiv1.Group(gatewayapiv1.GroupName)),
								Name:  "mygateway",
							},
							Conditions: []metav1.Condition{
								{
									Type:   "Accepted",
									Status: metav1.ConditionFalse,
								},
							},
						},
					},
				},
			},
		}

		s := runtime.NewScheme()
		assert.NilError(subT, gatewayapiv1.AddToScheme(s))

		// Objects to track in the fake client.
		objs := []runtime.Object{route}
		cl := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(objs...).Build()

		gwKeys, err := GatewaysFromPolicy(ctx, cl, rlp)
		assert.NilError(subT, err)
		assert.Equal(subT, len(gwKeys), 0)
	})

	t.Run("policy targeting unexpected resource", func(subT *testing.T) {
		rlp := &kuadrantv1beta2.RateLimitPolicy{
			TypeMeta: metav1.TypeMeta{
				Kind: "RateLimitPolicy", APIVersion: kuadrantv1beta2.GroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{Name: "a", Namespace: "ns"},
			Spec: kuadrantv1beta2.RateLimitPolicySpec{
				TargetRef: gatewayapiv1alpha2.PolicyTargetReference{
					Group: gatewayapiv1.GroupName,
					Kind:  "Unknown",
					Name:  gatewayapiv1.ObjectName("other"),
				},
			},
		}

		// Objects to track in the fake client.
		objs := []runtime.Object{}
		cl := fake.NewClientBuilder().WithRuntimeObjects(objs...).Build()

		ctx := logr.NewContext(context.TODO(), log.NewLogger())

		gwKeys, err := GatewaysFromPolicy(ctx, cl, rlp)
		assert.NilError(subT, err)
		assert.Equal(subT, len(gwKeys), 0)
	})
}
