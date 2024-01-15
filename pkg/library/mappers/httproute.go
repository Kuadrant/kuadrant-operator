package mappers

import (
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
)

func NewHTTPRouteEventMapper(o ...MapperOption) EventMapper {
	return &httpRouteEventMapper{opts: Apply(o...)}
}

var _ EventMapper = &httpRouteEventMapper{}

type httpRouteEventMapper struct {
	opts MapperOptions
}

func (m *httpRouteEventMapper) MapToPolicy(obj client.Object, policyKind utils.Referrer) []reconcile.Request {
	logger := m.opts.Logger.WithValues("httproute", client.ObjectKeyFromObject(obj))

	httpRoute, ok := obj.(*gatewayapiv1.HTTPRoute)
	if !ok {
		logger.Info("cannot map httproute event to kuadrant policy", "error", fmt.Sprintf("%T is not a *gatewayapiv1beta1.HTTPRoute", obj))
		return []reconcile.Request{}
	}

	requests := make([]reconcile.Request, 0)

	for _, policyKey := range utils.BackReferencesFromObject(httpRoute, policyKind) {
		logger.V(1).Info("kuadrant policy possibly affected by the httproute related event found", policyKind.Kind(), policyKey)
		requests = append(requests, reconcile.Request{NamespacedName: policyKey})
	}

	if len(requests) == 0 {
		logger.V(1).Info("no kuadrant policy possibly affected by the httproute related event")
	}

	return requests
}
