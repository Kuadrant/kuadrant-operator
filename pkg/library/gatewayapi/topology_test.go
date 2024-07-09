//go:build unit

package gatewayapi

import (
	"testing"

	"gotest.tools/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
	"github.com/kuadrant/kuadrant-operator/pkg/log"
)

func TestGatewayAPITopology_Gateways(t *testing.T) {
	t.Run("no gateways", func(subT *testing.T) {
		topology, err := NewBasicTopology(WithLogger(log.NewLogger()))
		assert.NilError(subT, err)
		assert.Assert(subT, len(topology.Gateways()) == 0, "topology should not return any gateway")
	})

	t.Run("invalid gateway is added as a topology node", func(subT *testing.T) {
		invalidGateway := testInvalidGateway("gw1", NS)
		gateways := []*gatewayapiv1.Gateway{invalidGateway}

		topology, err := NewBasicTopology(WithGateways(gateways), WithLogger(log.NewLogger()))
		assert.NilError(subT, err)

		assert.Equal(subT, len(topology.Gateways()), 1, "not ready gateways should be added")
	})

	t.Run("valid gateways are included", func(subT *testing.T) {
		gateways := make([]*gatewayapiv1.Gateway, 0)
		gwKeys := []client.ObjectKey{
			{Name: "gw1", Namespace: NS},
			{Name: "gw2", Namespace: NS},
			{Name: "gw3", Namespace: NS},
		}
		for _, gwKey := range gwKeys {
			gateways = append(gateways, testBasicGateway(gwKey.Name, gwKey.Namespace))
		}

		topology, err := NewBasicTopology(WithGateways(gateways), WithLogger(log.NewLogger()))
		assert.NilError(subT, err)

		assert.Assert(subT, len(topology.Gateways()) == 3, "expected gateways not returned")
		returnedKeys := make([]client.ObjectKey, 0)
		for _, gw := range topology.Gateways() {
			returnedKeys = append(returnedKeys, client.ObjectKeyFromObject(gw.Gateway))
		}
		assert.Assert(subT, utils.SameElements(gwKeys, returnedKeys))
	})
}

