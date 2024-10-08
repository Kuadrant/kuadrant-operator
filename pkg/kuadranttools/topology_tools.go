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

func TopologyFromGateway(ctx context.Context, cl client.Client, gw *gatewayapiv1.Gateway, policyType kuadrantgatewayapi.PolicyType) (*kuadrantgatewayapi.Topology, error) {
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
	logger.V(1).Info("TopologyFromGateway: list httproutes from gateway",
		"gateway", client.ObjectKeyFromObject(gw),
		"#HTTPRoutes", len(routeList.Items),
		"err", err)
	if err != nil {
		return nil, err
	}

	// Get all the policyKind policies
	policies, err := policyType.GetList(ctx, cl)
	logger.V(1).Info("TopologyFromGateway: list policies",
		"#policies", len(policies),
		"err", err)
	if err != nil {
		return nil, err
	}

	return kuadrantgatewayapi.NewTopology(
		kuadrantgatewayapi.WithGateways([]*gatewayapiv1.Gateway{gw}),
		kuadrantgatewayapi.WithRoutes(utils.Map(routeList.Items, ptr.To[gatewayapiv1.HTTPRoute])),
		kuadrantgatewayapi.WithPolicies(policies),
		kuadrantgatewayapi.WithLogger(logger),
	)
}

func TopologyForPolicies(ctx context.Context, cl client.Client, policyType kuadrantgatewayapi.PolicyType) (*kuadrantgatewayapi.Topology, error) {
	logger, err := logr.FromContext(ctx)
	if err != nil {
		return nil, err
	}

	gatewayList := &gatewayapiv1.GatewayList{}
	err = cl.List(
		ctx,
		gatewayList)
	logger.V(1).Info("TopologyForPolicies: list all gateways",
		"#Gateways", len(gatewayList.Items),
		"err", err)
	if err != nil {
		return nil, err
	}

	routeList := &gatewayapiv1.HTTPRouteList{}
	err = cl.List(
		ctx,
		routeList)
	logger.V(1).Info("TopologyForPolicies: list all httproutes",
		"#HTTPRoutes", len(routeList.Items),
		"err", err)
	if err != nil {
		return nil, err
	}

	policies, err := policyType.GetList(ctx, cl)
	logger.V(1).Info("TopologyForPolicies: list policies",
		"#policies", len(policies),
		"err", err)
	if err != nil {
		return nil, err
	}

	return kuadrantgatewayapi.NewTopology(
		kuadrantgatewayapi.WithGateways(utils.Map(gatewayList.Items, ptr.To[gatewayapiv1.Gateway])),
		kuadrantgatewayapi.WithRoutes(utils.Map(routeList.Items, ptr.To[gatewayapiv1.HTTPRoute])),
		kuadrantgatewayapi.WithPolicies(policies),
		kuadrantgatewayapi.WithLogger(logger),
	)
}
