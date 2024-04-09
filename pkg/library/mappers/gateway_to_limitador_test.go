//go:build unit

package mappers

import (
	"context"
	"testing"

	"gotest.tools/assert"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kuadrant/kuadrant-operator/pkg/common"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
	"github.com/kuadrant/kuadrant-operator/pkg/log"
)

func TestGatewayToLimitadorEventMapper(t *testing.T) {
	m := NewGatewayToLimitadorEventMapper(WithLogger(log.NewLogger()))

	t.Run("not gateway related event", func(subT *testing.T) {
		requests := m.Map(context.TODO(), &gatewayapiv1.HTTPRoute{})
		assert.DeepEqual(subT, []reconcile.Request{}, requests)
	})

	t.Run("gateway not assigned to kuadrant", func(subT *testing.T) {
		requests := m.Map(context.TODO(), &gatewayapiv1.Gateway{})
		assert.DeepEqual(subT, []reconcile.Request{}, requests)
	})

	t.Run("gateway assigned to kuadrant", func(subT *testing.T) {
		ns := "my-namespace"
		gateway := &gatewayapiv1.Gateway{}
		kuadrant.AnnotateObject(gateway, ns)
		requests := m.Map(context.TODO(), gateway)
		//assert.Assert(subT, len(requests) == 0, "expected", 1, "got", len(requests))
		assert.Equal(subT, len(requests), 1)
		assert.DeepEqual(subT, requests[0],
			reconcile.Request{NamespacedName: common.LimitadorObjectKey(ns)},
		)
	})
}