func TestGatewayAPITopology_GatewayNode_Routes(t *testing.T) {
	t.Run("empty routes", func(subT *testing.T) {
		gw1 := testBasicGateway("gw1", NS)
		gw2 := testBasicGateway("gw2", NS)
		gw3 := testBasicGateway("gw3", NS)
		gateways := []*gatewayapiv1.Gateway{gw1, gw2, gw3}

		topology, err := NewBasicTopology(WithGateways(gateways), WithLogger(log.NewLogger()))
		assert.NilError(subT, err)

		for _, gw := range topology.Gateways() {
			assert.Equal(subT, len(gw.Routes()), 0)
		}
	})

	t.Run("some routes", func(subT *testing.T) {
		// route11 -> gw1
		// route21 -> gw2

		gw1 := testBasicGateway("gw1", NS)
		gw2 := testBasicGateway("gw2", NS)
		gateways := []*gatewayapiv1.Gateway{gw1, gw2}

		route11 := testBasicRoute("route11", NS, gw1)
		route21 := testBasicRoute("route21", NS, gw2)
		routes := []*gatewayapiv1.HTTPRoute{route11, route21}

		topology, err := NewBasicTopology(
			WithGateways(gateways),
			WithRoutes(routes),
			WithLogger(log.NewLogger()),
		)
		assert.NilError(subT, err)

		gwIndex := map[client.ObjectKey]GatewayNode{}
		for _, gw := range topology.Gateways() {
			gwIndex[client.ObjectKeyFromObject(gw.Gateway)] = gw
		}

		gw1Node, ok := gwIndex[client.ObjectKeyFromObject(gw1)]
		assert.Assert(subT, ok, "expected gateway not found")
		assert.Equal(subT, len(gw1Node.Routes()), 1)
		assert.Equal(subT, client.ObjectKeyFromObject(route11), client.ObjectKeyFromObject(gw1Node.Routes()[0].Route()))

		gw2Node, ok := gwIndex[client.ObjectKeyFromObject(gw2)]
		assert.Assert(subT, ok, "expected gateway not found")
		assert.Equal(subT, len(gw2Node.Routes()), 1)
		assert.Equal(subT, client.ObjectKeyFromObject(route21), client.ObjectKeyFromObject(gw2Node.Routes()[0].Route()))
	})

	t.Run("some routes not accepted by gateway", func(subT *testing.T) {
		// routeA -> gw1 (accepted)
		// routeB -> gw1 (accepted)
		// routeB -> gw2 (not accepted)

		gw1 := testBasicGateway("gw1", NS)
		gw2 := testBasicGateway("gw2", NS)
		gateways := []*gatewayapiv1.Gateway{gw1, gw2}

		routeA := testBasicRoute("routeA", NS, gw1)
		routeB := testBasicRoute("routeB", NS, gw1, gw2)
		routeB.Status.Parents[1].Conditions[0].Status = metav1.ConditionFalse
		routes := []*gatewayapiv1.HTTPRoute{routeA, routeB}

		topology, err := NewValidTopology(
			WithGateways(gateways),
			WithRoutes(routes),
			WithLogger(log.NewLogger()),
		)
		assert.NilError(subT, err)

		gwIndex := map[client.ObjectKey]GatewayNode{}
		for _, gw := range topology.Gateways() {
			gwIndex[client.ObjectKeyFromObject(gw.Gateway)] = gw
		}

		gw1Node, ok := gwIndex[client.ObjectKeyFromObject(gw1)]
		assert.Assert(subT, ok, "expected gateway not found")
		assert.Equal(subT, len(gw1Node.Routes()), 2)
		returnedKeys := make([]client.ObjectKey, 0)
		for _, route := range gw1Node.Routes() {
			returnedKeys = append(returnedKeys, client.ObjectKeyFromObject(route.Route()))
		}
		expectedKeys := []client.ObjectKey{
			{Name: "routeA", Namespace: NS},
			{Name: "routeB", Namespace: NS},
		}
		assert.Assert(subT, utils.SameElements(expectedKeys, returnedKeys))

		gw2Node, ok := gwIndex[client.ObjectKeyFromObject(gw2)]
		assert.Assert(subT, ok, "expected gateway not found")
		assert.Equal(subT, len(gw2Node.Routes()), 0)
	})

	t.Run("routes targetting unprogrammed gateway are not linked", func(subT *testing.T) {
		// routeA -> gw1 (unprogrammed)
		invalidGateway := testInvalidGateway("gw1", NS)
		gateways := []*gatewayapiv1.Gateway{invalidGateway}
		route := testBasicRoute("route", NS, invalidGateway)
		routes := []*gatewayapiv1.HTTPRoute{route}

		topology, err := NewValidTopology(
			WithGateways(gateways),
			WithRoutes(routes),
			WithLogger(log.NewLogger()),
		)
		assert.NilError(subT, err)

		gws := topology.Gateways()
		assert.Equal(subT, len(gws), 1, "not ready gateways should be added")
		assert.Equal(subT, len(gws[0].Routes()), 0, "routes targetting invalid gateways should not be linked")
	})
}

func TestGatewayAPITopology_GatewayNode_AttachedPolicies(t *testing.T) {
	// policy1 -> gw 1
	// none -> gw2

	gw1 := testBasicGateway("gw1", NS)
	gw2 := testBasicGateway("gw2", NS)
	gateways := []*gatewayapiv1.Gateway{gw1, gw2}

	gwPolicy := testBasicGatewayPolicy("policy1", NS, gw1)
	policies := []Policy{gwPolicy}

	topology, err := NewBasicTopology(
		WithGateways(gateways),
		WithPolicies(policies),
		WithLogger(log.NewLogger()),
	)
	assert.NilError(t, err)

	gwIndex := map[client.ObjectKey]GatewayNode{}
	for _, gw := range topology.Gateways() {
		gwIndex[client.ObjectKeyFromObject(gw.Gateway)] = gw
	}

	gw1Node, ok := gwIndex[client.ObjectKeyFromObject(gw1)]
	assert.Assert(t, ok, "expected gateway not found")
	assert.Equal(t, len(gw1Node.AttachedPolicies()), 1)
	assert.Equal(t, client.ObjectKeyFromObject(gwPolicy), client.ObjectKeyFromObject(gw1Node.AttachedPolicies()[0]))

	gw2Node, ok := gwIndex[client.ObjectKeyFromObject(gw2)]
	assert.Assert(t, ok, "expected gateway not found")
	assert.Equal(t, len(gw2Node.AttachedPolicies()), 0)
}

