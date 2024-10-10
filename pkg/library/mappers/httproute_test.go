//go:build unit

package mappers

import (
	"context"
	"testing"

	"gotest.tools/assert"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1beta3 "github.com/kuadrant/kuadrant-operator/api/v1beta3"
	"github.com/kuadrant/kuadrant-operator/pkg/library/fieldindexers"
	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
	"github.com/kuadrant/kuadrant-operator/pkg/log"
)

func TestNewHTTPRouteEventMapper(t *testing.T) {
	testScheme := runtime.NewScheme()

	err := appsv1.AddToScheme(testScheme)
	if err != nil {
		t.Fatal(err)
	}
	err = gatewayapiv1.AddToScheme(testScheme)
	if err != nil {
		t.Fatal(err)
	}
	err = kuadrantv1beta3.AddToScheme(testScheme)
	if err != nil {
		t.Fatal(err)
	}

	spec := kuadrantv1beta3.AuthPolicySpec{
		TargetRef: gatewayapiv1alpha2.LocalPolicyTargetReference{
			Group: "gateway.networking.k8s.io",
			Kind:  "HTTPRoute",
			Name:  "test-route",
		},
	}
	routeList := &gatewayapiv1.HTTPRouteList{Items: make([]gatewayapiv1.HTTPRoute, 0)}
	authPolicyList := &kuadrantv1beta3.AuthPolicyList{Items: []kuadrantv1beta3.AuthPolicy{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "policy-1",
				Namespace: "app-ns",
			},
			Spec: spec,
		},
	}}
	gateway := &gatewayapiv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{Name: "test-gw", Namespace: "app-ns"},
		Status: gatewayapiv1.GatewayStatus{
			Conditions: []metav1.Condition{
				{
					Type:   "Programmed",
					Status: "True",
				},
			},
		},
	}
	objs := []runtime.Object{routeList, authPolicyList, gateway}
	cl := fake.NewClientBuilder().WithScheme(testScheme).WithRuntimeObjects(objs...).WithIndex(&gatewayapiv1.HTTPRoute{}, fieldindexers.HTTPRouteGatewayParentField, func(rawObj client.Object) []string {
		return nil
	}).Build()
	em := NewHTTPRouteEventMapper(WithLogger(log.NewLogger()), WithClient(cl))

	t.Run("not http route related event", func(subT *testing.T) {
		requests := em.MapToPolicy(context.Background(), &gatewayapiv1.Gateway{}, kuadrantv1beta3.NewAuthPolicyType())
		assert.DeepEqual(subT, []reconcile.Request{}, requests)
	})

	t.Run("http route related event - no requests", func(subT *testing.T) {
		requests := em.MapToPolicy(context.Background(), &gatewayapiv1.HTTPRoute{}, kuadrantv1beta3.NewAuthPolicyType())
		assert.DeepEqual(subT, []reconcile.Request{}, requests)
	})

	t.Run("http related event - requests", func(subT *testing.T) {
		httpRoute := &gatewayapiv1.HTTPRoute{
			ObjectMeta: metav1.ObjectMeta{
				Name:        "test-route",
				Namespace:   "app-ns",
				Annotations: map[string]string{"kuadrant.io/testpolicies": `[{"Namespace":"app-ns","Name":"policy-1"},{"Namespace":"app-ns","Name":"policy-2"}]`},
			},
			Spec: gatewayapiv1.HTTPRouteSpec{
				CommonRouteSpec: gatewayapiv1.CommonRouteSpec{
					ParentRefs: []gatewayapiv1.ParentReference{{Namespace: ptr.To(gatewayapiv1.Namespace("app-ns")), Name: "test-gw"}},
				},
			},

			Status: gatewayapiv1.HTTPRouteStatus{
				RouteStatus: gatewayapiv1.RouteStatus{
					Parents: []gatewayapiv1.RouteParentStatus{
						{
							ParentRef: gatewayapiv1.ParentReference{
								Name:      "test-gw",
								Namespace: ptr.To(gatewayapiv1.Namespace("app-ns")),
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

		objs = []runtime.Object{routeList, authPolicyList, gateway, httpRoute}
		cl = fake.NewClientBuilder().WithScheme(testScheme).WithRuntimeObjects(objs...).WithIndex(&gatewayapiv1.HTTPRoute{}, fieldindexers.HTTPRouteGatewayParentField, func(rawObj client.Object) []string {
			route, assertionOk := rawObj.(*gatewayapiv1.HTTPRoute)
			if !assertionOk {
				return nil
			}

			return utils.Map(kuadrantgatewayapi.GetRouteAcceptedGatewayParentKeys(route), func(key client.ObjectKey) string {
				return key.String()
			})
		}).Build()
		em = NewHTTPRouteEventMapper(WithLogger(log.NewLogger()), WithClient(cl))
		requests := em.MapToPolicy(context.Background(), httpRoute, kuadrantv1beta3.NewAuthPolicyType())
		expected := []reconcile.Request{{NamespacedName: types.NamespacedName{Namespace: "app-ns", Name: "policy-1"}}}
		assert.DeepEqual(subT, expected, requests)
	})
}
