package gatewayapi

import (
	"errors"
	"fmt"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kuadrant/kuadrant-operator/pkg/library/dag"
	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
)

const (
	typeField      dag.Field     = dag.Field("type")
	gatewayLabel   dag.NodeLabel = dag.NodeLabel("gateway")
	httprouteLabel dag.NodeLabel = dag.NodeLabel("httproute")
)

type RouteNode struct {
	*gatewayapiv1.HTTPRoute

	attachedPolicies []GatewayAPIPolicy
}

func (r *RouteNode) AttachedPolicies() []GatewayAPIPolicy {
	return r.attachedPolicies
}

func (r *RouteNode) Route() *gatewayapiv1.HTTPRoute {
	return r.HTTPRoute
}

type GatewayNode struct {
	*gatewayapiv1.Gateway

	attachedPolicies []GatewayAPIPolicy

	routes []RouteNode
}

func (g *GatewayNode) AttachedPolicies() []GatewayAPIPolicy {
	return g.attachedPolicies
}

func (g *GatewayNode) Routes() []RouteNode {
	return g.routes
}

func (g *GatewayNode) ObjectKey() client.ObjectKey {
	return client.ObjectKeyFromObject(g.Gateway)
}

type GatewayAPITopology struct {
	graph *dag.DAG
}

type gatewayDAGNode struct {
	*gatewayapiv1.Gateway

	attachedPolicies []GatewayAPIPolicy
}

func dagNodeIDFromObject(obj client.Object) dag.NodeID {
	return fmt.Sprintf("%s#%s", obj.GetObjectKind().GroupVersionKind().String(), client.ObjectKeyFromObject(obj).String())
}

func (g gatewayDAGNode) ID() string {
	return dagNodeIDFromObject(g.Gateway)
}

type httpRouteDAGNode struct {
	*gatewayapiv1.HTTPRoute

	attachedPolicies []GatewayAPIPolicy
}

func (h httpRouteDAGNode) ID() string {
	return dagNodeIDFromObject(h.HTTPRoute)
}

func NewGatewayAPITopology(gateways []*gatewayapiv1.Gateway, routes []*gatewayapiv1.HTTPRoute, policies []GatewayAPIPolicy) (*GatewayAPITopology, error) {
	// TODO logger
	typeIndexer := dag.WithFieldIndexer(typeField, func(n dag.Node) []dag.NodeLabel {
		switch n.(type) {
		case gatewayDAGNode:
			return []dag.NodeLabel{gatewayLabel}
		case httpRouteDAGNode:
			return []dag.NodeLabel{httprouteLabel}
		default:
			return nil
		}
	})

	fmt.Println("==================")
	fmt.Printf("len policies %d\n", len(policies))
	fmt.Printf("len routes %d\n", len(routes))

	graph := dag.NewDAG(typeIndexer)

	gatewayDAGNodes := buildGatewayDAGNodes(gateways, policies)

	routeDAGNodes := buildHTTPRouteDAGNodes(routes, policies)

	for _, node := range gatewayDAGNodes {
		err := graph.AddNode(node)
		if err != nil {
			return nil, err
		}
	}
	for _, node := range routeDAGNodes {
		err := graph.AddNode(node)
		if err != nil {
			return nil, err
		}
	}

	edges := buildDAGEdges(gatewayDAGNodes, routeDAGNodes)

	for _, edge := range edges {
		err := graph.AddEdge(edge.parent.ID(), edge.child.ID())
		if err != nil {
			return nil, err
		}
	}

	if !graph.Validate() {
		return nil, errors.New("DAG is not valid")
	}

	return &GatewayAPITopology{graph}, nil
}

type edge struct {
	parent dag.Node
	child  dag.Node
}

func buildDAGEdges(gateways []gatewayDAGNode, routes []httpRouteDAGNode) []edge {
	// internal index: key -> gateway for reference
	gatewaysIndex := make(map[client.ObjectKey]gatewayDAGNode, len(gateways))
	for _, gateway := range gateways {
		gatewaysIndex[client.ObjectKeyFromObject(gateway.Gateway)] = gateway
	}

	edges := make([]edge, 0)
	for _, route := range routes {
		for _, parentKey := range GetRouteAcceptedGatewayParentKeys(route.HTTPRoute) {
			// the parent gateway may not be in the available list of gateways
			// or the gateway may not be valid
			if gateway, ok := gatewaysIndex[parentKey]; ok {
				edges = append(edges, edge{parent: gateway, child: route})
			}
		}
	}

	return edges
}

