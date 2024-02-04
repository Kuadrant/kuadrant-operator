package events

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kuadrant/kuadrant-operator/pkg/common"
)

// GatewayEventMapper is an EventHandler that maps Gateway object events to policy events.
type GatewayEventMapper struct {
	Logger           logr.Logger
	PolicyRefsConfig common.PolicyRefsConfig
	PolicyKind       string
}

func NewGatewayEventMapper(logger logr.Logger, policyRefsConfig common.PolicyRefsConfig, policyKind string) *GatewayEventMapper {
	return &GatewayEventMapper{
		Logger:           logger.WithName("GatewayEventMapper"),
		PolicyRefsConfig: policyRefsConfig,
		PolicyKind:       policyKind,
	}
}

func (m *GatewayEventMapper) MapToPolicy(ctx context.Context, obj client.Object) []reconcile.Request {
	return m.mapToPolicyRequest(ctx, obj, m.PolicyKind, m.PolicyRefsConfig)
}

func (m *GatewayEventMapper) mapToPolicyRequest(_ context.Context, obj client.Object, policyKind string, policyRefsConfig common.PolicyRefsConfig) []reconcile.Request {
	logger := m.Logger.V(1).WithValues("object", client.ObjectKeyFromObject(obj))

	gateway, ok := obj.(*gatewayapiv1.Gateway)
	if !ok {
		logger.Info("mapToPolicyRequest:", "error", fmt.Sprintf("%T is not a *gatewayapiv1.Gateway", obj))
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
