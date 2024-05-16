package controllers

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
	"github.com/kuadrant/kuadrant-operator/pkg/library/kuadrant"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
)

const (
	HTTPRouteGatewayParentField = ".metadata.parentRefs.gateway"
)

func BuildTopology(ctx context.Context, ks8sClient client.Client, gw *gatewayapiv1.Gateway, policyKind string, listPolicyKind client.ObjectList) (*kuadrantgatewayapi.TopologyIndexes, error) {
	logger, err := logr.FromContext(ctx)
	if err != nil {
		return nil, err
	}

	routeList := &gatewayapiv1.HTTPRouteList{}
	// Get all the routes having the gateway as parent
	err = ks8sClient.List(ctx, routeList, client.MatchingFields{HTTPRouteGatewayParentField: client.ObjectKeyFromObject(gw).String()})
	logger.V(1).Info("list routes by gateway", "#routes", len(routeList.Items), "err", err)
	if err != nil {
		return nil, err
	}

	policies, err := GetPoliciesByKind(ctx, ks8sClient, policyKind, listPolicyKind)
	if err != nil {
		return nil, err
	}

	t, err := kuadrantgatewayapi.NewTopology(
		kuadrantgatewayapi.WithGateways([]*gatewayapiv1.Gateway{gw}),
		kuadrantgatewayapi.WithRoutes(utils.Map(routeList.Items, ptr.To[gatewayapiv1.HTTPRoute])),
		kuadrantgatewayapi.WithPolicies(policies),
		kuadrantgatewayapi.WithLogger(logger),
	)
	if err != nil {
		return nil, err
	}

	return kuadrantgatewayapi.NewTopologyIndexes(t), nil
}

func GetPoliciesByKind(ctx context.Context, ks8sClient client.Client, policyKind string, listKind client.ObjectList) ([]kuadrantgatewayapi.Policy, error) {
	logger, _ := logr.FromContext(ctx)
	logger = logger.WithValues("kind", policyKind)

	// Get all policies of the given kind
	err := ks8sClient.List(ctx, listKind)
	policyList, ok := listKind.(kuadrant.PolicyList)
	if !ok {
		return nil, fmt.Errorf("%T is not a kuadrant.PolicyList", listKind)
	}
	logger.V(1).Info("list policies by kind", "#policies", len(policyList.GetItems()), "err", err)
	if err != nil {
		return nil, err
	}

	return utils.Map(policyList.GetItems(), func(p kuadrant.Policy) kuadrantgatewayapi.Policy { return p }), nil
}
