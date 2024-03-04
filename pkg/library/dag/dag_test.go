//go:build unit

package dag

import (
	"errors"
	"testing"

	"gotest.tools/assert"

	"github.com/kuadrant/kuadrant-operator/pkg/library/utils"
)

type NodeTest string

func (n NodeTest) ID() string {
	return string(n)
}

type NodeTest2 string

func (n NodeTest2) ID() string {
	return string(n)
}

func TestDAGValidate(t *testing.T) {
	t.Run("empty DAG is valid", func(subT *testing.T) {
		d := NewDAG()
		assert.Assert(subT, d.Validate(), "empty DAG is not valid")
	})

	t.Run("DAG edgeless is valid", func(subT *testing.T) {
		d := NewDAG()
		nodes := []Node{
			NodeTest("0"),
			NodeTest("1"),
			NodeTest("2"),
			NodeTest("3"),
			NodeTest("4"),
		}

		for _, node := range nodes {
			assert.NilError(subT, d.AddNode(node))
		}

		assert.Assert(subT, d.Validate(), "edgeless DAG is not valid")
	})

	t.Run("DAG without roots with cycles is not valid", func(subT *testing.T) {
		d := NewDAG()

		nodes := []Node{
			NodeTest("0"),
			NodeTest("1"),
			NodeTest("2"),
			NodeTest("3"),
			NodeTest("4"),
		}

		for _, node := range nodes {
			assert.NilError(subT, d.AddNode(node))
		}

		// all nodes have some parent
		edges := []struct {
			parent NodeID
			child  NodeID
		}{
			{"0", "1"},
			{"1", "2"},
			{"2", "3"},
			{"3", "0"},
			{"0", "4"},
			{"4", "2"},
		}

		for _, edge := range edges {
			assert.NilError(subT, d.AddEdge(edge.parent, edge.child))
		}

		assert.Assert(subT, !d.Validate(), "DAG with cycles should not be valid")
	})

	t.Run("DAG with roots with cycles is not valid", func(subT *testing.T) {
		d := NewDAG()

		nodes := []Node{
			NodeTest("0"),
			NodeTest("1"),
			NodeTest("2"),
			NodeTest("3"),
		}

		for _, node := range nodes {
			assert.NilError(subT, d.AddNode(node))
		}

		// 0 node has no parent
		edges := []struct {
			parent NodeID
			child  NodeID
		}{
			{"0", "1"},
			{"0", "3"},
			{"1", "2"},
			{"2", "3"},
			{"3", "1"},
		}

		for _, edge := range edges {
			assert.NilError(subT, d.AddEdge(edge.parent, edge.child))
		}

		assert.Assert(subT, !d.Validate(), "DAG with cycles should not be valid")
	})

	t.Run("DAG without cycles is valid", func(subT *testing.T) {
		d := NewDAG()

		nodes := []Node{
			NodeTest("5"),
			NodeTest("7"),
			NodeTest("3"),
			NodeTest("11"),
			NodeTest("8"),
			NodeTest("2"),
			NodeTest("9"),
			NodeTest("10"),
		}

		for _, node := range nodes {
			assert.NilError(subT, d.AddNode(node))
		}

		edges := []struct {
			parent NodeID
			child  NodeID
		}{
			{"5", "11"},
			{"7", "11"},
			{"7", "8"},
			{"3", "8"},
			{"3", "9"},
			{"11", "2"},
			{"11", "9"},
			{"11", "10"},
			{"8", "9"},
		}

		for _, edge := range edges {
			assert.NilError(subT, d.AddEdge(edge.parent, edge.child))
		}

		assert.Assert(subT, d.Validate(), "DAG without cycles should be valid")
	})
}

func TestDAGIsNodeNotFound(t *testing.T) {
	t.Run("nil returns false", func(subT *testing.T) {
		if IsNodeNotFound(nil) {
			subT.Fatal("nil returns true")
		}
	})

	t.Run("errors.New returns false", func(subT *testing.T) {
		if IsNodeNotFound(errors.New("some error")) {
			subT.Fatal("errors.New returns true")
		}
	})

	t.Run("nodeNotFoundError instance returns true", func(subT *testing.T) {
		if !IsNodeNotFound(&nodeNotFoundError{id: "1"}) {
			subT.Fatal("should return true")
		}
	})
}

