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

type PolicyTargetNode interface {
	GetGatewayNode() *GatewayNode
	GetRouteNode() *RouteNode
}

type PolicyNode struct {
	policyDAGNode

	graph *dag.DAG
}

func (p *PolicyNode) GetPolicy() Policy {
	return p.Policy
}

func (p *PolicyNode) TargetRef() PolicyTargetNode {
	targetNodes := p.graph.Parents(p.ID())

	if len(targetNodes) == 0 {
		return nil
	}

	// there should be only one
	switch typedNode := targetNodes[0].(type) {
	case gatewayDAGNode:
		return &GatewayNode{typedNode, p.graph}
	case httpRouteDAGNode:
		return &RouteNode{typedNode, p.graph}
	}

	return nil
}

type RouteNode struct {
	httpRouteDAGNode

	graph *dag.DAG
}

var _ PolicyTargetNode = &RouteNode{}

func (r *RouteNode) AttachedPolicies() []PolicyNode {
	// get children of Policy kind
	policyNodeList := utils.Filter(r.graph.Children(r.ID()), func(n dag.Node) bool {
		_, ok := n.(policyDAGNode)
		return ok
	})

	return utils.Map(policyNodeList, func(n dag.Node) PolicyNode {
		return PolicyNode{n.(policyDAGNode), r.graph}
	})
}

func (r *RouteNode) Route() *gatewayapiv1.HTTPRoute {
	return r.HTTPRoute
}

func (r *RouteNode) ObjectKey() client.ObjectKey {
	return client.ObjectKeyFromObject(r.HTTPRoute)
}

func (r *RouteNode) GetGatewayNode() *GatewayNode {
	return nil
}

func (r *RouteNode) GetRouteNode() *RouteNode {
	return r
}

type GatewayNode struct {
	gatewayDAGNode

	graph *dag.DAG
}

var _ PolicyTargetNode = &GatewayNode{}

func (g *GatewayNode) AttachedPolicies() []PolicyNode {
	// get children of Policy kind
	policyNodeList := utils.Filter(g.graph.Children(g.ID()), func(n dag.Node) bool {
		_, ok := n.(policyDAGNode)
		return ok
	})

	return utils.Map(policyNodeList, func(n dag.Node) PolicyNode {
		return PolicyNode{n.(policyDAGNode), g.graph}
	})
}

func (g *GatewayNode) GetGateway() *gatewayapiv1.Gateway {
	return g.Gateway
}

func (g *GatewayNode) Routes() []RouteNode {
	// get children of httproute kind
	routeNodeList := utils.Filter(g.graph.Children(g.ID()), func(n dag.Node) bool {
		_, ok := n.(httpRouteDAGNode)
		return ok
	})

	return utils.Map(routeNodeList, func(n dag.Node) RouteNode {
		routeDAGNode := n.(httpRouteDAGNode)
		return RouteNode{routeDAGNode, g.graph}
	})
}

func (g *GatewayNode) ObjectKey() client.ObjectKey {
	return client.ObjectKeyFromObject(g.Gateway)
}

func (g *GatewayNode) GetGatewayNode() *GatewayNode {
	return g
}

func (g *GatewayNode) GetRouteNode() *RouteNode {
	return nil
}

// Topology defines a graph with Gateway API entities.
// Contains GatewayNodes (Gateway API gateways)
// Contains RouteNodes (Gateway API httproutes)
// Contains PolicyNodes (Gateway API policy attachment objects)
// Hierarchy is as follows.
// GatewayNode children can be either RouteNode or PolicyNode nodes
// RouteNode children are PolicyNode nodes
// PolicyNode parents can be either RouteNode or GatewayNode nodes
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

func (g gatewayDAGNode) ID() dag.NodeID {
	return dagNodeIDFromObject(g.Gateway)
}

type httpRouteDAGNode struct {
	*gatewayapiv1.HTTPRoute
}

