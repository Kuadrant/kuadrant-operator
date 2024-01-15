package mappers

import (
	"testing"

	"gotest.tools/assert"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
	"github.com/kuadrant/kuadrant-operator/pkg/log"
)

func TestNewGatewayEventMapper(t *testing.T) {
	em := NewGatewayEventMapper(WithLogger(log.NewLogger()))

	t.Run("not gateway related event", func(subT *testing.T) {
		requests := em.MapToPolicy(&gatewayapiv1.HTTPRoute{}, &utils.PolicyKindStub{})
		assert.DeepEqual(subT, []reconcile.Request{}, requests)
	})

	t.Run("gateway related event - no requests", func(subT *testing.T) {
		requests := em.MapToPolicy(&gatewayapiv1.Gateway{}, &utils.PolicyKindStub{})
		assert.DeepEqual(subT, []reconcile.Request{}, requests)
	})

	t.Run("gateway related event - requests", func(subT *testing.T) {
		gateway := &gatewayapiv1.Gateway{}
		gateway.SetAnnotations(map[string]string{"kuadrant.io/testpolicies": `[{"Namespace":"app-ns","Name":"policy-1"},{"Namespace":"app-ns","Name":"policy-2"}]`})
		requests := em.MapToPolicy(gateway, &utils.PolicyKindStub{})
		expected := []reconcile.Request{{NamespacedName: types.NamespacedName{Namespace: "app-ns", Name: "policy-1"}}, {NamespacedName: types.NamespacedName{Namespace: "app-ns", Name: "policy-2"}}}
		assert.DeepEqual(subT, expected, requests)
	})
}
