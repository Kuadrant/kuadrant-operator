package mappers

import (
	"context"
	"fmt"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
)

// HTTPRouteToParentGatewaysEventMapper is an EventHandler that maps HTTPRoute events to gateway events,
// by going through the parentRefs of the route
type HTTPRouteToParentGatewaysEventMapper struct {
	opts MapperOptions
}

func NewHTTPRouteToParentGatewaysEventMapper(o ...MapperOption) *HTTPRouteToParentGatewaysEventMapper {
	return &HTTPRouteToParentGatewaysEventMapper{opts: Apply(o...)}
}

func (m *HTTPRouteToParentGatewaysEventMapper) Map(_ context.Context, obj client.Object) []reconcile.Request {
	logger := m.opts.Logger.WithValues("object", client.ObjectKeyFromObject(obj))

	route, ok := obj.(*gatewayapiv1.HTTPRoute)
	if !ok {
		logger.Error(fmt.Errorf("%T is not a *gatewayapiv1.HTTPRoute", obj), "cannot map")
		return []reconcile.Request{}
	}

	return utils.Map(kuadrantgatewayapi.GetRouteAcceptedGatewayParentKeys(route), func(key client.ObjectKey) reconcile.Request {
		logger.V(1).Info("new gateway event", "key", key.String())
		return reconcile.Request{NamespacedName: key}
	})
}
