package mappers

import (
	"testing"

	"github.com/kuadrant/kuadrant-operator/pkg/library/common"
	"github.com/kuadrant/kuadrant-operator/pkg/log"
	"gotest.tools/assert"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayapiv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

func TestNewHTTPRouteEventMapper(t *testing.T) {
	em := NewHTTPRouteEventMapper(WithLogger(log.NewLogger()))

	t.Run("not http route related event", func(subT *testing.T) {
		requests := em.MapToPolicy(&gatewayapiv1beta1.Gateway{}, &common.PolicyKindStub{})
		assert.DeepEqual(subT, []reconcile.Request{}, requests)
	})

	t.Run("http route related event - no requests", func(subT *testing.T) {
		requests := em.MapToPolicy(&gatewayapiv1beta1.HTTPRoute{}, &common.PolicyKindStub{})
		assert.DeepEqual(subT, []reconcile.Request{}, requests)
	})

	t.Run("http related event - requests", func(subT *testing.T) {
		httpRoute := &gatewayapiv1beta1.HTTPRoute{}
		httpRoute.SetAnnotations(map[string]string{"kuadrant.io/testpolicies": `[{"Namespace":"app-ns","Name":"policy-1"},{"Namespace":"app-ns","Name":"policy-2"}]`})
		requests := em.MapToPolicy(httpRoute, &common.PolicyKindStub{})
		expected := []reconcile.Request{{NamespacedName: types.NamespacedName{Namespace: "app-ns", Name: "policy-1"}}, {NamespacedName: types.NamespacedName{Namespace: "app-ns", Name: "policy-2"}}}
		assert.DeepEqual(subT, expected, requests)
	})
}
