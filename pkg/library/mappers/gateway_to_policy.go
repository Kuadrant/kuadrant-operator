package mappers

import (
	"context"
	"fmt"

	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
)

func NewGatewayToPolicyEventMapper(policyType kuadrantgatewayapi.PolicyType, o ...MapperOption) *GatewayToPolicyEventMapper {
	return &GatewayToPolicyEventMapper{
		policyType: policyType,
		opts:       Apply(o...),
	}
}

type GatewayToPolicyEventMapper struct {
	opts       MapperOptions
	policyType kuadrantgatewayapi.PolicyType
}

func (m *GatewayToPolicyEventMapper) Map(ctx context.Context, obj client.Object) []reconcile.Request {
	logger := m.opts.Logger.WithValues("gateway", client.ObjectKeyFromObject(obj))
	gateway, ok := obj.(*gatewayapiv1.Gateway)
	if !ok {
		logger.Info("cannot map gateway related event", "error", fmt.Sprintf("%T is not a *gatewayapiv1beta1.Gateway", obj))
		return []reconcile.Request{}
	}
	routeList := &gatewayapiv1.HTTPRouteList{}
	if err := m.opts.Client.List(ctx, routeList); err != nil {
		logger.V(1).Error(err, "unable to list HTTPRoutes")
		return []reconcile.Request{}
	}

	policies, err := m.policyType.GetList(ctx, m.opts.Client)
	if err != nil {
		logger.V(1).Error(err, "unable to list policies")
		return []reconcile.Request{}
	}

	if len(policies) == 0 {
		logger.V(1).Info("no kuadrant policy possibly affected by the gateway related event")
		return []reconcile.Request{}
	}

	topology, err := kuadrantgatewayapi.NewBasicTopology(
		kuadrantgatewayapi.WithGateways([]*gatewayapiv1.Gateway{gateway}),
		kuadrantgatewayapi.WithRoutes(utils.Map(routeList.Items, ptr.To[gatewayapiv1.HTTPRoute])),
		kuadrantgatewayapi.WithPolicies(policies),
		kuadrantgatewayapi.WithLogger(logger),
	)
	if err != nil {
		logger.V(1).Error(err, "unable to build topology for gateway")
		return []reconcile.Request{}
	}

	index := kuadrantgatewayapi.NewTopologyIndexes(topology)
	return utils.Map(index.PoliciesFromGateway(gateway), func(p kuadrantgatewayapi.Policy) reconcile.Request {
		policyKey := client.ObjectKeyFromObject(p)
		logger.V(1).Info("new request", "policy key", policyKey)
		return reconcile.Request{NamespacedName: policyKey}
	})
}
