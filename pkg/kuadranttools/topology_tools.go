package kuadranttools

import (
	"context"

	"github.com/go-logr/logr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kuadrant/kuadrant-operator/pkg/library/fieldindexers"
	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
)

func TopologyFromGateway(ctx context.Context, cl client.Client, gw *gatewayapiv1.Gateway, policyKind kuadrantgatewayapi.Policy) (*kuadrantgatewayapi.Topology, error) {
	logger, err := logr.FromContext(ctx)
	if err != nil {
		return nil, err
	}

	routeList := &gatewayapiv1.HTTPRouteList{}
	// Get all the routes having the gateway as parent
	err = cl.List(
		ctx,
		routeList,
		client.MatchingFields{
			fieldindexers.HTTPRouteGatewayParentField: client.ObjectKeyFromObject(gw).String(),
		})
	logger.V(1).Info("topologyIndexesFromGateway: list httproutes from gateway",
		"gateway", client.ObjectKeyFromObject(gw),
		"#HTTPRoutes", len(routeList.Items),
		"err", err)
	if err != nil {
		return nil, err
	}

	// Get all the policyKind policies
	policies := policyKind.List(ctx, cl, "")
	logger.V(1).Info("topologyIndexesFromGateway: list policies",
		"#policies", len(policies),
		"err", err)

	return kuadrantgatewayapi.NewTopology(
		kuadrantgatewayapi.WithGateways([]*gatewayapiv1.Gateway{gw}),
		kuadrantgatewayapi.WithRoutes(utils.Map(routeList.Items, ptr.To[gatewayapiv1.HTTPRoute])),
		kuadrantgatewayapi.WithPolicies(policies),
		kuadrantgatewayapi.WithLogger(logger),
	)
}
