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
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
)

func NewHTTPRouteEventMapper(o ...MapperOption) EventMapper {
	return &httpRouteEventMapper{opts: Apply(o...)}
}

var _ EventMapper = &httpRouteEventMapper{}

type httpRouteEventMapper struct {
	opts MapperOptions
}

func (m *httpRouteEventMapper) MapToPolicy(ctx context.Context, obj client.Object, policyKind kuadrantgatewayapi.Policy) []reconcile.Request {
	logger := m.opts.Logger.WithValues("httproute", client.ObjectKeyFromObject(obj))
	requests := make([]reconcile.Request, 0)
	httpRoute, ok := obj.(*gatewayapiv1.HTTPRoute)
	if !ok {
		logger.Info("cannot map httproute event to kuadrant policy", "error", fmt.Sprintf("%T is not a *gatewayapiv1beta1.HTTPRoute", obj))
		return []reconcile.Request{}
	}

	gatewayKeys := kuadrantgatewayapi.GetRouteAcceptedGatewayParentKeys(httpRoute)

	for _, gatewayKey := range gatewayKeys {
		gateway := &gatewayapiv1.Gateway{}
		err := m.opts.Client.Get(ctx, gatewayKey, gateway)
		if err != nil {
			logger.Info("cannot get gateway", "error", err)
			continue
		}

		routeList := &gatewayapiv1.HTTPRouteList{}
		fields := client.MatchingFields{fieldindexers.HTTPRouteGatewayParentField: client.ObjectKeyFromObject(gateway).String()}
		if err = m.opts.Client.List(ctx, routeList, fields); err != nil {
			logger.Info("cannot list httproutes", "error", err)
			continue
		}
		policies := policyKind.List(ctx, m.opts.Client, obj.GetNamespace())
		if len(policies) == 0 {
			logger.Info("no kuadrant policy possibly affected by the gateway related event")
			continue
		}
		topology, err := kuadrantgatewayapi.NewTopology(
			kuadrantgatewayapi.WithGateways([]*gatewayapiv1.Gateway{gateway}),
			kuadrantgatewayapi.WithRoutes(utils.Map(routeList.Items, ptr.To[gatewayapiv1.HTTPRoute])),
			kuadrantgatewayapi.WithPolicies(policies),
			kuadrantgatewayapi.WithLogger(logger),
		)
		if err != nil {
			logger.Info("unable to build topology for gateway", "error", err)
			continue
		}
		index := kuadrantgatewayapi.NewTopologyIndexes(topology)
		data := utils.Map(index.PoliciesFromGateway(gateway), func(p kuadrantgatewayapi.Policy) reconcile.Request {
			policyKey := client.ObjectKeyFromObject(p)
			logger.V(1).Info("kuadrant policy possibly affected by the gateway related event found")
			return reconcile.Request{NamespacedName: policyKey}
		})
		requests = append(requests, data...)
	}

	if len(requests) != 0 {
		return requests
	}

	policyKey, err := kuadrant.DirectReferencesFromObject(httpRoute, policyKind)
	if err != nil {
		logger.Info("could not create direct reference from object", "error", err)
		return requests
	}
	requests = append(requests, reconcile.Request{NamespacedName: policyKey})
	return requests
}
