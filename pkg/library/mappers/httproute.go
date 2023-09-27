package mappers

import (
	"fmt"

	"github.com/kuadrant/kuadrant-operator/pkg/library/common"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayapiv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"
)

// TODO(@guicassolato): unit test
func NewHTTPRouteEventMapper(o ...mapperOption) EventMapper {
	return &httpRouteEventMapper{opts: apply(o...)}
}

type httpRouteEventMapper struct {
	opts mapperOptions
}

// TODO(@guicassolato): unit test
func (m *httpRouteEventMapper) MapToPolicy(obj client.Object, policyKind common.Referrer) []reconcile.Request {
	logger := m.opts.logger.WithValues("httproute", client.ObjectKeyFromObject(obj))

	httpRoute, ok := obj.(*gatewayapiv1beta1.HTTPRoute)
	if !ok {
		logger.Info("cannot map httproute event to kuadrant policy", "error", fmt.Sprintf("%T is not a *gatewayapiv1beta1.HTTPRoute", obj))
		return []reconcile.Request{}
	}

	requests := make([]reconcile.Request, 0)

	for _, policyKey := range common.BackReferencesFromObject(httpRoute, policyKind) {
		logger.V(1).Info("kuadrant policy possibly affected by the httproute related event found", policyKind.Kind(), policyKey)
		requests = append(requests, reconcile.Request{NamespacedName: policyKey})
	}

	if len(requests) == 0 {
		logger.V(1).Info("no kuadrant policy possibly affected by the httproute related event")
	}

	return requests
}
