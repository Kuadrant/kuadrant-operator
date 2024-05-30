package wasm

import (
	"context"

	"github.com/go-logr/logr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	"github.com/kuadrant/kuadrant-operator/pkg/library/fieldindexers"
	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
)

func TopologyIndexesFromGateway(ctx context.Context, cl client.Client, gw *gatewayapiv1.Gateway) (*kuadrantgatewayapi.TopologyIndexes, error) {
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

	rlpList := &kuadrantv1beta2.RateLimitPolicyList{}
	// Get all the rate limit policies
	err = cl.List(ctx, rlpList)
	logger.V(1).Info("topologyIndexesFromGateway: list rate limit policies",
		"#RLPS", len(rlpList.Items),
		"err", err)
	if err != nil {
		return nil, err
	}

	policies := utils.Map(rlpList.Items, func(p kuadrantv1beta2.RateLimitPolicy) kuadrantgatewayapi.Policy { return &p })

	t, err := kuadrantgatewayapi.NewTopology(
		kuadrantgatewayapi.WithGateways([]*gatewayapiv1.Gateway{gw}),
		kuadrantgatewayapi.WithRoutes(utils.Map(routeList.Items, ptr.To)),
		kuadrantgatewayapi.WithPolicies(policies),
		kuadrantgatewayapi.WithLogger(logger),
	)
	if err != nil {
		return nil, err
	}

	return kuadrantgatewayapi.NewTopologyIndexes(t), nil
}
