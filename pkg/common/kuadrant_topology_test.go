//go:build unit

package common

import (
	"strings"
	"testing"

	"gotest.tools/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	gatewayapiv1alpha2 "sigs.k8s.io/gateway-api/apis/v1alpha2"
)

const (
	NS = "nsA"
)

func TestPoliciesFromGateway(t *testing.T) {
	t.Run("empty topology", func(subT *testing.T) {
		topology := NewKuadrantTopology(nil, nil, nil)

		policies := topology.PoliciesFromGateway(testBasicGateway("gw1"))
		assert.Equal(subT, len(policies), 0)
	})

	t.Run("unknown gateway", func(subT *testing.T) {
		gateways := []*gatewayapiv1.Gateway{
			testBasicGateway("gw1"),
			testBasicGateway("gw2"),
		}

		topology := NewKuadrantTopology(gateways, nil, nil)

		policies := topology.PoliciesFromGateway(testBasicGateway("unknown"))
		assert.Equal(subT, len(policies), 0)
	})

	t.Run("invalid gateway is skipped", func(subT *testing.T) {
		// route1 -> gw1
		// policy1 -> gw1
		// policy2 -> route1

		invalidGateway := testInvalidGateway("gw1")
		gateways := []*gatewayapiv1.Gateway{invalidGateway}

		route1 := testBasicRoute("route1", invalidGateway)
		routes := []*gatewayapiv1.HTTPRoute{route1}

		gwPolicy := testBasicGatewayPolicy("policy1", invalidGateway)
		routePolicy := testBasicRoutePolicy("policy2", route1)
		policies := []KuadrantPolicy{gwPolicy, routePolicy}

		topology := NewKuadrantTopology(gateways, routes, policies)

		policiesFromGateway := topology.PoliciesFromGateway(invalidGateway)
		assert.Equal(subT, len(policiesFromGateway), 0)
	})

	t.Run("gateway with direct policy", func(subT *testing.T) {
		// route1 -> gw1
		// policy1 -> gw1

		gw1 := testBasicGateway("gw1")
		gateways := []*gatewayapiv1.Gateway{gw1}

		route1 := testBasicRoute("route1", gw1)
		routes := []*gatewayapiv1.HTTPRoute{route1}

		gwPolicy := testBasicGatewayPolicy("policy1", gw1)
		policies := []KuadrantPolicy{gwPolicy}

		topology := NewKuadrantTopology(gateways, routes, policies)

		policiesFromGateway := topology.PoliciesFromGateway(gw1)
		assert.Equal(subT, len(policiesFromGateway), 1)
		assert.Equal(subT,
			client.ObjectKeyFromObject(policiesFromGateway[0]),
			client.ObjectKeyFromObject(gwPolicy),
		)
	})

	t.Run("gateway with policies targeting routes", func(subT *testing.T) {
		// route1 -> gw1
		// policy1 -> route1

		gw1 := testBasicGateway("gw1")
		gateways := []*gatewayapiv1.Gateway{gw1}

		route1 := testBasicRoute("route1", gw1)
		routes := []*gatewayapiv1.HTTPRoute{route1}

		routePolicy := testBasicRoutePolicy("policy1", route1)
		policies := []KuadrantPolicy{routePolicy}

		topology := NewKuadrantTopology(gateways, routes, policies)

		policiesFromGateway := topology.PoliciesFromGateway(gw1)
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

		gw1 := testBasicGateway("gw1")
		gw2 := testBasicGateway("gw2")
		gateways := []*gatewayapiv1.Gateway{gw1, gw2}

		route1 := testBasicRoute("route1", gw1, gw2)
		routes := []*gatewayapiv1.HTTPRoute{route1}

		routePolicy := testBasicRoutePolicy("policy1", route1)
		policies := []KuadrantPolicy{routePolicy}

		topology := NewKuadrantTopology(gateways, routes, policies)

		policiesGw1 := topology.PoliciesFromGateway(gw1)
		assert.Equal(subT, len(policiesGw1), 1)
		assert.Equal(subT,
			client.ObjectKeyFromObject(policiesGw1[0]),
			client.ObjectKeyFromObject(routePolicy),
		)

		policiesGw2 := topology.PoliciesFromGateway(gw2)
		assert.Equal(subT, len(policiesGw2), 1)
		assert.Equal(subT,
			client.ObjectKeyFromObject(policiesGw2[0]),
			client.ObjectKeyFromObject(routePolicy),
		)
	})
}

