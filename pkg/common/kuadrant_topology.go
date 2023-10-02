package common

import (
	"encoding/json"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"
)

type KuadrantTopology struct {
	// gatewayPolicies is an index of gateways mapping to Kuadrant Policies which
	// directly or indirectly are targeting the indexed gateway.
	// When a kuadrant policy directly or indirectly targets a gateway, the policy's configuration
	// needs to be added to that gateway.
	// Type: Gateway -> []Policy
	gatewayPolicies map[client.ObjectKey][]KuadrantPolicy

	// policyRoute is an index of policies mapping to HTTPRoutes
	// The index only includes policies targeting only existing and accepted (by parent gateways) HTTPRoutes
	// Type: Policy -> HTTPRoute
	policyRoute map[client.ObjectKey]*gatewayapiv1.HTTPRoute

	// freeRoutes is an index of gateways mapping to HTTPRoutes not targeted by a kuadrant policy
	// Gateway -> []HTTPRoute
	freeRoutes map[client.ObjectKey][]*gatewayapiv1.HTTPRoute

	// Raw topology with gateways, routes and policies
	// Currently only used for logging
	internalTopology *gatewayAPITopology
}

func NewKuadrantTopology(gateways []*gatewayapiv1.Gateway, routes []*gatewayapiv1.HTTPRoute, policies []KuadrantPolicy) *KuadrantTopology {
	t := newGatewayAPITopology(gateways, routes, policies)

	return &KuadrantTopology{
		gatewayPolicies:  buildGatewayPoliciesIndex(t),
		policyRoute:      buildPolicyRouteIndex(t),
		freeRoutes:       buildFreeRoutesIndex(t),
		internalTopology: t,
	}
}

func (k *KuadrantTopology) PoliciesFromGateway(gateway *gatewayapiv1.Gateway) []KuadrantPolicy {
	return k.gatewayPolicies[client.ObjectKeyFromObject(gateway)]
}

func (k *KuadrantTopology) GetPolicyHTTPRoute(policy KuadrantPolicy) *gatewayapiv1.HTTPRoute {
	return k.policyRoute[client.ObjectKeyFromObject(policy)]
}

func (k *KuadrantTopology) GetFreeRoutes(gateway *gatewayapiv1.Gateway) []*gatewayapiv1.HTTPRoute {
	return k.freeRoutes[client.ObjectKeyFromObject(gateway)]
}

// String representation of the topology
// This is not designed to be a serialization format that could be deserialized
func (k *KuadrantTopology) String() string {
	type Gateway struct {
		Key    string   `json:"id"`
		Routes []string `json:"routes"`
		Policy *string  `json:"policy"`
	}

	type Route struct {
		Key     string   `json:"id"`
		Parents []string `json:"parents"`
		Policy  *string  `json:"policy"`
	}

	type Policy struct {
		Key     string  `json:"id"`
		Gateway *string `json:"gateway"`
		Route   *string `json:"route"`
	}

	gateways := func() []Gateway {
		var gList []Gateway
		for _, gwNode := range k.internalTopology.GatewaysIndex {
			gw := Gateway{Key: gwNode.ObjectKey().String()}

			var rList []string
			for _, route := range gwNode.Routes {
				rList = append(rList, route.ObjectKey().String())
			}
			gw.Routes = rList

			if gwNode.DirectPolicy != nil {
				gw.Policy = &[]string{gwNode.DirectPolicy.ObjectKey().String()}[0]
			}

			gList = append(gList, gw)
		}
		return gList
	}()

	routes := func() []Route {
		var rList []Route
		for _, routeNode := range k.internalTopology.RoutesIndex {
			route := Route{Key: routeNode.ObjectKey().String()}

			var pList []string
			for _, parent := range routeNode.Parents {
				pList = append(pList, parent.ObjectKey().String())
			}
			route.Parents = pList

			if routeNode.DirectPolicy != nil {
				route.Policy = &[]string{routeNode.DirectPolicy.ObjectKey().String()}[0]
			}

			rList = append(rList, route)
		}
		return rList
	}()

	policies := func() []Policy {
		var pList []Policy
		for _, policyNode := range k.internalTopology.PoliciesIndex {
			policy := Policy{Key: policyNode.ObjectKey().String()}
			if policyNode.TargetedGateway != nil {
				policy.Gateway = &[]string{policyNode.TargetedGateway.ObjectKey().String()}[0]
			}
			if policyNode.TargetedRoute != nil {
				policy.Route = &[]string{policyNode.TargetedRoute.ObjectKey().String()}[0]
			}
			pList = append(pList, policy)
		}
		return pList
	}()

	policiesPerGateway := func() map[string][]string {
		index := make(map[string][]string, 0)
		for gatewayKey, policyList := range k.gatewayPolicies {
			index[gatewayKey.String()] = Map(policyList, func(p KuadrantPolicy) string {
				return client.ObjectKeyFromObject(p).String()
			})
		}
		if len(index) == 0 {
			return nil
		}
		return index
	}()

	policiesTargetingRoutes := func() map[string]string {
		index := make(map[string]string, 0)
		for policyKey, route := range k.policyRoute {
			index[policyKey.String()] = client.ObjectKeyFromObject(route).String()
		}
		if len(index) == 0 {
			return nil
		}
		return index
	}()

	freeRoutesPerGateway := func() map[string][]string {
		index := make(map[string][]string, 0)
		for gatewayKey, routeList := range k.freeRoutes {
			index[gatewayKey.String()] = Map(routeList, func(route *gatewayapiv1.HTTPRoute) string {
				return client.ObjectKeyFromObject(route).String()
			})
		}
		if len(index) == 0 {
			return nil
		}
		return index
	}()

	topologyRepr := struct {
		Gateways        []Gateway           `json:"gateways"`
		Routes          []Route             `json:"routes"`
		Policies        []Policy            `json:"policies"`
		GatewayPolicies map[string][]string `json:"policiesPerGateway"`
		PolicyRoute     map[string]string   `json:"policiesTargetingRoutes"`
		FreeRoutes      map[string][]string `json:"freeRoutesPerGateway"`
	}{
		gateways,
		routes,
		policies,
		policiesPerGateway,
		policiesTargetingRoutes,
		freeRoutesPerGateway,
	}

	jsonData, err := json.MarshalIndent(topologyRepr, "", "  ")
	if err != nil {
		panic(err)
	}
	return string(jsonData)
}