func TestGatewayAPITopology_Routes(t *testing.T) {
	t.Run("no routes", func(subT *testing.T) {
		topology, err := NewBasicTopology(WithLogger(log.NewLogger()))
		if err != nil {
			subT.Fatal(err)
		}

		if len(topology.Routes()) != 0 {
			subT.Fatal("topology should not return any route")
		}
	})

	t.Run("parentless routes are included", func(subT *testing.T) {
		routes := make([]*gatewayapiv1.HTTPRoute, 0)
		for _, routeName := range []string{"r1", "r2", "r3"} {
			routes = append(routes, testBasicRoute(routeName, NS))
		}

		topology, err := NewBasicTopology(WithRoutes(routes), WithLogger(log.NewLogger()))
		assert.NilError(subT, err)

		assert.Assert(subT, len(topology.Routes()) == 3, "expected routes not returned")
	})
}

func TestGatewayAPITopology_RouteNode_AttachedPolicies(t *testing.T) {
	// policy1 -> route 1
	// none -> route 2

	route1 := testBasicRoute("route1", NS)
	route2 := testBasicRoute("route2", NS)
	routes := []*gatewayapiv1.HTTPRoute{route1, route2}

	routePolicy := testBasicRoutePolicy("policy1", NS, route1)
	policies := []Policy{routePolicy}

	topology, err := NewBasicTopology(
		WithRoutes(routes),
		WithPolicies(policies),
		WithLogger(log.NewLogger()),
	)
	assert.NilError(t, err)

	routeIndex := map[client.ObjectKey]RouteNode{}
	for _, route := range topology.Routes() {
		routeIndex[client.ObjectKeyFromObject(route.Route())] = route
	}

	route1Node, ok := routeIndex[client.ObjectKeyFromObject(route1)]
	assert.Assert(t, ok, "expected route not found")
	assert.Equal(t, len(route1Node.AttachedPolicies()), 1)
	assert.Equal(t, client.ObjectKeyFromObject(routePolicy), client.ObjectKeyFromObject(route1Node.AttachedPolicies()[0]))

	route2Node, ok := routeIndex[client.ObjectKeyFromObject(route2)]
	assert.Assert(t, ok, "expected route not found")
	assert.Equal(t, len(route2Node.AttachedPolicies()), 0)
}

func TestGatewayAPITopology_Policies(t *testing.T) {
	t.Run("no policies", func(subT *testing.T) {
		topology, err := NewBasicTopology(WithLogger(log.NewLogger()))
		assert.NilError(subT, err)
		assert.Assert(subT, len(topology.Policies()) == 0, "topology should not return any policy")
	})

	t.Run("policy targetting missing network objet is added as a topology node", func(subT *testing.T) {
		policies := make([]Policy, 0)
		for _, pName := range []string{"p1", "p2", "p3"} {
			policies = append(policies, testStandalonePolicy(pName, NS))
		}

		topology, err := NewBasicTopology(WithPolicies(policies), WithLogger(log.NewLogger()))
		assert.NilError(subT, err)

		assert.Equal(subT, len(topology.Policies()), 3, "standalone policies should be added")
	})

	t.Run("happy path", func(subT *testing.T) {
		route := testBasicRoute("route", NS)
		routes := []*gatewayapiv1.HTTPRoute{route}

		routePolicy := testBasicRoutePolicy("policy", NS, route)
		policies := []Policy{routePolicy}

		topology, err := NewBasicTopology(
			WithRoutes(routes),
			WithPolicies(policies),
			WithLogger(log.NewLogger()),
		)
		assert.NilError(subT, err)

		assert.Equal(subT, len(topology.Policies()), 1, "expected policies not returned")
	})
}

