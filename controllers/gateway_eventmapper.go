package controllers

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayapiv1beta1 "sigs.k8s.io/gateway-api/apis/v1beta1"

	"github.com/kuadrant/kuadrant-operator/pkg/common"
)

// GatewayEventMapper is an EventHandler that maps Gateway object events to policy events.
type GatewayEventMapper struct {
	Logger logr.Logger
}

func (m *GatewayEventMapper) MapToRateLimitPolicy(_ context.Context, obj client.Object) []reconcile.Request {
	return m.mapToPolicyRequest(obj, "ratelimitpolicy", &common.KuadrantRateLimitPolicyRefsConfig{})
}

func (m *GatewayEventMapper) MapToAuthPolicy(_ context.Context, obj client.Object) []reconcile.Request {
	return m.mapToPolicyRequest(obj, "authpolicy", &common.KuadrantAuthPolicyRefsConfig{})
}

func (m *GatewayEventMapper) mapToPolicyRequest(obj client.Object, policyKind string, policyRefsConfig common.PolicyRefsConfig) []reconcile.Request {
	logger := m.Logger.V(1).WithValues("object", client.ObjectKeyFromObject(obj))

	gateway, ok := obj.(*gatewayapiv1beta1.Gateway)
	if !ok {
		logger.Info("mapToPolicyRequest:", "error", fmt.Sprintf("%T is not a *gatewayapiv1beta1.Gateway", obj))
		return []reconcile.Request{}
	}

	gw := common.GatewayWrapper{Gateway: gateway, PolicyRefsConfig: policyRefsConfig}

	requests := make([]reconcile.Request, 0)

	for _, policyKey := range gw.PolicyRefs() {
		logger.Info("mapToPolicyRequest", policyKind, policyKey)
		requests = append(requests, reconcile.Request{NamespacedName: policyKey})
	}

	return requests
}
