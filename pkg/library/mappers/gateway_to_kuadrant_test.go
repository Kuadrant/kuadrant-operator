//go:build unit

package mappers

import (
	"context"
	"testing"

	"gotest.tools/assert"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
	"github.com/kuadrant/kuadrant-operator/pkg/log"
)

func TestGatewayToKuadrantEventMapper(t *testing.T) {
	m := NewGatewayToKuadrantEventMapper(WithLogger(log.NewLogger()))

	t.Run("not gateway related event", func(subT *testing.T) {
		requests := m.Map(context.TODO(), &gatewayapiv1.HTTPRoute{})
		assert.DeepEqual(subT, []reconcile.Request{}, requests)
	})

	t.Run("gateway not assigned to kuadrant", func(subT *testing.T) {
		requests := m.Map(context.TODO(), &gatewayapiv1.Gateway{})
		assert.DeepEqual(subT, []reconcile.Request{}, requests)
	})

	t.Run("gateway assigned to kuadrant", func(subT *testing.T) {
		name := "my-name"
		ns := "my-namespace"
		gateway := &gatewayapiv1.Gateway{}
		kuadrant.AnnotateObject(gateway, name, ns)
		requests := m.Map(context.TODO(), gateway)
		assert.Equal(subT, len(requests), 1)
		assert.DeepEqual(subT, requests[0],
			reconcile.Request{NamespacedName: client.ObjectKey{
				Name:      name,
				Namespace: ns,
			}},
		)
	})
}