func TestGatewayAPITopology_Policies_TargetRef(t *testing.T) {
	t.Run("policy targetting missing network objet does not return TargetRef", func(subT *testing.T) {
		topology, err := NewBasicTopology(
			WithPolicies([]Policy{testStandalonePolicy("p", NS)}),
			WithLogger(log.NewLogger()),
		)
		assert.NilError(subT, err)

		policyNodes := topology.Policies()
		assert.Equal(subT, len(policyNodes), 1, "standalone policies should be added")
		assert.Assert(subT, policyNodes[0].TargetRef() == nil, "standalone policies should not have target ref")
	})

	t.Run("targetting a httproute should return routenode", func(subT *testing.T) {
		route := testBasicRoute("route", NS)
		routes := []*gatewayapiv1.HTTPRoute{route}

		routePolicy := testBasicRoutePolicy("policy", NS, route)
		policies := []Policy{routePolicy}

		topology, err := NewBasicTopology(
			WithRoutes(routes),
			WithPolicies(policies),
			WithLogger(log.NewLogger()),
		)
		assert.NilError(subT, err)

		policyNodes := topology.Policies()
		assert.Equal(subT, len(policyNodes), 1, "expected policies not returned")
		targetRefNode := policyNodes[0].TargetRef()
		targetRouteNode, ok := targetRefNode.(*RouteNode)
		assert.Assert(subT, ok, "policy's target ref is not routenode")
		assert.Equal(subT, targetRouteNode.ObjectKey(), client.ObjectKeyFromObject(route), "policy's target ref has unexpected key")
	})

	t.Run("targetting a gateway should return gatewaynode", func(subT *testing.T) {
		gw := testBasicGateway("gw", NS)
		gateways := []*gatewayapiv1.Gateway{gw}

		gwPolicy := testBasicGatewayPolicy("policy", NS, gw)
		policies := []Policy{gwPolicy}

		topology, err := NewBasicTopology(
			WithGateways(gateways),
			WithPolicies(policies),
			WithLogger(log.NewLogger()),
		)
		assert.NilError(subT, err)

		policyNodes := topology.Policies()
		assert.Equal(subT, len(policyNodes), 1, "expected policies not returned")
		targetRefNode := policyNodes[0].TargetRef()
		targetGatewayNode, ok := targetRefNode.(*GatewayNode)
		assert.Assert(subT, ok, "policy's target ref is not gatewaynode")
		assert.Equal(subT, targetGatewayNode.ObjectKey(), client.ObjectKeyFromObject(gw), "policy's target ref has unexpected key")
	})
}

func TestGatewayAPITopology_GetPolicy(t *testing.T) {
	t.Run("when ID is not found, returns not found", func(subT *testing.T) {
		topology, err := NewBasicTopology(
			WithPolicies([]Policy{testStandalonePolicy("p", NS)}),
			WithLogger(log.NewLogger()),
		)
		assert.NilError(subT, err)

		_, ok := topology.GetPolicy(testStandalonePolicy("other", NS))
		assert.Assert(subT, !ok, "'other' policy should not be found")
	})

	t.Run("when ID is found, returns the policy", func(subT *testing.T) {
		policies := make([]Policy, 0)
		for _, pName := range []string{"p1", "p2", "p3"} {
			policies = append(policies, testStandalonePolicy(pName, NS))
		}

		topology, err := NewBasicTopology(WithPolicies(policies), WithLogger(log.NewLogger()))
		assert.NilError(subT, err)

		policyNode, ok := topology.GetPolicy(testStandalonePolicy("p1", NS))
		assert.Assert(subT, ok, "policy should be found")

		assert.Equal(subT, client.ObjectKeyFromObject(policyNode), client.ObjectKey{
			Name: "p1", Namespace: NS,
		})
	})
}