func buildGatewayPoliciesIndex(t *gatewayAPITopology) map[client.ObjectKey][]KuadrantPolicy {
	// Build Gateway -> []Policy index with all the policies affecting the indexed gateway
	index := make(map[client.ObjectKey][]KuadrantPolicy, 0)
	for _, gatewayNode := range t.GatewaysIndex {
		// Consisting of:
		// - Policy targeting directly the gateway
		// - Policies targeting the descendant routes of the gateway
		policies := make([]KuadrantPolicy, 0)

		if gatewayNode.DirectPolicy != nil {
			policies = append(policies, gatewayNode.DirectPolicy.Policy)
		}

		for _, routeNode := range gatewayNode.Routes {
			if routeNode.DirectPolicy != nil {
				policies = append(policies, routeNode.DirectPolicy.Policy)
			}
		}

		index[gatewayNode.ObjectKey()] = policies
	}

	return index
}

func buildPolicyRouteIndex(t *gatewayAPITopology) map[client.ObjectKey]*gatewayapiv1.HTTPRoute {
	// Build Policy -> HTTPRoute index with the route targeted by the indexed policy
	index := make(map[client.ObjectKey]*gatewayapiv1.HTTPRoute, 0)
	for _, policyNode := range t.PoliciesIndex {
		if policyNode.TargetedRoute != nil {
			index[policyNode.ObjectKey()] = policyNode.TargetedRoute.Route
		}
	}

	return index
}

func buildFreeRoutesIndex(t *gatewayAPITopology) map[client.ObjectKey][]*gatewayapiv1.HTTPRoute {
	// Build Gateway -> []HTTPRoute index with all the routes not targeted by a policy
	index := make(map[client.ObjectKey][]*gatewayapiv1.HTTPRoute, 0)

	for _, gatewayNode := range t.GatewaysIndex {
		routes := make([]*gatewayapiv1.HTTPRoute, 0)

		for _, routeNode := range gatewayNode.Routes {
			if routeNode.DirectPolicy == nil {
				routes = append(routes, routeNode.Route)
			}
		}

		index[gatewayNode.ObjectKey()] = routes
	}

	return index
}

type gatewayNode struct {
	DirectPolicy *policyNode
	Routes       map[client.ObjectKey]*routeNode
	Gateway      *gatewayapiv1.Gateway
}

func (g *gatewayNode) ObjectKey() client.ObjectKey {
	if g.Gateway == nil {
		return client.ObjectKey{}
	}

	return client.ObjectKeyFromObject(g.Gateway)
}

type routeNode struct {
	Parents      map[client.ObjectKey]*gatewayNode
	DirectPolicy *policyNode
	Route        *gatewayapiv1.HTTPRoute
}

