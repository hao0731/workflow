package engine

import (
	"context"
	"sync"

	"github.com/cheriehsieh/orchestration/internal/eventstore"
)

// JoinState tracks pending join nodes waiting for all predecessors.
type JoinState struct {
	mu     sync.RWMutex
	states map[string]*PendingJoin // key: executionID:joinNodeID
}

// PendingJoin tracks the state of a join node waiting for inputs.
type PendingJoin struct {
	ExecutionID     string
	JoinNodeID      string
	WorkflowID      string
	RequiredNodes   []string                  // Predecessor node IDs
	CompletedNodes  map[string]bool           // Which predecessors have completed
	CollectedInputs map[string]map[string]any // ToPort -> output data
	RunIndex        int
}

// NewJoinState creates a new join state tracker.
func NewJoinState() *JoinState {
	return &JoinState{
		states: make(map[string]*PendingJoin),
	}
}

func (js *JoinState) key(executionID, joinNodeID string) string {
	return executionID + ":" + joinNodeID
}

// GetOrCreate returns existing pending join or creates a new one.
func (js *JoinState) GetOrCreate(executionID, joinNodeID, workflowID string, requiredNodes []string) *PendingJoin {
	js.mu.Lock()
	defer js.mu.Unlock()

	key := js.key(executionID, joinNodeID)
	if pj, exists := js.states[key]; exists {
		return pj
	}

	pj := &PendingJoin{
		ExecutionID:     executionID,
		JoinNodeID:      joinNodeID,
		WorkflowID:      workflowID,
		RequiredNodes:   requiredNodes,
		CompletedNodes:  make(map[string]bool),
		CollectedInputs: make(map[string]map[string]any),
	}
	js.states[key] = pj
	return pj
}

// MarkCompleted marks a predecessor as completed and returns true if all are done.
func (js *JoinState) MarkCompleted(executionID, joinNodeID, fromNodeID, toPort string, outputData map[string]any) (allDone bool, inputs map[string]any) {
	js.mu.Lock()
	defer js.mu.Unlock()

	key := js.key(executionID, joinNodeID)
	pj, exists := js.states[key]
	if !exists {
		return false, nil
	}

	pj.CompletedNodes[fromNodeID] = true
	if toPort == "" {
		toPort = fromNodeID // Use node ID as default port name
	}
	pj.CollectedInputs[toPort] = outputData

	// Check if all required nodes have completed
	for _, req := range pj.RequiredNodes {
		if !pj.CompletedNodes[req] {
			return false, nil
		}
	}

	// All done - collect inputs and clean up
	inputs = make(map[string]any)
	for port, data := range pj.CollectedInputs {
		inputs[port] = data
	}
	delete(js.states, key)

	return true, inputs
}

// Remove cleans up join state for an execution.
func (js *JoinState) Remove(executionID, joinNodeID string) {
	js.mu.Lock()
	defer js.mu.Unlock()
	delete(js.states, js.key(executionID, joinNodeID))
}

// CollectJoinInputsFromEvents queries the event store for completed nodes to build join inputs.
// This is used for recovery after restart.
func CollectJoinInputsFromEvents(ctx context.Context, es eventstore.EventStore, executionID string, workflow *Workflow, joinNodeID string) (map[string]any, bool) {
	events, err := es.GetBySubject(ctx, executionID)
	if err != nil {
		return nil, false
	}

	predecessors := workflow.GetPredecessors(joinNodeID)
	connections := workflow.GetConnectionsTo(joinNodeID)

	// Build a map of FromNode -> ToPort
	portMap := make(map[string]string)
	for _, c := range connections {
		toPort := c.ToPort
		if toPort == "" {
			toPort = c.FromNode
		}
		portMap[c.FromNode] = toPort
	}

	completedOutputs := make(map[string]map[string]any)
	for _, e := range events {
		if e.Type() != NodeExecutionCompleted {
			continue
		}
		var data NodeExecutionCompletedData
		if err := e.DataAs(&data); err != nil {
			continue
		}
		completedOutputs[data.NodeID] = data.OutputData
	}

	// Check if all predecessors have completed
	inputs := make(map[string]any)
	for _, pred := range predecessors {
		output, ok := completedOutputs[pred]
		if !ok {
			return nil, false // Not all predecessors done
		}
		port := portMap[pred]
		inputs[port] = output
	}

	return inputs, true
}

// IsJoinNode checks if a node is a join node.
func IsJoinNode(n *Node) bool {
	return n != nil && n.Type == JoinNode
}

// GetJoinTargets returns join nodes that a completed node feeds into.
func GetJoinTargets(workflow *Workflow, completedNodeID string) []string {
	var joins []string
	for _, c := range workflow.Connections {
		if c.FromNode == completedNodeID {
			node := workflow.GetNode(c.ToNode)
			if IsJoinNode(node) {
				joins = append(joins, c.ToNode)
			}
		}
	}
	return joins
}

// FindToPort finds the ToPort for a connection from sourceNode to targetNode.
func FindToPort(workflow *Workflow, sourceNode, targetNode string) string {
	for _, c := range workflow.Connections {
		if c.FromNode == sourceNode && c.ToNode == targetNode {
			if c.ToPort != "" {
				return c.ToPort
			}
			return sourceNode
		}
	}
	return sourceNode
}
