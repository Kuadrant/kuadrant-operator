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
	policyLabel    dag.NodeLabel = dag.NodeLabel("policy")
)

type PolicyNode struct {
	Policy
}

func (p *PolicyNode) GetPolicy() Policy {
	return p.Policy
}

type RouteNode struct {
	*gatewayapiv1.HTTPRoute

	graph  *dag.DAG
	nodeID string
}

func (r *RouteNode) AttachedPolicies() []Policy {
	// get children of Policy kind
	policyNodeList := utils.Filter(r.graph.Children(r.nodeID), func(n dag.Node) bool {
		_, ok := n.(policyDAGNode)
		return ok
	})

	return utils.Map(policyNodeList, func(n dag.Node) Policy {
		policyDAGNode := n.(policyDAGNode)
		return policyDAGNode.Policy
	})
}

func (r *RouteNode) Route() *gatewayapiv1.HTTPRoute {
	return r.HTTPRoute
}

type GatewayNode struct {
	*gatewayapiv1.Gateway

	graph  *dag.DAG
	nodeID string
}

func (g *GatewayNode) AttachedPolicies() []Policy {
	// get children of Policy kind
	policyNodeList := utils.Filter(g.graph.Children(g.nodeID), func(n dag.Node) bool {
		_, ok := n.(policyDAGNode)
		return ok
	})

	return utils.Map(policyNodeList, func(n dag.Node) Policy {
		policyDAGNode := n.(policyDAGNode)
		return policyDAGNode.Policy
	})
}

func (g *GatewayNode) Routes() []RouteNode {
	// get children of httproute kind
	routeNodeList := utils.Filter(g.graph.Children(g.nodeID), func(n dag.Node) bool {
		_, ok := n.(httpRouteDAGNode)
		return ok
	})

	return utils.Map(routeNodeList, func(n dag.Node) RouteNode {
		routeDAGNode := n.(httpRouteDAGNode)
		return RouteNode{HTTPRoute: routeDAGNode.HTTPRoute, graph: g.graph, nodeID: n.ID()}
	})
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
}

func dagNodeIDFromObject(obj client.Object) dag.NodeID {
	return fmt.Sprintf("%s#%s", obj.GetObjectKind().GroupVersionKind().String(), client.ObjectKeyFromObject(obj).String())
}

func (g gatewayDAGNode) ID() string {
	return dagNodeIDFromObject(g.Gateway)
}

type httpRouteDAGNode struct {
	*gatewayapiv1.HTTPRoute
}

func (h httpRouteDAGNode) ID() string {
	return dagNodeIDFromObject(h.HTTPRoute)
}

type policyDAGNode struct {
	Policy
}

func (p policyDAGNode) ID() string {
	return dagNodeIDFromObject(p.Policy)
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
		case policyDAGNode:
			return []dag.NodeLabel{policyLabel}
		default:
			return nil
		}
	})

	graph := dag.NewDAG(typeIndexer)

	gatewayDAGNodes := utils.Map(o.gateways, func(g *gatewayapiv1.Gateway) gatewayDAGNode {
		return gatewayDAGNode{Gateway: g}
	})

	routeDAGNodes := utils.Map(o.routes, func(route *gatewayapiv1.HTTPRoute) httpRouteDAGNode {
		return httpRouteDAGNode{HTTPRoute: route}
	})

	policyDAGNodes := utils.Map(o.policies, func(policy Policy) policyDAGNode {
		return policyDAGNode{Policy: policy}
	})

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

	for _, node := range policyDAGNodes {
		err := graph.AddNode(node)
		if err != nil {
			return nil, err
		}
	}

	edges := buildDAGEdges(gatewayDAGNodes, routeDAGNodes, policyDAGNodes)

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

func buildDAGEdges(gateways []gatewayDAGNode, routes []httpRouteDAGNode, policies []policyDAGNode) []edge {
	// filter out not programmed gateways
	programmedGateways := utils.Filter(gateways, func(g gatewayDAGNode) bool {
		return meta.IsStatusConditionTrue(g.Status.Conditions, string(gatewayapiv1.GatewayConditionProgrammed))
	})

	// internal index: key -> gateway for reference
	gatewaysIndex := make(map[client.ObjectKey]gatewayDAGNode, len(programmedGateways))
	for _, gateway := range programmedGateways {
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

		// Compute route's child (attached) policies
		attachedPolicies := utils.Filter(policies, func(p policyDAGNode) bool {
			group := p.GetTargetRef().Group
			kind := p.GetTargetRef().Kind
			name := p.GetTargetRef().Name
			namespace := ptr.Deref(p.GetTargetRef().Namespace, gatewayapiv1.Namespace(p.GetNamespace()))

			return group == gatewayapiv1.GroupName &&
				kind == "HTTPRoute" &&
				name == gatewayapiv1.ObjectName(route.Name) &&
				namespace == gatewayapiv1.Namespace(route.Namespace)
		})

		for _, attachedPolicy := range attachedPolicies {
			edges = append(edges, edge{parent: route, child: attachedPolicy})
		}

	}

	for _, g := range programmedGateways {
		// Compute gateway's child (attached) policies
		attachedPolicies := utils.Filter(policies, func(p policyDAGNode) bool {
			group := p.GetTargetRef().Group
			kind := p.GetTargetRef().Kind
			name := p.GetTargetRef().Name
			namespace := ptr.Deref(p.GetTargetRef().Namespace, gatewayapiv1.Namespace(p.GetNamespace()))

			return group == gatewayapiv1.GroupName &&
				kind == "Gateway" &&
				name == gatewayapiv1.ObjectName(g.Name) &&
				namespace == gatewayapiv1.Namespace(g.Namespace)
		})

		for _, attachedPolicy := range attachedPolicies {
			edges = append(edges, edge{parent: g, child: attachedPolicy})
		}
	}

	return edges
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

		return GatewayNode{Gateway: gNode.Gateway, graph: g.graph, nodeID: gNode.ID()}
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
		return RouteNode{HTTPRoute: rNode.HTTPRoute, graph: g.graph, nodeID: rNode.ID()}
	})
}

func (g *Topology) Policies() []PolicyNode {
	policyNodes := g.graph.GetNodes(typeField, policyLabel)

	return utils.Map(policyNodes, func(r dag.Node) PolicyNode {
		pNode, ok := r.(policyDAGNode)
		if !ok { // should not happen
			g.Logger.Error(
				fmt.Errorf("node ID %s type %T", r.ID(), r),
				"DAG route index returns nodes that are not routes",
			)
			return PolicyNode{}
		}
		return PolicyNode{Policy: pNode.Policy}
	})
}
