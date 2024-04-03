package dag

import (
	"errors"
	"fmt"

	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
)

// A Directed Acyclic Graph (DAG) is a graph representing a structure formed by vertices, or nodes,
// connected by directed edges.
// In a DAG, each edge has an initial node, called the parent, and a final node, called the child.
// The graph is considered acyclic because it does not contain any cycles,
// meaning there are no sequences of consecutive directed edges that form a closed loop.
// NOTE: this package is not thread-safe

type NodeID = string

type nodeNotFoundError struct {
	id NodeID
}

func (e *nodeNotFoundError) Error() string {
	return fmt.Sprintf("node %s not found", e.id)
}

func IsNodeNotFound(err error) bool {
	var nodeNotFoundErr *nodeNotFoundError
	return errors.As(err, &nodeNotFoundErr)
}

type Node interface {
	ID() NodeID
}

type internalNode struct {
	id       NodeID
	node     Node
	parents  map[NodeID]*internalNode
	children map[NodeID]*internalNode
}

type Options struct {
	fieldIndexers []FieldIndexer
}

type NodeLabel = string
type Field = string

type IndexerFunc func(Node) []NodeLabel

func WithFieldIndexer(f Field, e IndexerFunc) *FieldIndexer {
	return &FieldIndexer{f, e}
}

type FieldIndexer struct {
	field   Field
	indexer IndexerFunc
}

func (f FieldIndexer) ApplyTo(opts *Options) {
	opts.fieldIndexers = append(opts.fieldIndexers, f)
}

type Opt interface {
	// ApplyTo applies this configuration to the given options.
	ApplyTo(*Options)
}

var _ Opt = FieldIndexer{}

type DAG struct {
	nodes         map[NodeID]*internalNode
	fieldIndexers []FieldIndexer
	nodeIndexes   map[Field]map[NodeLabel][]Node
}

func NewDAG(opts ...Opt) *DAG {
	// Capture options
	dagOpts := &Options{}
	for _, opt := range opts {
		opt.ApplyTo(dagOpts)
	}

	return &DAG{
		nodes:         make(map[NodeID]*internalNode),
		fieldIndexers: dagOpts.fieldIndexers,
		nodeIndexes:   make(map[Field]map[NodeLabel][]Node),
	}
}

func (d *DAG) AddNode(node Node) error {
	n := &internalNode{
		id:       node.ID(),
		node:     node,
		parents:  make(map[string]*internalNode),
		children: make(map[string]*internalNode),
	}

	if _, exists := d.nodes[n.id]; exists {
		return fmt.Errorf("node %s already exists", n.id)
	}

	d.nodes[n.id] = n
	d.populateIndexes(node)

	return nil
}

func (d *DAG) populateIndexes(node Node) {
	for _, fieldIndexer := range d.fieldIndexers {
		nodelabels := fieldIndexer.indexer(node)
		field := fieldIndexer.field
		if d.nodeIndexes[field] == nil {
			d.nodeIndexes[field] = make(map[NodeLabel][]Node)
		}
		for _, nodeLabel := range nodelabels {
			d.nodeIndexes[field][nodeLabel] = append(d.nodeIndexes[field][nodeLabel], node)
		}
	}
}

func (d *DAG) AddEdge(parent NodeID, child NodeID) error {
	parentInternalNode, parentExists := d.nodes[parent]
	if !parentExists {
		return fmt.Errorf("parent node %s must exist", parent)
	}

	childInternalNode, childExists := d.nodes[child]
	if !childExists {
		return fmt.Errorf("child node %s must exist", child)
	}

	if _, exists := parentInternalNode.children[childInternalNode.id]; exists {
		return fmt.Errorf("parent node %s already has an edge with child %s", parentInternalNode.id, childInternalNode.id)
	}

	if _, exists := childInternalNode.parents[parentInternalNode.id]; exists {
		return fmt.Errorf("child node %s already has an edge with parent %s", childInternalNode.id, parentInternalNode.id)
	}

	parentInternalNode.children[childInternalNode.id] = childInternalNode
	childInternalNode.parents[parentInternalNode.id] = parentInternalNode

	return nil
}

// Parents return all parents of the node.
func (d *DAG) Parents(n NodeID) []Node {
	internalNode, exists := d.nodes[n]
	if !exists {
		return nil
	}

	result := make([]Node, 0)

	for _, parent := range internalNode.parents {
		result = append(result, parent.node)
	}

	return result
}

// Children return all children of the node.
func (d *DAG) Children(n NodeID) []Node {
	internalNode, exists := d.nodes[n]
	if !exists {
		return nil
	}

	result := make([]Node, 0)

	for _, child := range internalNode.children {
		result = append(result, child.node)
	}

	return result
}

func (d *DAG) GetNode(n NodeID) (Node, error) {
	internalNode, exists := d.nodes[n]
	if !exists {
		return nil, &nodeNotFoundError{id: n}
	}

	return internalNode.node, nil
}

// GetNodes returns a list of nodes. Indexes are required in the constructor
func (d *DAG) GetNodes(field Field, label NodeLabel) []Node {
	if fieldIndex, fieldExists := d.nodeIndexes[field]; fieldExists {
		if nodeList, labelExists := fieldIndex[label]; labelExists {
			return nodeList
		}
	}

	return nil
}

// Validate validates the DAG. A DAG is valid if it has no cycles.
func (d *DAG) Validate() bool {
	// Based on Kahn's algorithm
	// https://en.wikipedia.org/wiki/Topological_sorting

	type node struct {
		id       string
		parents  map[string]interface{}
		children []*node
	}

	type graph struct {
		nodes []*node
	}

	// build a mutable simple graph representation out of DAG only for validating purposes
	build := func() *graph {
		g := &graph{
			nodes: make([]*node, 0),
		}

		nodeIndex := make(map[string]*node)

		// the index needs to be built before populating parents and children
		for id := range d.nodes {
			nodeIndex[id] = &node{
				id:       id,
				parents:  make(map[string]interface{}),
				children: make([]*node, 0),
			}
		}

		for id, n := range d.nodes {
			simpleNode := nodeIndex[id] // should exist
			if simpleNode == nil {
				panic("it should not happen")
			}
			for parentID := range n.parents {
				simpleNode.parents[parentID] = nil
			}
			for childID := range n.children {
				simpleNode.children = append(simpleNode.children, nodeIndex[childID])
			}
		}

		for _, simpleNode := range nodeIndex {
			g.nodes = append(g.nodes, simpleNode)
		}

		return g
	}

	g := build()

	// S: Set of all nodes with no incoming edge
	s := utils.Filter(g.nodes, func(n *node) bool { return len(n.parents) == 0 })

	for len(s) != 0 {
		var n *node
		// remove a node n from S
		n, s = s[0], s[1:]

		// for each node m with an edge e from n to m do
		for len(n.children) != 0 {
			var m *node
			// remove edge e from the graph
			m, n.children = n.children[0], n.children[1:]
			delete(m.parents, n.id)
			// if m has no other incoming edges then insert m into S
			if len(m.parents) == 0 {
				s = append(s, m)
			}
		}
	}

	for _, n := range g.nodes {
		if len(n.parents) > 0 {
			// if graph has edges then return error
			// graph has at least one cycle
			return false
		}
	}

	return true
}
