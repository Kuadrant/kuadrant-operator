//go:build unit

package mappers

import (
	"testing"

	"gotest.tools/assert"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
	"github.com/kuadrant/kuadrant-operator/pkg/log"
)

func TestNewHTTPRouteEventMapper(t *testing.T) {
	em := NewHTTPRouteEventMapper(WithLogger(log.NewLogger()))

	t.Run("not http route related event", func(subT *testing.T) {
		requests := em.MapToPolicy(&gatewayapiv1.Gateway{}, &kuadrant.PolicyKindStub{})
		assert.DeepEqual(subT, []reconcile.Request{}, requests)
	})

	t.Run("http route related event - no requests", func(subT *testing.T) {
		requests := em.MapToPolicy(&gatewayapiv1.HTTPRoute{}, &kuadrant.PolicyKindStub{})
		assert.DeepEqual(subT, []reconcile.Request{}, requests)
	})

	t.Run("http related event - requests", func(subT *testing.T) {
		httpRoute := &gatewayapiv1.HTTPRoute{}
		httpRoute.SetAnnotations(map[string]string{"kuadrant.io/testpolicies": `[{"Namespace":"app-ns","Name":"policy-1"},{"Namespace":"app-ns","Name":"policy-2"}]`})
		requests := em.MapToPolicy(httpRoute, &kuadrant.PolicyKindStub{})
		expected := []reconcile.Request{{NamespacedName: types.NamespacedName{Namespace: "app-ns", Name: "policy-1"}}, {NamespacedName: types.NamespacedName{Namespace: "app-ns", Name: "policy-2"}}}
		assert.DeepEqual(subT, expected, requests)
	})
}
