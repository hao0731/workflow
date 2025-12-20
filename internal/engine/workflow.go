package engine

type NodeType string

const (
	StartNode  NodeType = "StartNode"
	ActionNode NodeType = "ActionNode"
)

type Node struct {
	ID         string         `json:"id"`
	Type       NodeType       `json:"type"`
	Name       string         `json:"name"`
	Parameters map[string]any `json:"parameters"`
}

type Connection struct {
	FromNode string `json:"from_node"`
	ToNode   string `json:"to_node"`
}

type Workflow struct {
	ID          string       `json:"id"`
	Nodes       []Node       `json:"nodes"`
	Connections []Connection `json:"connections"`
}

func (w *Workflow) GetNode(id string) *Node {
	for i := range w.Nodes {
		if w.Nodes[i].ID == id {
			return &w.Nodes[i]
		}
	}
	return nil
}

func (w *Workflow) GetNextNodes(nodeID string) []string {
	var next []string
	for _, c := range w.Connections {
		if c.FromNode == nodeID {
			next = append(next, c.ToNode)
		}
	}
	return next
}

func (w *Workflow) GetStartNode() *Node {
	for i := range w.Nodes {
		if w.Nodes[i].Type == StartNode {
			return &w.Nodes[i]
		}
	}
	return nil
}