func buildGatewayDAGNodes(gateways []*gatewayapiv1.Gateway, policies []GatewayAPIPolicy) []gatewayDAGNode {
	programmedGateways := utils.Filter(gateways, func(g *gatewayapiv1.Gateway) bool {
		return meta.IsStatusConditionTrue(g.Status.Conditions, GatewayProgrammedConditionType)
	})

	return utils.Map(programmedGateways, func(g *gatewayapiv1.Gateway) gatewayDAGNode {
		// Compute attached policies
		attachedPolicies := utils.Filter(policies, func(p GatewayAPIPolicy) bool {
			group := p.GetTargetRef().Group
			kind := p.GetTargetRef().Kind
			name := p.GetTargetRef().Name
			namespace := ptr.Deref(p.GetTargetRef().Namespace, gatewayapiv1.Namespace(p.GetNamespace()))

			return group == gatewayapiv1.GroupName &&
				kind == "Gateway" &&
				name == gatewayapiv1.ObjectName(g.Name) &&
				namespace == gatewayapiv1.Namespace(g.Namespace)
		})
		return gatewayDAGNode{Gateway: g, attachedPolicies: attachedPolicies}
	})
}

func buildHTTPRouteDAGNodes(routes []*gatewayapiv1.HTTPRoute, policies []GatewayAPIPolicy) []httpRouteDAGNode {
	return utils.Map(routes, func(route *gatewayapiv1.HTTPRoute) httpRouteDAGNode {
		fmt.Println("==================buildHTTPRouteDAGNodes route")
		fmt.Println(client.ObjectKeyFromObject(route))
		// Compute attached policies
		attachedPolicies := utils.Filter(policies, func(p GatewayAPIPolicy) bool {
			fmt.Println("==================buildHTTPRouteDAGNodes policy")
			fmt.Println(client.ObjectKeyFromObject(p))
			group := p.GetTargetRef().Group
			kind := p.GetTargetRef().Kind
			name := p.GetTargetRef().Name
			namespace := ptr.Deref(p.GetTargetRef().Namespace, gatewayapiv1.Namespace(p.GetNamespace()))

			return group == gatewayapiv1.GroupName &&
				kind == "HTTPRoute" &&
				name == gatewayapiv1.ObjectName(route.Name) &&
				namespace == gatewayapiv1.Namespace(route.Namespace)
		})
		return httpRouteDAGNode{HTTPRoute: route, attachedPolicies: attachedPolicies}
	})
}

func (g *GatewayAPITopology) Gateways() []GatewayNode {
	gatewayNodes := g.graph.GetNodes(typeField, gatewayLabel)

	return utils.Map(gatewayNodes, func(n dag.Node) GatewayNode {
		gNode, ok := n.(gatewayDAGNode)
		if !ok {
			// TODO logger
			panic("DAG gateway index returns nodes that are not gateways")
		}

		routeNodes := g.graph.Children(gNode.ID())
		// convert to "RouteNode" from httpRouteDAGNode
		routes := utils.Map(routeNodes, func(r dag.Node) RouteNode {
			rDAGNode, ok := r.(httpRouteDAGNode)
			if !ok {
				// TODO logger
				panic("DAG index returns gateway children that are not routes")
			}
			return RouteNode{
				HTTPRoute:        rDAGNode.HTTPRoute,
				attachedPolicies: rDAGNode.attachedPolicies,
			}
		})

		return GatewayNode{
			Gateway:          gNode.Gateway,
			attachedPolicies: gNode.attachedPolicies,
			routes:           routes,
		}

	})
}

func (g *GatewayAPITopology) Routes() []RouteNode {
	routeNodes := g.graph.GetNodes(typeField, httprouteLabel)

	return utils.Map(routeNodes, func(r dag.Node) RouteNode {
		rNode, ok := r.(httpRouteDAGNode)
		if !ok {
			// TODO logger
			panic("DAG route index returns nodes that are not routes")
		}
		return RouteNode{
			HTTPRoute:        rNode.HTTPRoute,
			attachedPolicies: rNode.attachedPolicies,
		}
	})
}