func (h httpRouteDAGNode) ID() dag.NodeID {
	return dagNodeIDFromObject(h.HTTPRoute)
}

type policyDAGNode struct {
	Policy
}

func (p policyDAGNode) ID() dag.NodeID {
	return dagNodeIDFromObject(p.Policy)
}

type topologyOptions struct {
	gateways               []*gatewayapiv1.Gateway
	routes                 []*gatewayapiv1.HTTPRoute
	policies               []Policy
	logger                 logr.Logger
	programmedGatewaysOnly bool
	linkAcceptedRoutesOnly bool
}

// TopologyOpts allows to manipulate topologyOptions.
type TopologyOpts func(*topologyOptions)

func WithAcceptedRoutesLinkedOnly() TopologyOpts {
	return func(o *topologyOptions) {
		o.linkAcceptedRoutesOnly = true
	}
}

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

func WithProgrammedGatewaysOnly(programmedGatewaysOnly bool) TopologyOpts {
	return func(o *topologyOptions) {
		o.programmedGatewaysOnly = programmedGatewaysOnly
	}
}

func NewTopology(opts ...TopologyOpts) (*Topology, error) {
	// defaults
	o := &topologyOptions{
		logger:                 logr.Discard(),
		programmedGatewaysOnly: false,
		linkAcceptedRoutesOnly: false,
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

	edges := buildDAGEdges(o, gatewayDAGNodes, routeDAGNodes, policyDAGNodes)

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

func buildDAGEdges(opts *topologyOptions, gateways []gatewayDAGNode, routes []httpRouteDAGNode, policies []policyDAGNode) []edge {
	effectiveGatewys := gateways

	if opts.programmedGatewaysOnly {
		// filter out not programmed gateways
		effectiveGatewys = utils.Filter(gateways, func(g gatewayDAGNode) bool {
			return meta.IsStatusConditionTrue(g.Status.Conditions, string(gatewayapiv1.GatewayConditionProgrammed))
		})
	}

	// internal index: key -> gateway for reference
	gatewaysIndex := make(map[client.ObjectKey]gatewayDAGNode, len(effectiveGatewys))
	for _, gateway := range effectiveGatewys {
		gatewaysIndex[client.ObjectKeyFromObject(gateway.Gateway)] = gateway
	}

	edges := make([]edge, 0)

	for _, route := range routes {
		gatewayParentKeys := GetGatewayParentKeys(route.HTTPRoute)

		if opts.linkAcceptedRoutesOnly {
			gatewayParentKeys = GetRouteAcceptedGatewayParentKeys(route.HTTPRoute)
		}

		for _, parentKey := range gatewayParentKeys {
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

	for _, g := range effectiveGatewys {
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

		return GatewayNode{gNode, g.graph}
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
		return RouteNode{rNode, g.graph}
	})
}

func (g *Topology) Policies() []PolicyNode {
	policyNodes := g.graph.GetNodes(typeField, policyLabel)

	return utils.Map(policyNodes, func(r dag.Node) PolicyNode {
		pNode, ok := r.(policyDAGNode)
		if !ok { // should not happen
			g.Logger.Error(
				fmt.Errorf("node ID %s type %T", r.ID(), r),
				"DAG policy index returns nodes that are not policies",
			)
			return PolicyNode{}
		}
		return PolicyNode{pNode, g.graph}
	})
}

func (g *Topology) GetPolicy(policy Policy) (PolicyNode, bool) {
	dagNode, err := g.graph.GetNode(policyDAGNode{policy}.ID())
	if err != nil {
		return PolicyNode{}, false
	}

	pNode, ok := dagNode.(policyDAGNode)
	if !ok {
		g.Logger.Error(
			fmt.Errorf("policy key %s with node ID %s type %T",
				client.ObjectKeyFromObject(policy), dagNode.ID(), dagNode),
			"the policy ID conflicts with another graph node type",
		)
		return PolicyNode{}, false
	}
	return PolicyNode{pNode, g.graph}, true
}
