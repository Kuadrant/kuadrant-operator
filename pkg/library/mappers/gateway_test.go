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
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	"github.com/kuadrant/kuadrant-operator/pkg/library/fieldindexers"
	"github.com/kuadrant/kuadrant-operator/pkg/log"
)

func TestNewGatewayEventMapper(t *testing.T) {
	err := appsv1.AddToScheme(scheme.Scheme)
	if err != nil {
		t.Fatal(err)
	}
	err = gatewayapiv1.AddToScheme(scheme.Scheme)
	if err != nil {
		t.Fatal(err)
	}
	err = kuadrantv1beta2.AddToScheme(scheme.Scheme)
	if err != nil {
		t.Fatal(err)
	}

	spec := kuadrantv1beta2.AuthPolicySpec{
		TargetRef: gatewayapiv1alpha2.PolicyTargetReference{
			Group: "gateway.networking.k8s.io",
			Kind:  "Gateway",
			Name:  "test-gw",
		},
	}
	routeList := &gatewayapiv1.HTTPRouteList{Items: make([]gatewayapiv1.HTTPRoute, 0)}
	authPolicyList := &kuadrantv1beta2.AuthPolicyList{Items: []kuadrantv1beta2.AuthPolicy{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "policy-1",
				Namespace: "app-ns",
			},
			Spec: spec,
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "policy-2",
				Namespace: "app-ns",
			},
			Spec: spec,
		},
	}}
	objs := []runtime.Object{routeList, authPolicyList}
	cl := fake.NewClientBuilder().WithScheme(scheme.Scheme).WithRuntimeObjects(objs...).WithIndex(&gatewayapiv1.HTTPRoute{}, fieldindexers.HTTPRouteGatewayParentField, func(rawObj client.Object) []string {
		return nil
	}).Build()
	em := NewGatewayEventMapper(WithLogger(log.NewLogger()), WithClient(cl))

	t.Run("not gateway related event", func(subT *testing.T) {
		requests := em.MapToPolicy(context.Background(), &gatewayapiv1.HTTPRoute{}, &kuadrantv1beta2.RateLimitPolicy{})
		assert.DeepEqual(subT, []reconcile.Request{}, requests)
	})

	t.Run("gateway related event - no requests", func(subT *testing.T) {
		requests := em.MapToPolicy(context.Background(), &gatewayapiv1.Gateway{}, &kuadrantv1beta2.RateLimitPolicy{})
		assert.DeepEqual(subT, []reconcile.Request{}, requests)
	})

	t.Run("gateway related event - requests", func(subT *testing.T) {
		gateway := &gatewayapiv1.Gateway{
			ObjectMeta: metav1.ObjectMeta{Name: "test-gw", Namespace: "app-ns"},
			Status: gatewayapiv1alpha2.GatewayStatus{
				Conditions: []metav1.Condition{
					{
						Type:   "Programmed",
						Status: "True",
					},
				},
			},
		}
		requests := em.MapToPolicy(context.Background(), gateway, &kuadrantv1beta2.AuthPolicy{})
		expected := []reconcile.Request{{NamespacedName: types.NamespacedName{Namespace: "app-ns", Name: "policy-1"}}, {NamespacedName: types.NamespacedName{Namespace: "app-ns", Name: "policy-2"}}}
		assert.DeepEqual(subT, expected, requests)
	})
}
