package gatewayapi

import (
	"errors"
	"fmt"

	"github.com/go-logr/logr"
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

	attachedPolicies []Policy
}

func (r *RouteNode) AttachedPolicies() []Policy {
	return r.attachedPolicies
}

func (r *RouteNode) Route() *gatewayapiv1.HTTPRoute {
	return r.HTTPRoute
}

type GatewayNode struct {
	*gatewayapiv1.Gateway

	attachedPolicies []Policy

	routes []RouteNode
}

func (g *GatewayNode) AttachedPolicies() []Policy {
	return g.attachedPolicies
}

func (g *GatewayNode) Routes() []RouteNode {
	return g.routes
}

func (g *GatewayNode) ObjectKey() client.ObjectKey {
	return client.ObjectKeyFromObject(g.Gateway)
}

type Topology struct {
	graph  *dag.DAG
	Logger logr.Logger
}

type gatewayDAGNode struct {
	*gatewayapiv1.Gateway

	attachedPolicies []Policy
}

func dagNodeIDFromObject(obj client.Object) dag.NodeID {
	return fmt.Sprintf("%s#%s", obj.GetObjectKind().GroupVersionKind().String(), client.ObjectKeyFromObject(obj).String())
}

func (g gatewayDAGNode) ID() string {
	return dagNodeIDFromObject(g.Gateway)
}

type httpRouteDAGNode struct {
	*gatewayapiv1.HTTPRoute

	attachedPolicies []Policy
}

func (h httpRouteDAGNode) ID() string {
	return dagNodeIDFromObject(h.HTTPRoute)
}

type topologyOptions struct {
	gateways []*gatewayapiv1.Gateway
	routes   []*gatewayapiv1.HTTPRoute
	policies []Policy
	logger   logr.Logger
}

// TopologyOpts allows to manipulate topologyOptions.
type TopologyOpts func(*topologyOptions)

func WithLogger(logger logr.Logger) TopologyOpts {
	return func(o *topologyOptions) {
		o.logger = logger
	}
}

func WithGateways(gateways []*gatewayapiv1.Gateway) TopologyOpts {
	return func(o *topologyOptions) {
		o.gateways = gateways
	}
}

func WithRoutes(routes []*gatewayapiv1.HTTPRoute) TopologyOpts {
	return func(o *topologyOptions) {
		o.routes = routes
	}
}

func WithPolicies(policies []Policy) TopologyOpts {
	return func(o *topologyOptions) {
		o.policies = policies
	}
}

func NewTopology(opts ...TopologyOpts) (*Topology, error) {
	// defaults
	o := &topologyOptions{
		logger: logr.Discard(),
	}

	for _, opt := range opts {
		opt(o)
	}

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

	graph := dag.NewDAG(typeIndexer)

	gatewayDAGNodes := buildGatewayDAGNodes(o.gateways, o.policies)

	routeDAGNodes := buildHTTPRouteDAGNodes(o.routes, o.policies)

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

	return &Topology{graph, o.logger}, nil
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

func buildGatewayDAGNodes(gateways []*gatewayapiv1.Gateway, policies []Policy) []gatewayDAGNode {
	programmedGateways := utils.Filter(gateways, func(g *gatewayapiv1.Gateway) bool {
		return meta.IsStatusConditionTrue(g.Status.Conditions, string(gatewayapiv1.GatewayConditionProgrammed))
	})

	return utils.Map(programmedGateways, func(g *gatewayapiv1.Gateway) gatewayDAGNode {
		// Compute attached policies
		attachedPolicies := utils.Filter(policies, func(p Policy) bool {
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

func buildHTTPRouteDAGNodes(routes []*gatewayapiv1.HTTPRoute, policies []Policy) []httpRouteDAGNode {
	return utils.Map(routes, func(route *gatewayapiv1.HTTPRoute) httpRouteDAGNode {
		// Compute attached policies
		attachedPolicies := utils.Filter(policies, func(p Policy) bool {
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

func (g *Topology) Gateways() []GatewayNode {
	gatewayNodes := g.graph.GetNodes(typeField, gatewayLabel)

	return utils.Map(gatewayNodes, func(n dag.Node) GatewayNode {
		gNode, ok := n.(gatewayDAGNode)
		if !ok { // should not happen
			g.Logger.Error(
				fmt.Errorf("node ID %s type %T", n.ID(), n),
				"DAG gateway index returns nodes that are not gateways",
			)
			return GatewayNode{}
		}

		routeNodes := g.graph.Children(gNode.ID())
		// convert to "RouteNode" from httpRouteDAGNode
		routes := utils.Map(routeNodes, func(r dag.Node) RouteNode {
			rDAGNode, ok := r.(httpRouteDAGNode)
			if !ok { // should not happen
				g.Logger.Error(
					fmt.Errorf("node ID %s type %T", n.ID(), n),
					"DAG index returns gateway children that are not routes",
				)
				return RouteNode{}
			}
			return RouteNode(rDAGNode)
		})

		return GatewayNode{
			Gateway:          gNode.Gateway,
			attachedPolicies: gNode.attachedPolicies,
			routes:           routes,
		}
	})
}

func (g *Topology) Routes() []RouteNode {
	routeNodes := g.graph.GetNodes(typeField, httprouteLabel)

	return utils.Map(routeNodes, func(r dag.Node) RouteNode {
		rNode, ok := r.(httpRouteDAGNode)
		if !ok { // should not happen
			g.Logger.Error(
				fmt.Errorf("node ID %s type %T", r.ID(), r),
				"DAG route index returns nodes that are not routes",
			)
			return RouteNode{}
		}
		return RouteNode(rNode)
	})
}
