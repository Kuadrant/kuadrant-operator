package apim

import (
	"fmt"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	"github.com/kuadrant/kuadrant-controller/pkg/rlptools"
)

// GatewayEventMapper is an EventHandler that maps Gateway object events to policy events.
type GatewayEventMapper struct {
	Logger logr.Logger
}

func (h *GatewayEventMapper) MapToRateLimitPolicy(obj client.Object) []reconcile.Request {
	gateway, ok := obj.(*gatewayapiv1alpha2.Gateway)
	if !ok {
		h.Logger.V(1).Info("MapToRateLimitPolicy: gateway not received", "error", fmt.Sprintf("%T is not a *gatewayapiv1alpha2.Gateway", obj))
		return []reconcile.Request{}
	}

	gw := rlptools.GatewayWrapper{Gateway: gateway}

	requests := make([]reconcile.Request, 0)

	for _, rlpKey := range gw.RLPRefs() {
		h.Logger.V(1).Info("MapToRateLimitPolicy", "ratelimitpolicy", rlpKey)
		requests = append(requests, reconcile.Request{
			NamespacedName: rlpKey,
		})
	}

	return requests
}