func TestGetPolicyHTTPRoute(t *testing.T) {
	t.Run("empty topology", func(subT *testing.T) {
		// policy1 -> route1

		route1 := testBasicRoute("route1", nil...)
		policy := testBasicRoutePolicy("policy1", route1)

		topology := NewKuadrantTopology(nil, nil, nil)

		route := topology.GetPolicyHTTPRoute(policy)
		assert.Assert(subT, route == nil)
	})

	t.Run("gateway with direct policy", func(subT *testing.T) {
		// policy1 -> gw1

		gw1 := testBasicGateway("gw1")
		gateways := []*gatewayapiv1.Gateway{gw1}

		gwPolicy := testBasicGatewayPolicy("policy1", gw1)
		policies := []KuadrantPolicy{gwPolicy}

		topology := NewKuadrantTopology(gateways, nil, policies)

		route := topology.GetPolicyHTTPRoute(gwPolicy)
		assert.Assert(subT, route == nil)
	})

	t.Run("route with direct policy", func(subT *testing.T) {
		// route1 -> gw1
		// policy1 -> route1

		gw1 := testBasicGateway("gw1")
		gateways := []*gatewayapiv1.Gateway{gw1}

		route1 := testBasicRoute("route1", gw1)
		routes := []*gatewayapiv1.HTTPRoute{route1}

		routePolicy := testBasicRoutePolicy("policy1", route1)
		policies := []KuadrantPolicy{routePolicy}

		topology := NewKuadrantTopology(gateways, routes, policies)

		route := topology.GetPolicyHTTPRoute(routePolicy)
		assert.Assert(subT, route != nil)
		assert.Equal(subT,
			client.ObjectKeyFromObject(route),
			client.ObjectKeyFromObject(route1),
		)
	})
}

func TestGetUntargetedRoutes(t *testing.T) {
	t.Run("gateway without routes", func(subT *testing.T) {
		// gw1
		// policy1 -> gw1
		gw1 := testBasicGateway("gw1")
		gateways := []*gatewayapiv1.Gateway{gw1}

		gatewayPolicy := testBasicGatewayPolicy("policy1", gw1)
		policies := []KuadrantPolicy{gatewayPolicy}

		topology := NewKuadrantTopology(gateways, nil, policies)

		untargetedRoutes := topology.GetUntargetedRoutes(gw1)
		assert.Equal(subT, len(untargetedRoutes), 0)
	})

	t.Run("all routes have policies", func(subT *testing.T) {
		// gw1
		// route 1 -> gw1
		// route 2 -> gw1
		// policy1 -> route1
		// policy2 -> route1
		gw1 := testBasicGateway("gw1")
		gateways := []*gatewayapiv1.Gateway{gw1}

		route1 := testBasicRoute("route1", gw1)
		route2 := testBasicRoute("route2", gw1)
		routes := []*gatewayapiv1.HTTPRoute{route1, route2}

		routePolicy1 := testBasicRoutePolicy("policy1", route1)
		routePolicy2 := testBasicRoutePolicy("policy2", route2)
		policies := []KuadrantPolicy{routePolicy1, routePolicy2}

		topology := NewKuadrantTopology(gateways, routes, policies)

		untargetedRoutes := topology.GetUntargetedRoutes(gw1)
		assert.Equal(subT, len(untargetedRoutes), 0)
	})

	t.Run("only one route is untargeted", func(subT *testing.T) {
		// gw1
		// route 1 -> gw1
		// route 2 -> gw1
		// policy1 -> route1
		gw1 := testBasicGateway("gw1")
		gateways := []*gatewayapiv1.Gateway{gw1}

		route1 := testBasicRoute("route1", gw1)
		route2 := testBasicRoute("route2", gw1)
		routes := []*gatewayapiv1.HTTPRoute{route1, route2}

		routePolicy1 := testBasicRoutePolicy("policy1", route1)
		policies := []KuadrantPolicy{routePolicy1}

		topology := NewKuadrantTopology(gateways, routes, policies)

		untargetedRoutes := topology.GetUntargetedRoutes(gw1)
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
		gw1 := testBasicGateway("gw1")
		gateways := []*gatewayapiv1.Gateway{gw1}

		route1 := testBasicRoute("route1", gw1)
		route2 := testBasicRoute("route2", gw1)
		routes := []*gatewayapiv1.HTTPRoute{route1, route2}

		topology := NewKuadrantTopology(gateways, routes, nil)

		untargetedRoutes := topology.GetUntargetedRoutes(gw1)
		assert.Equal(subT, len(untargetedRoutes), 2)
	})
}

