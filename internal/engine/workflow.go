package engine

// DefaultPort is the default output port name.
const DefaultPort = "default"

// Common port names for convenience.
const (
	PortSuccess = "success"
	PortFailure = "failure"
	PortTrue    = "true"
	PortFalse   = "false"
)

// JoinOperator defines how a join node waits for inputs.
type JoinOperator string

const (
	JoinAll JoinOperator = "all" // Wait for ALL predecessors (AND)
	JoinAny JoinOperator = "any" // Wait for ANY predecessor (OR)
)

// TriggerType defines how a workflow can be started.
type TriggerType string

const (
	TriggerManual TriggerType = "manual" // Started via API call
	TriggerEvent  TriggerType = "event"  // Started by marketplace event
)

// Trigger configures how a StartNode is activated.
type Trigger struct {
	Type     TriggerType    `json:"type"`
	Criteria map[string]any `json:"criteria,omitempty"`  // e.g., {"event_name": "order.created", "domain": "ecommerce"}
	InputMap map[string]any `json:"input_map,omitempty"` // Maps event fields to workflow inputs
}

type NodeType string

const (
	StartNode    NodeType = "StartNode"
	ActionNode   NodeType = "ActionNode"
	IfNode       NodeType = "IfNode"
	JoinNode     NodeType = "JoinNode"     // Waits for multiple inputs
	PublishEvent NodeType = "PublishEvent" // Publishes to marketplace
)

type Node struct {
	ID         string         `json:"id"`
	Type       NodeType       `json:"type"`
	FullType   string         `json:"full_type,omitempty"`
	Name       string         `json:"name"`
	Parameters map[string]any `json:"parameters"`
	Trigger    *Trigger       `json:"trigger,omitempty"` // Only for StartNode
}

// DispatchType returns the versioned identity used for worker routing.
func (n *Node) DispatchType() string {
	if n == nil {
		return ""
	}
	if n.FullType != "" {
		return n.FullType
	}
	if n.Parameters != nil {
		if fullType, ok := n.Parameters["full_type"].(string); ok && fullType != "" {
			return fullType
		}
	}
	return string(n.Type)
}

// JoinConfig returns join configuration from node parameters.
func (n *Node) JoinConfig() (JoinOperator, []string) {
	if n.Type != JoinNode {
		return "", nil
	}

	op := JoinAll // Default to "all"
	if opStr, ok := n.Parameters["operator"].(string); ok {
		op = JoinOperator(opStr)
	}

	var inputs []string
	if inputsRaw, ok := n.Parameters["inputs"].([]any); ok {
		for _, v := range inputsRaw {
			if s, ok := v.(string); ok {
				inputs = append(inputs, s)
			}
		}
	}

	return op, inputs
}

// GetEventTrigger returns the event trigger criteria if this is an event-triggered start node.
func (n *Node) GetEventTrigger() (eventName, domain string, ok bool) {
	if n.Type != StartNode || n.Trigger == nil || n.Trigger.Type != TriggerEvent {
		return "", "", false
	}
	eventName, _ = n.Trigger.Criteria["event_name"].(string)
	domain, _ = n.Trigger.Criteria["domain"].(string)
	return eventName, domain, eventName != ""
}

type Connection struct {
	FromNode string `json:"from_node"`
	FromPort string `json:"from_port,omitempty"` // Output port name
	ToNode   string `json:"to_node"`
	ToPort   string `json:"to_port,omitempty"` // Input port name (for join nodes)
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

// GetNextNodes returns nodes connected to the given node's output port.
func (w *Workflow) GetNextNodes(nodeID, port string) []string {
	if port == "" {
		port = DefaultPort
	}

	var next []string
	for _, c := range w.Connections {
		if c.FromNode != nodeID {
			continue
		}

		connPort := c.FromPort
		if connPort == "" {
			connPort = DefaultPort
		}

		if connPort == port {
			next = append(next, c.ToNode)
		}
	}
	return next
}

// GetConnectionsTo returns all connections pointing to a node.
func (w *Workflow) GetConnectionsTo(nodeID string) []Connection {
	var conns []Connection
	for _, c := range w.Connections {
		if c.ToNode == nodeID {
			conns = append(conns, c)
		}
	}
	return conns
}

// GetPredecessors returns IDs of all nodes that connect to this node.
func (w *Workflow) GetPredecessors(nodeID string) []string {
	var preds []string
	for _, c := range w.Connections {
		if c.ToNode == nodeID {
			preds = append(preds, c.FromNode)
		}
	}
	return preds
}

func (w *Workflow) GetStartNode() *Node {
	for i := range w.Nodes {
		if w.Nodes[i].Type == StartNode {
			return &w.Nodes[i]
		}
	}
	return nil
}

// HasEventTrigger returns true if this workflow has a StartNode with an event trigger.
func (w *Workflow) HasEventTrigger() bool {
	start := w.GetStartNode()
	if start == nil {
		return false
	}
	_, _, ok := start.GetEventTrigger()
	return ok
}
