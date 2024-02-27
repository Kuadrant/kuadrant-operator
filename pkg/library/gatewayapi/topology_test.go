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
		topology, err := NewGatewayAPITopology(WithLogger(log.NewLogger()))
		assert.NilError(subT, err)
		assert.Assert(subT, len(topology.Gateways()) == 0, "topology should not return any gateway")
	})

	t.Run("invalid gateway is skipped", func(subT *testing.T) {
		invalidGateway := testInvalidGateway("gw1", NS)
		gateways := []*gatewayapiv1.Gateway{invalidGateway}

		topology, err := NewGatewayAPITopology(WithGateways(gateways), WithLogger(log.NewLogger()))
		assert.NilError(subT, err)

		assert.Assert(subT, len(topology.Gateways()) == 0, "not ready gateways should not be added")
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

		topology, err := NewGatewayAPITopology(WithGateways(gateways), WithLogger(log.NewLogger()))
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

		topology, err := NewGatewayAPITopology(WithGateways(gateways), WithLogger(log.NewLogger()))
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

		topology, err := NewGatewayAPITopology(
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

		topology, err := NewGatewayAPITopology(
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
			client.ObjectKey{Name: "routeA", Namespace: NS},
			client.ObjectKey{Name: "routeB", Namespace: NS},
		}
		assert.Assert(subT, utils.SameElements(expectedKeys, returnedKeys))

		gw2Node, ok := gwIndex[client.ObjectKeyFromObject(gw2)]
		assert.Assert(subT, ok, "expected gateway not found")
		assert.Equal(subT, len(gw2Node.Routes()), 0)
	})
}

func TestGatewayAPITopology_GatewayNode_AttachedPolicies(t *testing.T) {
	// policy1 -> gw 1
	// none -> gw2

	gw1 := testBasicGateway("gw1", NS)
	gw2 := testBasicGateway("gw2", NS)
	gateways := []*gatewayapiv1.Gateway{gw1, gw2}

	gwPolicy := testBasicGatewayPolicy("policy1", NS, gw1)
	policies := []GatewayAPIPolicy{gwPolicy}

	topology, err := NewGatewayAPITopology(
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
		topology, err := NewGatewayAPITopology(WithLogger(log.NewLogger()))
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

		topology, err := NewGatewayAPITopology(WithRoutes(routes), WithLogger(log.NewLogger()))
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
	policies := []GatewayAPIPolicy{routePolicy}

	topology, err := NewGatewayAPITopology(
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
