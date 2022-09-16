package apim

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"

	apimv1alpha1 "github.com/kuadrant/kuadrant-controller/apis/apim/v1alpha1"
	"github.com/kuadrant/kuadrant-controller/pkg/rlptools"
)

// GatewayRateLimitPolicyEventMapper is an EventHandler that maps:
// RLP events targeting a Gateway TO all the RLPs configuring that same gateway
type GatewayRateLimitPolicyEventMapper struct {
	Logger logr.Logger
	Client client.Client
}

func (h *GatewayRateLimitPolicyEventMapper) MapRouteRateLimitPolicy(obj client.Object) []reconcile.Request {
	rlp, ok := obj.(*apimv1alpha1.RateLimitPolicy)
	if !ok {
		h.Logger.V(1).Info("MapRouteRateLimitPolicy: RLP not received", "error", fmt.Sprintf("%T is not a *apimv1alpha1.RateLimitPolicy", obj))
		return []reconcile.Request{}
	}

	// filter out all RLP not targeting a gateway
	if !rlp.IsForGateway() {
		return []reconcile.Request{}
	}

	gwKey := rlp.TargetKey()
	gateway := &gatewayapiv1alpha2.Gateway{}
	err := h.Client.Get(context.TODO(), gwKey, gateway)
	h.Logger.V(1).Info("MapRouteRateLimitPolicy", "fetch gateway", gwKey, "err", err)
	if err != nil {
		if apierrors.IsNotFound(err) {
			h.Logger.V(1).Info("MapRouteRateLimitPolicy: targetRef Gateway not found")
		}
		return []reconcile.Request{}
	}

	gw := rlptools.GatewayWrapper{Gateway: gateway}

	requests := make([]reconcile.Request, 0)

	for _, rlpKey := range gw.RLPRefs() {
		h.Logger.V(1).Info("MapRouteRateLimitPolicy", "ratelimitpolicy", rlpKey)
		requests = append(requests, reconcile.Request{
			NamespacedName: rlpKey,
		})
	}

	return requests
}