func TestDAGAddEdge(t *testing.T) {
	d := NewDAG()

	assert.NilError(t, d.AddNode(NodeTest("0")))
	assert.NilError(t, d.AddNode(NodeTest("1")))

	assert.Error(t, d.AddEdge("unknown", "0"), "parent node unknown must exist")

	assert.NilError(t, d.AddEdge("0", "1"))
}

func TestDAGParents(t *testing.T) {
	d := NewDAG()
	nodes := []Node{NodeTest("0"), NodeTest("1")}
	for _, node := range nodes {
		assert.NilError(t, d.AddNode(node))
	}

	assert.NilError(t, d.AddEdge("0", "1"))

	t.Run("unknown node returns empty", func(subT *testing.T) {
		assert.Assert(subT, len(d.Parents("unknown")) == 0, "unknown node returns not empty")
	})

	t.Run("node with parent returns expected nodes", func(subT *testing.T) {
		parents := d.Parents("1")
		assert.Assert(subT, utils.SameElements(parents, []Node{NodeTest("0")}), "unexpected parents", "parents", parents)
	})

	t.Run("node without parent returns empty", func(subT *testing.T) {
		parents := d.Parents("0")
		assert.Assert(subT, len(parents) == 0, "parents should be empty", "parents", parents)
	})
}

func TestDAGChildren(t *testing.T) {
	d := NewDAG()

	nodes := []Node{NodeTest("0"), NodeTest("1")}
	for _, node := range nodes {
		assert.NilError(t, d.AddNode(node))
	}

	assert.NilError(t, d.AddEdge("0", "1"))

	t.Run("unknown node returns empty", func(subT *testing.T) {
		assert.Assert(subT, len(d.Children("unknown")) == 0, "unknown node returns not empty")
	})

	t.Run("node with children returns expected nodes", func(subT *testing.T) {
		children := d.Children("0")
		assert.Assert(subT, utils.SameElements(children, []Node{NodeTest("1")}), "unexpected children", "children", children)
	})

	t.Run("node without children returns empty", func(subT *testing.T) {
		children := d.Children("1")
		assert.Assert(subT, len(children) == 0, "children should be empty", "children", children)
	})
}

func TestDAGGetNode(t *testing.T) {
	d := NewDAG()

	assert.NilError(t, d.AddNode(NodeTest("0")))
	assert.NilError(t, d.AddNode(NodeTest("1")))

	t.Run("unknown id returns not found", func(subT *testing.T) {
		_, err := d.GetNode("unknown")
		assert.Assert(subT, IsNodeNotFound(err), "unknown id does not return not found")
	})

	t.Run("existing id returns node", func(subT *testing.T) {
		for _, nodeID := range []string{"0", "1"} {
			node, err := d.GetNode(nodeID)
			assert.NilError(subT, err)

			nodeTest, ok := node.(NodeTest)
			assert.Assert(subT, ok, "unexpected node type", "nodeID", nodeID)
			assert.Equal(subT, nodeTest, NodeTest(nodeID))
		}
	})
}

