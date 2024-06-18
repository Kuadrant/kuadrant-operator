//go:build unit

package mappers

import (
	"context"
	"testing"

	"gotest.tools/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
	"github.com/kuadrant/kuadrant-operator/pkg/log"
)

func TestHTTPRouteToKuadrantEventMapper(t *testing.T) {
	t.Run("not policy related event", func(subT *testing.T) {
		s := runtime.NewScheme()
		assert.NilError(subT, gatewayapiv1.AddToScheme(s))

		// Objects to track in the fake client.
		objs := []runtime.Object{}
		cl := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(objs...).Build()

		m := NewHTTPRouteToKuadrantEventMapper(WithLogger(log.NewLogger()), WithClient(cl))

		requests := m.Map(context.TODO(), &gatewayapiv1.Gateway{})
		assert.Equal(subT, len(requests), 0)
	})

	t.Run("not accepted route", func(subT *testing.T) {
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
		objs := []runtime.Object{}
		cl := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(objs...).Build()

		m := NewHTTPRouteToKuadrantEventMapper(WithLogger(log.NewLogger()), WithClient(cl))

		requests := m.Map(context.TODO(), route)
		assert.Equal(subT, len(requests), 0)
	})

	t.Run("accepted route", func(subT *testing.T) {
		userNamespace := "user-ns"
		kuadrantNamespace := "kuadrant-ns"
		kuadrantName := "kuadrant-name"

		gateway := &gatewayapiv1.Gateway{
			TypeMeta: metav1.TypeMeta{
				Kind: "Gateway", APIVersion: gatewayapiv1.GroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{Name: "mygateway", Namespace: userNamespace},
		}
		kuadrant.AnnotateObject(gateway, kuadrantName, kuadrantNamespace)

		route := &gatewayapiv1.HTTPRoute{
			TypeMeta: metav1.TypeMeta{
				Kind: "HTTPRoute", APIVersion: gatewayapiv1.GroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{Name: "myroute", Namespace: userNamespace},
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
		objs := []runtime.Object{route, gateway}
		cl := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(objs...).Build()

		m := NewHTTPRouteToKuadrantEventMapper(WithLogger(log.NewLogger()), WithClient(cl))

		requests := m.Map(context.TODO(), route)

		assert.Equal(subT, len(requests), 1)
		assert.DeepEqual(subT, requests[0],
			reconcile.Request{
				NamespacedName: client.ObjectKey{Name: kuadrantName, Namespace: kuadrantNamespace},
			},
		)
	})

	t.Run("parent accepted gateway does not exist", func(subT *testing.T) {
		ns := "ns"

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
		objs := []runtime.Object{}
		cl := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(objs...).Build()

		m := NewHTTPRouteToKuadrantEventMapper(WithLogger(log.NewLogger()), WithClient(cl))

		requests := m.Map(context.TODO(), route)

		assert.Equal(subT, len(requests), 0)
	})

	t.Run("route parent gateway not assigned to kuadrant", func(subT *testing.T) {
		userNamespace := "user-ns"

		// Does not have kuadrant namespace annotated
		gateway := &gatewayapiv1.Gateway{
			TypeMeta: metav1.TypeMeta{
				Kind: "Gateway", APIVersion: gatewayapiv1.GroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{Name: "mygateway", Namespace: userNamespace},
		}

		route := &gatewayapiv1.HTTPRoute{
			TypeMeta: metav1.TypeMeta{
				Kind: "HTTPRoute", APIVersion: gatewayapiv1.GroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{Name: "myroute", Namespace: userNamespace},
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
		objs := []runtime.Object{route, gateway}
		cl := fake.NewClientBuilder().WithScheme(s).WithRuntimeObjects(objs...).Build()

		m := NewHTTPRouteToKuadrantEventMapper(WithLogger(log.NewLogger()), WithClient(cl))

		requests := m.Map(context.TODO(), route)

		assert.Equal(subT, len(requests), 0)
	})
}
