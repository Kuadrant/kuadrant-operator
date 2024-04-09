//go:build unit

package mappers

import (
	"context"
	"testing"

	"gotest.tools/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
	"github.com/kuadrant/kuadrant-operator/pkg/log"
)

func TestRLPToLimitadorEventMapper(t *testing.T) {
	t.Run("not policy related event", func(subT *testing.T) {
		// Objects to track in the fake client.
		s := runtime.NewScheme()
		assert.NilError(subT, gatewayapiv1.AddToScheme(s))

		// Objects to track in the fake client.
		objs := []runtime.Object{}
		cl := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(objs...).Build()

		m := NewRLPToLimitadorEventMapper(WithLogger(log.NewLogger()), WithClient(cl))

		requests := m.Map(context.TODO(), &gatewayapiv1.HTTPRoute{})
		assert.Equal(subT, len(requests), 0)
	})

	t.Run("policy targeting gateway", func(subT *testing.T) {
		ns := "ns"
		rlp := &kuadrantv1beta2.RateLimitPolicy{
			TypeMeta: metav1.TypeMeta{
				Kind: "RateLimitPolicy", APIVersion: kuadrantv1beta2.GroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{Name: "a", Namespace: ns},
			Spec: kuadrantv1beta2.RateLimitPolicySpec{
				TargetRef: gatewayapiv1alpha2.PolicyTargetReference{
					Group: gatewayapiv1.GroupName,
					Kind:  "Gateway",
					Name:  gatewayapiv1.ObjectName("mygateway"),
				},
			},
		}

		gateway := &gatewayapiv1.Gateway{
			TypeMeta: metav1.TypeMeta{
				Kind: "Gateway", APIVersion: gatewayapiv1.GroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{Name: "mygateway", Namespace: ns},
		}
		kuadrant.AnnotateObject(gateway, ns)

		s := runtime.NewScheme()
		assert.NilError(subT, gatewayapiv1.AddToScheme(s))

		// Objects to track in the fake client.
		objs := []runtime.Object{gateway}
		cl := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(objs...).Build()

		m := NewRLPToLimitadorEventMapper(WithLogger(log.NewLogger()), WithClient(cl))

		requests := m.Map(context.TODO(), rlp)
		assert.Equal(subT, len(requests), 1)
		assert.DeepEqual(subT, requests[0],
			reconcile.Request{NamespacedName: common.LimitadorObjectKey(ns)},
		)
	})

	t.Run("policy targeting not existing gateway", func(subT *testing.T) {
		ns := "ns"
		rlp := &kuadrantv1beta2.RateLimitPolicy{
			TypeMeta: metav1.TypeMeta{
				Kind: "RateLimitPolicy", APIVersion: kuadrantv1beta2.GroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{Name: "a", Namespace: ns},
			Spec: kuadrantv1beta2.RateLimitPolicySpec{
				TargetRef: gatewayapiv1alpha2.PolicyTargetReference{
					Group: gatewayapiv1.GroupName,
					Kind:  "Gateway",
					Name:  gatewayapiv1.ObjectName("notexisting"),
				},
			},
		}

		s := runtime.NewScheme()
		assert.NilError(subT, gatewayapiv1.AddToScheme(s))

		// Objects to track in the fake client.
		objs := []runtime.Object{}
		cl := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(objs...).Build()

		m := NewRLPToLimitadorEventMapper(WithLogger(log.NewLogger()), WithClient(cl))

		requests := m.Map(context.TODO(), rlp)
		assert.Equal(subT, len(requests), 0)
	})

	t.Run("policy targeting gateway not assigned to kuadrant", func(subT *testing.T) {
		ns := "ns"
		rlp := &kuadrantv1beta2.RateLimitPolicy{
			TypeMeta: metav1.TypeMeta{
				Kind: "RateLimitPolicy", APIVersion: kuadrantv1beta2.GroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{Name: "a", Namespace: ns},
			Spec: kuadrantv1beta2.RateLimitPolicySpec{
				TargetRef: gatewayapiv1alpha2.PolicyTargetReference{
					Group: gatewayapiv1.GroupName,
					Kind:  "Gateway",
					Name:  gatewayapiv1.ObjectName("mygateway"),
				},
			},
		}

		// Does not have kuadrant namespace annotated
		gateway := &gatewayapiv1.Gateway{
			TypeMeta: metav1.TypeMeta{
				Kind: "Gateway", APIVersion: gatewayapiv1.GroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{Name: "mygateway", Namespace: ns},
		}

		s := runtime.NewScheme()
		assert.NilError(subT, gatewayapiv1.AddToScheme(s))

		// Objects to track in the fake client.
		objs := []runtime.Object{gateway}
		cl := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(objs...).Build()

		m := NewRLPToLimitadorEventMapper(WithLogger(log.NewLogger()), WithClient(cl))

		requests := m.Map(context.TODO(), rlp)
		assert.Equal(subT, len(requests), 0)
	})

	t.Run("policy targeting accepted route", func(subT *testing.T) {
		ns := "ns"
		rlp := &kuadrantv1beta2.RateLimitPolicy{
			TypeMeta: metav1.TypeMeta{
				Kind: "RateLimitPolicy", APIVersion: kuadrantv1beta2.GroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{Name: "a", Namespace: ns},
			Spec: kuadrantv1beta2.RateLimitPolicySpec{
				TargetRef: gatewayapiv1alpha2.PolicyTargetReference{
					Group: gatewayapiv1.GroupName,
					Kind:  "HTTPRoute",
					Name:  gatewayapiv1.ObjectName("myroute"),
				},
			},
		}

		gateway := &gatewayapiv1.Gateway{
			TypeMeta: metav1.TypeMeta{
				Kind: "Gateway", APIVersion: gatewayapiv1.GroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{Name: "mygateway", Namespace: ns},
		}
		kuadrant.AnnotateObject(gateway, ns)

		route := &gatewayapiv1.HTTPRoute{
			TypeMeta: metav1.TypeMeta{
				Kind: "HTTPRoute", APIVersion: gatewayapiv1.GroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{Name: "myroute", Namespace: ns},
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
		objs := []runtime.Object{gateway, route}
		cl := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(objs...).Build()

		m := NewRLPToLimitadorEventMapper(WithLogger(log.NewLogger()), WithClient(cl))

		requests := m.Map(context.TODO(), rlp)
		assert.Equal(subT, len(requests), 1)
		assert.DeepEqual(subT, requests[0],
			reconcile.Request{NamespacedName: common.LimitadorObjectKey(ns)},
		)
	})

	t.Run("policy targeting not accepted route", func(subT *testing.T) {
		ns := "ns"
		rlp := &kuadrantv1beta2.RateLimitPolicy{
			TypeMeta: metav1.TypeMeta{
				Kind: "RateLimitPolicy", APIVersion: kuadrantv1beta2.GroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{Name: "a", Namespace: ns},
			Spec: kuadrantv1beta2.RateLimitPolicySpec{
				TargetRef: gatewayapiv1alpha2.PolicyTargetReference{
					Group: gatewayapiv1.GroupName,
					Kind:  "HTTPRoute",
					Name:  gatewayapiv1.ObjectName("myroute"),
				},
			},
		}

		gateway := &gatewayapiv1.Gateway{
			TypeMeta: metav1.TypeMeta{
				Kind: "Gateway", APIVersion: gatewayapiv1.GroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{Name: "mygateway", Namespace: ns},
		}
		kuadrant.AnnotateObject(gateway, ns)

		route := &gatewayapiv1.HTTPRoute{
			TypeMeta: metav1.TypeMeta{
				Kind: "HTTPRoute", APIVersion: gatewayapiv1.GroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{Name: "myroute", Namespace: ns},
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
		objs := []runtime.Object{gateway, route}
		cl := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(objs...).Build()

		m := NewRLPToLimitadorEventMapper(WithLogger(log.NewLogger()), WithClient(cl))

		requests := m.Map(context.TODO(), rlp)
		assert.Equal(subT, len(requests), 0)
	})

	t.Run("policy targeting not existing route", func(subT *testing.T) {
		ns := "ns"
		rlp := &kuadrantv1beta2.RateLimitPolicy{
			TypeMeta: metav1.TypeMeta{
				Kind: "RateLimitPolicy", APIVersion: kuadrantv1beta2.GroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{Name: "a", Namespace: ns},
			Spec: kuadrantv1beta2.RateLimitPolicySpec{
				TargetRef: gatewayapiv1alpha2.PolicyTargetReference{
					Group: gatewayapiv1.GroupName,
					Kind:  "HTTPRoute",
					Name:  gatewayapiv1.ObjectName("unknown"),
				},
			},
		}

		s := runtime.NewScheme()
		assert.NilError(subT, gatewayapiv1.AddToScheme(s))

		// Objects to track in the fake client.
		objs := []runtime.Object{}
		cl := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(objs...).Build()

		m := NewRLPToLimitadorEventMapper(WithLogger(log.NewLogger()), WithClient(cl))

		requests := m.Map(context.TODO(), rlp)
		assert.Equal(subT, len(requests), 0)
	})

	t.Run("policy targeting unexpected resource", func(subT *testing.T) {
		ns := "ns"
		rlp := &kuadrantv1beta2.RateLimitPolicy{
			TypeMeta: metav1.TypeMeta{
				Kind: "RateLimitPolicy", APIVersion: kuadrantv1beta2.GroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{Name: "a", Namespace: ns},
			Spec: kuadrantv1beta2.RateLimitPolicySpec{
				TargetRef: gatewayapiv1alpha2.PolicyTargetReference{
					Group: gatewayapiv1.GroupName,
					Kind:  "Unknown",
					Name:  gatewayapiv1.ObjectName("unknown"),
				},
			},
		}

		s := runtime.NewScheme()
		assert.NilError(subT, gatewayapiv1.AddToScheme(s))

		// Objects to track in the fake client.
		objs := []runtime.Object{}
		cl := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(objs...).Build()

		m := NewRLPToLimitadorEventMapper(WithLogger(log.NewLogger()), WithClient(cl))

		requests := m.Map(context.TODO(), rlp)
		assert.Equal(subT, len(requests), 0)
	})
}