func TestKuadrantTopologyString(t *testing.T) {
	t.Run("empty topology", func(subT *testing.T) {
		topology := NewKuadrantTopology(nil, nil, nil)

		topologyStr := topology.String()
		assert.Assert(subT, strings.Contains(topologyStr, `"gateways": null`))
		assert.Assert(subT, strings.Contains(topologyStr, `"routes": null`))
		assert.Assert(subT, strings.Contains(topologyStr, `"policies": null`))
		assert.Assert(subT, strings.Contains(topologyStr, `"policiesPerGateway": null`))
		assert.Assert(subT, strings.Contains(topologyStr, `"policiesTargetingRoutes": null`))
		assert.Assert(subT, strings.Contains(topologyStr, `"untargetedRoutesPerGateway": null`))
	})

	t.Run("1 gateway 1 route 1 policy for route", func(subT *testing.T) {
		// route1 -> gw1
		// policy1 -> route1

		gw1 := testBasicGateway("gw1")
		gateways := []*gatewayapiv1.Gateway{gw1}

		route1 := testBasicRoute("route1", gw1)
		routes := []*gatewayapiv1.HTTPRoute{route1}

		routePolicy := testBasicRoutePolicy("policy1", route1)
		policies := []KuadrantPolicy{routePolicy}

		topology := NewKuadrantTopology(gateways, routes, policies)

		topologyStr := topology.String()
		assert.Assert(subT, strings.Contains(topologyStr, `"gateways": [
    {
      "id": "nsA/gw1",
      "routes": [
        "nsA/route1"
      ],
      "policy": null
    }
  ]`))
		assert.Assert(subT, strings.Contains(topologyStr, `"routes": [
    {
      "id": "nsA/route1",
      "parents": [
        "nsA/gw1"
      ],
      "policy": "nsA/policy1"
    }
  ]`))
		assert.Assert(subT, strings.Contains(topologyStr, `"policies": [
    {
      "id": "nsA/policy1",
      "gateway": null,
      "route": "nsA/route1"
    }
  ]`))
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

func testBasicGateway(name string) *gatewayapiv1.Gateway {
	// Valid gateway
	return &gatewayapiv1.Gateway{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: NS,
			Name:      name,
		},
		Status: gatewayapiv1.GatewayStatus{
			Conditions: []metav1.Condition{
				{
					Type:   GatewayProgrammedConditionType,
					Status: metav1.ConditionTrue,
				},
			},
		},
	}
}

func testInvalidGateway(name string) *gatewayapiv1.Gateway {
	gw := testBasicGateway(name)
	// remove conditions to make it invalid
	gw.Status = gatewayapiv1.GatewayStatus{}

	return gw
}

func testBasicRoute(name string, parents ...*gatewayapiv1.Gateway) *gatewayapiv1.HTTPRoute {
	parentRefs := make([]gatewayapiv1.ParentReference, 0)
	for _, val := range parents {
		parentRefs = append(parentRefs, gatewayapiv1.ParentReference{
			Group:     &[]gatewayapiv1.Group{"gateway.networking.k8s.io"}[0],
			Kind:      &[]gatewayapiv1.Kind{"Gateway"}[0],
			Namespace: &[]gatewayapiv1.Namespace{gatewayapiv1.Namespace(val.Namespace)}[0],
			Name:      gatewayapiv1.ObjectName(val.Name),
		})
	}

	parentStatusRefs := Map(parentRefs, func(p gatewayapiv1.ParentReference) gatewayapiv1.RouteParentStatus {
		return gatewayapiv1.RouteParentStatus{
			ParentRef:  p,
			Conditions: []metav1.Condition{{Type: "Accepted", Status: metav1.ConditionTrue}},
		}
	})

	return &gatewayapiv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: NS,
			Name:      name,
		},
		Spec: gatewayapiv1.HTTPRouteSpec{
			CommonRouteSpec: gatewayapiv1.CommonRouteSpec{
				ParentRefs: parentRefs,
			},
		},
		Status: gatewayapiv1.HTTPRouteStatus{
			RouteStatus: gatewayapiv1.RouteStatus{
				Parents: parentStatusRefs,
			},
		},
	}
}

func testBasicGatewayPolicy(name string, gateway *gatewayapiv1.Gateway) KuadrantPolicy {
	return &TestPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: NS,
			Name:      name,
		},
		TargetRef: gatewayapiv1alpha2.PolicyTargetReference{
			Group:     gatewayapiv1.Group("gateway.networking.k8s.io"),
			Kind:      gatewayapiv1.Kind("Gateway"),
			Namespace: &[]gatewayapiv1.Namespace{gatewayapiv1.Namespace(gateway.Namespace)}[0],
			Name:      gatewayapiv1.ObjectName(gateway.Name),
		},
	}
}

func testBasicRoutePolicy(name string, route *gatewayapiv1.HTTPRoute) KuadrantPolicy {
	return &TestPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: NS,
			Name:      name,
		},
		TargetRef: gatewayapiv1alpha2.PolicyTargetReference{
			Group:     gatewayapiv1.Group("gateway.networking.k8s.io"),
			Kind:      gatewayapiv1.Kind("HTTPRoute"),
			Namespace: &[]gatewayapiv1.Namespace{gatewayapiv1.Namespace(route.Namespace)}[0],
			Name:      gatewayapiv1.ObjectName(route.Name),
		},
	}
}
