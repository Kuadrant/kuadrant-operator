package controllers

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayapiv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	kuadrantv1beta1 "github.com/kuadrant/kuadrant-operator/api/v1beta1"
	"github.com/kuadrant/kuadrant-operator/pkg/common"
)

// GatewayRateLimitPolicyEventMapper is an EventHandler that maps:
// RLP events targeting a Gateway TO all the RLPs configuring that same gateway
type GatewayRateLimitPolicyEventMapper struct {
	Logger logr.Logger
	Client client.Client
}

func (h *GatewayRateLimitPolicyEventMapper) MapRouteRateLimitPolicy(obj client.Object) []reconcile.Request {
	rlp, ok := obj.(*kuadrantv1beta1.RateLimitPolicy)
	if !ok {
		h.Logger.V(1).Info("MapRouteRateLimitPolicy: RLP not received", "error", fmt.Sprintf("%T is not a *kuadrantv1beta1.RateLimitPolicy", obj))
		return []reconcile.Request{}
	}

	// filter out all RLP not targeting a gateway
	if !common.IsTargetRefGateway(rlp.Spec.TargetRef) {
		return []reconcile.Request{}
	}

	gwKey := rlp.TargetKey()
	gateway := &gatewayapiv1beta1.Gateway{}
	err := h.Client.Get(context.TODO(), gwKey, gateway)
	h.Logger.V(1).Info("MapRouteRateLimitPolicy", "fetch gateway", gwKey, "err", err)
	if err != nil {
		if apierrors.IsNotFound(err) {
			h.Logger.V(1).Info("MapRouteRateLimitPolicy: targetRef Gateway not found")
		}
		return []reconcile.Request{}
	}

	gw := common.GatewayWrapper{Gateway: gateway, PolicyRefsConfig: &common.KuadrantRateLimitPolicyRefsConfig{}}

	requests := make([]reconcile.Request, 0)

	for _, rlpKey := range gw.PolicyRefs() {
		h.Logger.V(1).Info("MapRouteRateLimitPolicy", "ratelimitpolicy", rlpKey)
		requests = append(requests, reconcile.Request{
			NamespacedName: rlpKey,
		})
	}

	return requests
}
