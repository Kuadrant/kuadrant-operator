//go:build unit

package gatewayapi

import (
	"strings"
	"testing"

	"gotest.tools/assert"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kuadrant/kuadrant-operator/pkg/log"
)

func TestTopologyIndexes_PoliciesFromGateway(t *testing.T) {
	t.Run("empty topology", func(subT *testing.T) {
		t, err := NewBasicTopology(WithLogger(log.NewLogger()))
		assert.NilError(subT, err)
		topologyIndexes := NewTopologyIndexes(t)

		policies := topologyIndexes.PoliciesFromGateway(testBasicGateway("gw1", NS))
		assert.Equal(subT, len(policies), 0)
	})

	t.Run("unknown gateway", func(subT *testing.T) {
		gateways := []*gatewayapiv1.Gateway{
			testBasicGateway("gw1", NS),
			testBasicGateway("gw2", NS),
		}

		t, err := NewBasicTopology(WithGateways(gateways), WithLogger(log.NewLogger()))
		assert.NilError(subT, err)
		topologyIndexes := NewTopologyIndexes(t)

		policies := topologyIndexes.PoliciesFromGateway(testBasicGateway("unknown", NS))
		assert.Equal(subT, len(policies), 0)
	})

	t.Run("invalid gateway is skipped", func(subT *testing.T) {
		// route1 -> gw1
		// policy1 -> gw1
		// policy2 -> route1

		invalidGateway := testInvalidGateway("gw1", NS)
		gateways := []*gatewayapiv1.Gateway{invalidGateway}

		route1 := testBasicRoute("route1", NS, invalidGateway)
		routes := []*gatewayapiv1.HTTPRoute{route1}

		gwPolicy := testBasicGatewayPolicy("policy1", NS, invalidGateway)
		routePolicy := testBasicRoutePolicy("policy2", NS, route1)
		policies := []Policy{gwPolicy, routePolicy}

		t, err := NewValidTopology(
			WithGateways(gateways),
			WithRoutes(routes),
			WithPolicies(policies),
			WithLogger(log.NewLogger()),
		)
		assert.NilError(subT, err)
		topologyIndexes := NewTopologyIndexes(t)

		policiesFromGateway := topologyIndexes.PoliciesFromGateway(invalidGateway)
		assert.Equal(subT, len(policiesFromGateway), 0)
	})

	t.Run("gateway with direct policy", func(subT *testing.T) {
		// route1 -> gw1
		// policy1 -> gw1

		gw1 := testBasicGateway("gw1", NS)
		gateways := []*gatewayapiv1.Gateway{gw1}

		route1 := testBasicRoute("route1", NS, gw1)
		routes := []*gatewayapiv1.HTTPRoute{route1}

		gwPolicy := testBasicGatewayPolicy("policy1", NS, gw1)
		policies := []Policy{gwPolicy}

		t, err := NewBasicTopology(
			WithGateways(gateways),
			WithRoutes(routes),
			WithPolicies(policies),
			WithLogger(log.NewLogger()),
		)
		assert.NilError(subT, err)
		topologyIndexes := NewTopologyIndexes(t)

		policiesFromGateway := topologyIndexes.PoliciesFromGateway(gw1)
		assert.Equal(subT, len(policiesFromGateway), 1)
		assert.Equal(subT,
			client.ObjectKeyFromObject(policiesFromGateway[0]),
			client.ObjectKeyFromObject(gwPolicy),
		)
	})

	t.Run("gateway with policies targeting routes", func(subT *testing.T) {
		// route1 -> gw1
		// policy1 -> route1

		gw1 := testBasicGateway("gw1", NS)
		gateways := []*gatewayapiv1.Gateway{gw1}

		route1 := testBasicRoute("route1", NS, gw1)
		routes := []*gatewayapiv1.HTTPRoute{route1}

		routePolicy := testBasicRoutePolicy("policy1", NS, route1)
		policies := []Policy{routePolicy}

		t, err := NewBasicTopology(
			WithGateways(gateways),
			WithRoutes(routes),
			WithPolicies(policies),
			WithLogger(log.NewLogger()),
		)
		assert.NilError(subT, err)
		topologyIndexes := NewTopologyIndexes(t)

		policiesFromGateway := topologyIndexes.PoliciesFromGateway(gw1)
		assert.Equal(subT, len(policiesFromGateway), 1)
		assert.Equal(subT,
			client.ObjectKeyFromObject(policiesFromGateway[0]),
			client.ObjectKeyFromObject(routePolicy),
		)
	})

	t.Run("single policy targeting route with multiple parent gateways", func(subT *testing.T) {
		// route1 -> gw1
		// route1 -> gw2
		// policy1 -> route1

		gw1 := testBasicGateway("gw1", NS)
		gw2 := testBasicGateway("gw2", NS)
		gateways := []*gatewayapiv1.Gateway{gw1, gw2}

		route1 := testBasicRoute("route1", NS, gw1, gw2)
		routes := []*gatewayapiv1.HTTPRoute{route1}

		routePolicy := testBasicRoutePolicy("policy1", NS, route1)
		policies := []Policy{routePolicy}

		t, err := NewBasicTopology(
			WithGateways(gateways),
			WithRoutes(routes),
			WithPolicies(policies),
			WithLogger(log.NewLogger()),
		)
		assert.NilError(subT, err)
		topologyIndexes := NewTopologyIndexes(t)

		policiesGw1 := topologyIndexes.PoliciesFromGateway(gw1)
		assert.Equal(subT, len(policiesGw1), 1)
		assert.Equal(subT,
			client.ObjectKeyFromObject(policiesGw1[0]),
			client.ObjectKeyFromObject(routePolicy),
		)

		policiesGw2 := topologyIndexes.PoliciesFromGateway(gw2)
		assert.Equal(subT, len(policiesGw2), 1)
		assert.Equal(subT,
			client.ObjectKeyFromObject(policiesGw2[0]),
			client.ObjectKeyFromObject(routePolicy),
		)
	})
}