func TestDAGIndexes(t *testing.T) {
	t.Run("empty indexer does not index nodes", func(subT *testing.T) {
		d := NewDAG()

		nodes := []Node{
			NodeTest("0"),
			NodeTest("1"),
		}

		for _, node := range nodes {
			assert.NilError(subT, d.AddNode(node))
		}

		assert.NilError(subT, d.AddEdge("0", "1"))

		assert.Assert(subT, d.Validate(), "DAG without cycles should be valid")

		indexedNodes := d.GetNodes(Field("1"), NodeLabel("somelabel"))
		assert.Assert(subT, len(indexedNodes) == 0, "empty index should not return any node")
	})

	t.Run("multiple indexes return expected nodes", func(subT *testing.T) {
		// root indexer will only label node 0 with root
		rootIndexer := WithFieldIndexer(Field("rootIndex"), func(n Node) []NodeLabel {
			if n.ID() == "0" {
				return []NodeLabel{NodeLabel("root")}
			}
			return nil
		})

		// every node will be labeled with node ID
		selfIDIndexer := WithFieldIndexer(Field("selfID"), func(n Node) []NodeLabel {
			return []NodeLabel{NodeLabel(n.ID())}
		})

		d := NewDAG(rootIndexer, selfIDIndexer)

		nodes := []Node{NodeTest("0"), NodeTest("1")}

		for _, node := range nodes {
			assert.NilError(subT, d.AddNode(node))
		}

		assert.NilError(subT, d.AddEdge("0", "1"))
		assert.Assert(subT, d.Validate(), "DAG without cycles should be valid")

		assert.Assert(subT, utils.SameElements(
			d.GetNodes(Field("rootIndex"), NodeLabel("root")),
			[]Node{NodeTest("0")},
		), "index for Field rootIndex and root label failed")

		for _, node := range nodes {
			assert.Assert(subT, utils.SameElements(
				d.GetNodes(Field("selfID"), NodeLabel(node.ID())),
				[]Node{NodeTest(node.ID())},
			), "index for Field selfID failed", "label", node.ID())
		}

		// mixing labels and fields does not work
		// rootIndexer does not generate any label other than "root"
		assert.Assert(subT, len(d.GetNodes(Field("rootIndex"), NodeLabel("0"))) == 0,
			"index for Field rootIndex has nodes indexed with '0' label")
		assert.Assert(subT, len(d.GetNodes(Field("rootIndex"), NodeLabel("1"))) == 0,
			"index for Field rootIndex has nodes indexed with '1' label")
		// selfIDIndexer does not generate any label other than node ID
		assert.Assert(subT, len(d.GetNodes(Field("selfID"), NodeLabel("root"))) == 0,
			"index for Field selfID has nodes indexed with 'root' label")
	})

	t.Run("multiple labels returns expected nodes", func(subT *testing.T) {
		nodeIndexer1 := WithFieldIndexer(Field("1"), func(n Node) []NodeLabel {
			nodeLabels := []NodeLabel{NodeLabel("commonLabel")}
			switch n.(type) {
			case NodeTest:
				return append(nodeLabels, NodeLabel("NodeTest"))
			case NodeTest2:
				return append(nodeLabels, NodeLabel("NodeTest2"))
			default:
				return nil
			}
		})

		d := NewDAG(nodeIndexer1)

		nodes := []Node{
			NodeTest("00"),
			NodeTest("01"),
			NodeTest2("20"),
			NodeTest2("21"),
		}

		for _, node := range nodes {
			assert.NilError(subT, d.AddNode(node))
		}

		edges := []struct {
			parent NodeID
			child  NodeID
		}{
			{"00", "01"},
			{"01", "20"},
			{"20", "21"},
		}

		for _, edge := range edges {
			assert.NilError(subT, d.AddEdge(edge.parent, edge.child))
		}

		assert.Assert(subT, d.Validate(), "DAG without cycles should be valid")

		indexedNodes := d.GetNodes(Field("1"), NodeLabel("NodeTest"))
		assert.Assert(subT, utils.SameElements(indexedNodes, []Node{NodeTest("00"), NodeTest("01")}),
			"index for Field 1 and label NodeTest failed")
		indexedNodes = d.GetNodes(Field("1"), NodeLabel("NodeTest2"))
		assert.Assert(subT, utils.SameElements(indexedNodes, []Node{NodeTest2("20"), NodeTest2("21")}),
			"index for Field 1 and label NodeTest2 failed")
		indexedNodes = d.GetNodes(Field("1"), NodeLabel("commonLabel"))
		assert.Assert(subT, utils.SameElements(indexedNodes, nodes), "index for Field 1 and label commonLabel failed")
	})
}
