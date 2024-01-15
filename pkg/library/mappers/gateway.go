package mappers

import (
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayapiv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
)

// TODO(@guicassolato): unit test
func NewGatewayEventMapper(o ...mapperOption) EventMapper {
	return &gatewayEventMapper{opts: apply(o...)}
}

type gatewayEventMapper struct {
	opts mapperOptions
}

// TODO(@guicassolato): unit test
func (m *gatewayEventMapper) MapToPolicy(obj client.Object, policyKind utils.Referrer) []reconcile.Request {
	logger := m.opts.logger.WithValues("gateway", client.ObjectKeyFromObject(obj))

	gateway, ok := obj.(*gatewayapiv1beta1.Gateway)
	if !ok {
		logger.Info("cannot map gateway related event to kuadrant policy", "error", fmt.Sprintf("%T is not a *gatewayapiv1beta1.Gateway", obj))
		return []reconcile.Request{}
	}

	requests := make([]reconcile.Request, 0)

	for _, policyKey := range utils.BackReferencesFromObject(gateway, policyKind) {
		logger.V(1).Info("kuadrant policy possibly affected by the gateway related event found", policyKind.Kind(), policyKey)
		requests = append(requests, reconcile.Request{NamespacedName: policyKey})
	}

	if len(requests) == 0 {
		logger.V(1).Info("no kuadrant policy possibly affected by the gateway related event")
	}

	return requests
}