func TestTopologyIndexes_GetPolicyHTTPRoute(t *testing.T) {
	t.Run("empty topology", func(subT *testing.T) {
		// policy1 -> route1

		route1 := testBasicRoute("route1", NS, nil...)
		policy := testBasicRoutePolicy("policy1", NS, route1)

		t, err := NewBasicTopology(WithLogger(log.NewLogger()))
		assert.NilError(subT, err)
		topologyIndexes := NewTopologyIndexes(t)

		route := topologyIndexes.GetPolicyHTTPRoute(policy)
		assert.Assert(subT, route == nil)
	})

	t.Run("gateway with direct policy", func(subT *testing.T) {
		// policy1 -> gw1

		gw1 := testBasicGateway("gw1", NS)
		gateways := []*gatewayapiv1.Gateway{gw1}

		gwPolicy := testBasicGatewayPolicy("policy1", NS, gw1)
		policies := []Policy{gwPolicy}

		t, err := NewBasicTopology(
			WithGateways(gateways),
			WithPolicies(policies),
			WithLogger(log.NewLogger()),
		)
		assert.NilError(subT, err)
		topologyIndexes := NewTopologyIndexes(t)

		route := topologyIndexes.GetPolicyHTTPRoute(gwPolicy)
		assert.Assert(subT, route == nil)
	})

	t.Run("route with direct policy", func(subT *testing.T) {
		// route1 -> gw1
		// policy1 -> route1

		gw1 := testBasicGateway("gw1", NS)
		gateways := []*gatewayapiv1.Gateway{gw1}

		route1 := testBasicRoute("route1", NS, gw1)
		routes := []*gatewayapiv1.HTTPRoute{route1}

		routePolicy := testBasicRoutePolicy("policy1", NS, route1)
		policies := []Policy{routePolicy}

		t, err := NewBasicTopology(
			WithGateways(gateways),
			WithRoutes(routes),
			WithPolicies(policies),
			WithLogger(log.NewLogger()),
		)
		assert.NilError(subT, err)
		topologyIndexes := NewTopologyIndexes(t)

		route := topologyIndexes.GetPolicyHTTPRoute(routePolicy)
		assert.Assert(subT, route != nil)
		assert.Equal(subT,
			client.ObjectKeyFromObject(route),
			client.ObjectKeyFromObject(route1),
		)
	})
}

