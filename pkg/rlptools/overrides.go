package rlptools

import (
	"slices"

	"github.com/samber/lo"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	kuadrantv1beta2 "github.com/kuadrant/kuadrant-operator/api/v1beta2"
	kuadrantgatewayapi "github.com/kuadrant/kuadrant-operator/pkg/library/gatewayapi"
)

// ApplyOverrides applies the overrides defined in the RateLimitPolicies attached to the gateway policies for a given
// gateway, and returns a new topology with all policies overridden as applicable.
func ApplyOverrides(topology *kuadrantgatewayapi.Topology, gateway *gatewayapiv1.Gateway) (*kuadrantgatewayapi.Topology, error) {
	gatewayNode, ok := lo.Find(topology.Gateways(), func(g kuadrantgatewayapi.GatewayNode) bool {
		return g.ObjectKey() == client.ObjectKeyFromObject(gateway)
	})
	if !ok || len(gatewayNode.AttachedPolicies()) == 0 {
		return topology, nil
	}

	overridePolicies := lo.FilterMap(gatewayNode.AttachedPolicies(), func(policy kuadrantgatewayapi.Policy, _ int) (*kuadrantv1beta2.RateLimitPolicy, bool) {
		rlp, ok := policy.(*kuadrantv1beta2.RateLimitPolicy)
		if !ok || rlp.Spec.Overrides == nil {
			return nil, false
		}
		return rlp, true
	})

	if len(overridePolicies) == 0 {
		return topology, nil
	}

	overriddenPolicies := lo.Map(overridePolicies, func(p *kuadrantv1beta2.RateLimitPolicy, _ int) kuadrantgatewayapi.Policy { return p })

	for _, route := range topology.Routes() {
		if !slices.Contains(kuadrantgatewayapi.GetRouteAcceptedGatewayParentKeys(route.HTTPRoute), client.ObjectKeyFromObject(gateway)) {
			overriddenPolicies = append(overriddenPolicies, route.AttachedPolicies()...)
			continue
		}

		for _, policy := range route.AttachedPolicies() {
			overriddenPolicy := policy.DeepCopyObject().(*kuadrantv1beta2.RateLimitPolicy)
			overriddenPolicy.Spec.CommonSpec().Limits = overridePolicies[0].Spec.Overrides.Limits
			overriddenPolicies = append(overriddenPolicies, overriddenPolicy)
		}
	}

	return kuadrantgatewayapi.NewTopology(
		kuadrantgatewayapi.WithGateways(lo.Map(topology.Gateways(), func(g kuadrantgatewayapi.GatewayNode, _ int) *gatewayapiv1.Gateway { return g.Gateway })),
		kuadrantgatewayapi.WithRoutes(lo.Map(topology.Routes(), func(r kuadrantgatewayapi.RouteNode, _ int) *gatewayapiv1.HTTPRoute { return r.HTTPRoute })),
		kuadrantgatewayapi.WithPolicies(overriddenPolicies),
		kuadrantgatewayapi.WithLogger(topology.Logger),
	)
}
