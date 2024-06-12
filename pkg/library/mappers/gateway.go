package mappers

import (
	"context"
	"fmt"

	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kuadrant/kuadrant-operator/pkg/library/fieldindexers"
	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
)

func NewGatewayEventMapper(o ...MapperOption) EventMapper {
	return &gatewayEventMapper{opts: Apply(o...)}
}

var _ EventMapper = &gatewayEventMapper{}

type gatewayEventMapper struct {
	opts MapperOptions
}

func (m *gatewayEventMapper) MapToPolicy(ctx context.Context, obj client.Object, policyKind kuadrantgatewayapi.Policy) []reconcile.Request {
	logger := m.opts.Logger.WithValues("gateway", client.ObjectKeyFromObject(obj))
	gateway, ok := obj.(*gatewayapiv1.Gateway)
	if !ok {
		logger.Info("cannot map gateway related event to kuadrant policy", "error", fmt.Sprintf("%T is not a *gatewayapiv1beta1.Gateway", obj))
		return []reconcile.Request{}
	}
	routeList := &gatewayapiv1.HTTPRouteList{}
	fields := client.MatchingFields{fieldindexers.HTTPRouteGatewayParentField: client.ObjectKeyFromObject(gateway).String()}
	if err := m.opts.Client.List(ctx, routeList, fields); err != nil {
		logger.V(1).Error(err, "unable to list HTTPRoutes")
		return []reconcile.Request{}
	}

	policies := policyKind.List(ctx, m.opts.Client, obj.GetNamespace())
	if len(policies) == 0 {
		logger.V(1).Info("no kuadrant policy possibly affected by the gateway related event")
		return []reconcile.Request{}
	}

	topology, err := kuadrantgatewayapi.NewTopology(
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
		logger.V(1).Info("kuadrant policy possibly affected by the gateway related event found")
		return reconcile.Request{NamespacedName: policyKey}
	})
}