func TestTopologyIndexes_GetUntargetedRoutes(t *testing.T) {
	t.Run("gateway without routes", func(subT *testing.T) {
		// gw1
		// policy1 -> gw1
		gw1 := testBasicGateway("gw1", NS)
		gateways := []*gatewayapiv1.Gateway{gw1}

		gatewayPolicy := testBasicGatewayPolicy("policy1", NS, gw1)
		policies := []Policy{gatewayPolicy}

		t, err := NewBasicTopology(
			WithGateways(gateways),
			WithPolicies(policies),
			WithLogger(log.NewLogger()),
		)
		assert.NilError(subT, err)
		topologyIndexes := NewTopologyIndexes(t)

		untargetedRoutes := topologyIndexes.GetUntargetedRoutes(gw1)
		assert.Equal(subT, len(untargetedRoutes), 0)
	})

	t.Run("all routes have policies", func(subT *testing.T) {
		// gw1
		// route 1 -> gw1
		// route 2 -> gw1
		// policy1 -> route1
		// policy2 -> route1
		gw1 := testBasicGateway("gw1", NS)
		gateways := []*gatewayapiv1.Gateway{gw1}

		route1 := testBasicRoute("route1", NS, gw1)
		route2 := testBasicRoute("route2", NS, gw1)
		routes := []*gatewayapiv1.HTTPRoute{route1, route2}

		routePolicy1 := testBasicRoutePolicy("policy1", NS, route1)
		routePolicy2 := testBasicRoutePolicy("policy2", NS, route2)
		policies := []Policy{routePolicy1, routePolicy2}

		t, err := NewBasicTopology(
			WithGateways(gateways),
			WithRoutes(routes),
			WithPolicies(policies),
			WithLogger(log.NewLogger()),
		)
		assert.NilError(subT, err)
		topologyIndexes := NewTopologyIndexes(t)

		untargetedRoutes := topologyIndexes.GetUntargetedRoutes(gw1)
		assert.Equal(subT, len(untargetedRoutes), 0)
	})

	t.Run("only one route is untargeted", func(subT *testing.T) {
		// gw1
		// route 1 -> gw1
		// route 2 -> gw1
		// policy1 -> route1
		gw1 := testBasicGateway("gw1", NS)
		gateways := []*gatewayapiv1.Gateway{gw1}

		route1 := testBasicRoute("route1", NS, gw1)
		route2 := testBasicRoute("route2", NS, gw1)
		routes := []*gatewayapiv1.HTTPRoute{route1, route2}

		routePolicy1 := testBasicRoutePolicy("policy1", NS, route1)
		policies := []Policy{routePolicy1}

		t, err := NewBasicTopology(
			WithGateways(gateways),
			WithRoutes(routes),
			WithPolicies(policies),
			WithLogger(log.NewLogger()),
		)
		assert.NilError(subT, err)
		topologyIndexes := NewTopologyIndexes(t)

		untargetedRoutes := topologyIndexes.GetUntargetedRoutes(gw1)
		assert.Equal(subT, len(untargetedRoutes), 1)
		assert.Equal(subT,
			client.ObjectKeyFromObject(untargetedRoutes[0]),
			client.ObjectKeyFromObject(route2),
		)
	})

	t.Run("all routes are untargeted", func(subT *testing.T) {
		// gw1
		// route 1 -> gw1
		// route 2 -> gw1
		gw1 := testBasicGateway("gw1", NS)
		gateways := []*gatewayapiv1.Gateway{gw1}

		route1 := testBasicRoute("route1", NS, gw1)
		route2 := testBasicRoute("route2", NS, gw1)
		routes := []*gatewayapiv1.HTTPRoute{route1, route2}

		t, err := NewBasicTopology(
			WithGateways(gateways),
			WithRoutes(routes),
			WithLogger(log.NewLogger()),
		)
		assert.NilError(subT, err)
		topologyIndexes := NewTopologyIndexes(t)

		untargetedRoutes := topologyIndexes.GetUntargetedRoutes(gw1)
		assert.Equal(subT, len(untargetedRoutes), 2)
	})
}

func TestTopologyIndexes_TopologyString(t *testing.T) {
	t.Run("empty topology", func(subT *testing.T) {
		t, err := NewBasicTopology(WithLogger(log.NewLogger()))
		assert.NilError(subT, err)
		topologyIndexes := NewTopologyIndexes(t)
		assert.NilError(subT, err)

		topologyStr := topologyIndexes.String()
		assert.Assert(subT, strings.Contains(topologyStr, `"policiesPerGateway": null`))
		assert.Assert(subT, strings.Contains(topologyStr, `"policiesTargetingRoutes": null`))
		assert.Assert(subT, strings.Contains(topologyStr, `"untargetedRoutesPerGateway": null`))
	})

	t.Run("1 gateway 1 route 1 policy for route", func(subT *testing.T) {
		// route1 -> gw1
		// policy1 -> route1

		gw1 := testBasicGateway("gw1", NS)
		gateways := []*gatewayapiv1.Gateway{gw1}

		route1 := testBasicRoute("route1", NS, gw1)
		routes := []*gatewayapiv1.HTTPRoute{route1}

		routePolicy := testBasicRoutePolicy("policy1", NS, route1)
		policies := []Policy{routePolicy}

		t, err := NewBasicTopology(
			WithGateways(gateways),
			WithRoutes(routes),
			WithPolicies(policies),
			WithLogger(log.NewLogger()),
		)
		assert.NilError(subT, err)
		topologyIndexes := NewTopologyIndexes(t)

		topologyStr := topologyIndexes.String()
		assert.Assert(subT, strings.Contains(topologyStr, `"policiesPerGateway": {
    "nsA/gw1": [
      "nsA/policy1"
    ]
  }`))
		assert.Assert(subT, strings.Contains(topologyStr, `"policiesTargetingRoutes": {
    "nsA/policy1": "nsA/route1"
  }`))
		assert.Assert(subT, strings.Contains(topologyStr, `"untargetedRoutesPerGateway": {
    "nsA/gw1": []
  }`))
	})
}
