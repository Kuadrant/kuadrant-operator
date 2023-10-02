package common

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
)

// HTTPRouteToParentGatewaysEventMapper is an EventHandler that maps HTTPRoute events to gateway events,
// by going through the parentRefs of the route
type HTTPRouteToParentGatewaysEventMapper struct {
	Logger logr.Logger
}

func (m *HTTPRouteToParentGatewaysEventMapper) Map(_ context.Context, obj client.Object) []reconcile.Request {
	logger := m.Logger.WithValues("object", client.ObjectKeyFromObject(obj))

	route, ok := obj.(*gatewayapiv1.HTTPRoute)
	if !ok {
		logger.Error(fmt.Errorf("%T is not a *gatewayapiv1.HTTPRoute", obj), "cannot map")
		return []reconcile.Request{}
	}

	requests := make([]reconcile.Request, 0)

	for _, parentRef := range route.Spec.ParentRefs {
		// skips if parentRef is not a Gateway
		if !IsParentGateway(parentRef) {
			continue
		}

		ns := route.Namespace
		if parentRef.Namespace != nil {
			ns = string(*parentRef.Namespace)
		}

		nn := types.NamespacedName{Name: string(parentRef.Name), Namespace: ns}
		logger.V(1).Info("map", " gateway", nn)

		requests = append(requests, reconcile.Request{NamespacedName: nn})
	}

	return requests
}