func (r *routeNode) ObjectKey() client.ObjectKey {
	if r.Route == nil {
		return client.ObjectKey{}
	}

	return client.ObjectKeyFromObject(r.Route)
}

type policyNode struct {
	Policy          KuadrantPolicy
	TargetedGateway *gatewayNode
	TargetedRoute   *routeNode
}

func (p *policyNode) ObjectKey() client.ObjectKey {
	if p.Policy == nil {
		return client.ObjectKey{}
	}

	return client.ObjectKeyFromObject(p.Policy)
}

type gatewayAPITopology struct {
	GatewaysIndex map[client.ObjectKey]*gatewayNode
	RoutesIndex   map[client.ObjectKey]*routeNode
	PoliciesIndex map[client.ObjectKey]*policyNode
}

func newGatewayAPITopology(gateways []*gatewayapiv1.Gateway, routes []*gatewayapiv1.HTTPRoute, policies []KuadrantPolicy) *gatewayAPITopology {
	gatewaysIndex := initializeGateways(gateways)
	routesIndex := initializeRoutes(routes)
	policiesIndex := initializePolicies(policies)

	// Build botton -> up. Start from policies (leaves) up to gateways
	for _, policyNode := range policiesIndex {
		if IsTargetRefGateway(policyNode.Policy.GetTargetRef()) {
			namespace := string(ptr.Deref(policyNode.Policy.GetTargetRef().Namespace, policyNode.Policy.GetWrappedNamespace()))

			gwKey := client.ObjectKey{Name: string(policyNode.Policy.GetTargetRef().Name), Namespace: namespace}

			gatewayNode := gatewaysIndex[gwKey]

			// the targeted gateway may not be in the available list of gateways
			if gatewayNode != nil {
				policyNode.TargetedGateway = gatewayNode
				gatewayNode.DirectPolicy = policyNode
			}
		} else if IsTargetRefHTTPRoute(policyNode.Policy.GetTargetRef()) {
			namespace := string(ptr.Deref(policyNode.Policy.GetTargetRef().Namespace, policyNode.Policy.GetWrappedNamespace()))

			routeKey := client.ObjectKey{Name: string(policyNode.Policy.GetTargetRef().Name), Namespace: namespace}

			routeNode := routesIndex[routeKey]

			// the targeted route may not be in the available list of routes
			if routeNode != nil {
				policyNode.TargetedRoute = routeNode
				routeNode.DirectPolicy = policyNode
			}
		}

		//  skipping the policy as it does not target neither a valid route nor a valid gateway
	}

	for _, routeNode := range routesIndex {
		for _, parentKey := range GetRouteAcceptedGatewayParentKeys(routeNode.Route) {
			gatewayNode := gatewaysIndex[parentKey]
			// the parent gateway may not be in the available list of gateways
			// or the gateway may not be valid
			if gatewayNode != nil {
				gatewayNode.Routes[routeNode.ObjectKey()] = routeNode
				routeNode.Parents[gatewayNode.ObjectKey()] = gatewayNode
			}
		}
	}

	return &gatewayAPITopology{
		GatewaysIndex: gatewaysIndex,
		RoutesIndex:   routesIndex,
		PoliciesIndex: policiesIndex,
	}
}

func initializeGateways(gateways []*gatewayapiv1.Gateway) map[client.ObjectKey]*gatewayNode {
	gatewaysIndex := make(map[client.ObjectKey]*gatewayNode, 0)

	validGateways := Filter(gateways, func(g *gatewayapiv1.Gateway) bool {
		return meta.IsStatusConditionTrue(g.Status.Conditions, GatewayProgrammedConditionType)
	})

	for _, gateway := range validGateways {
		gatewaysIndex[client.ObjectKeyFromObject(gateway)] = &gatewayNode{
			Routes:  make(map[client.ObjectKey]*routeNode, 0),
			Gateway: gateway,
		}
	}
	return gatewaysIndex
}

func initializeRoutes(routes []*gatewayapiv1.HTTPRoute) map[client.ObjectKey]*routeNode {
	routesIndex := make(map[client.ObjectKey]*routeNode, 0)
	for _, route := range routes {
		routesIndex[client.ObjectKeyFromObject(route)] = &routeNode{
			Parents: make(map[client.ObjectKey]*gatewayNode, 0),
			Route:   route,
		}
	}
	return routesIndex
}

func initializePolicies(policies []KuadrantPolicy) map[client.ObjectKey]*policyNode {
	policiesIndex := make(map[client.ObjectKey]*policyNode, 0)
	for _, policy := range policies {
		policiesIndex[client.ObjectKeyFromObject(policy)] = &policyNode{Policy: policy}
	}
	return policiesIndex
}
