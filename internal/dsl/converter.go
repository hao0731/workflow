package dsl

import (
	"fmt"
	"strings"

	"github.com/cheriehsieh/orchestration/internal/engine"
)

// WorkflowConverter transforms DSL models to engine models.
type WorkflowConverter interface {
	Convert(def *WorkflowDefinition) (*engine.Workflow, error)
}

// DefaultConverter implements the standard conversion logic.
type DefaultConverter struct{}

// NewDefaultConverter creates a new DefaultConverter.
func NewDefaultConverter() *DefaultConverter {
	return &DefaultConverter{}
}

// Convert transforms a WorkflowDefinition into an engine.Workflow.
func (c *DefaultConverter) Convert(def *WorkflowDefinition) (*engine.Workflow, error) {
	if def == nil {
		return nil, fmt.Errorf("definition is nil")
	}

	workflow := &engine.Workflow{
		ID:          def.ID,
		Nodes:       make([]engine.Node, 0, len(def.Nodes)),
		Connections: make([]engine.Connection, 0, len(def.Connections)),
	}

	// Convert nodes
	for _, nodeDef := range def.Nodes {
		node, err := c.convertNode(nodeDef)
		if err != nil {
			return nil, fmt.Errorf("failed to convert node %s: %w", nodeDef.ID, err)
		}
		workflow.Nodes = append(workflow.Nodes, node)
	}

	// Convert connections
	for _, connDef := range def.Connections {
		conn := engine.Connection{
			FromNode: connDef.From,
			FromPort: connDef.FromPort,
			ToNode:   connDef.To,
			ToPort:   connDef.ToPort,
		}
		workflow.Connections = append(workflow.Connections, conn)
	}

	return workflow, nil
}

// convertNode converts a DSL node definition to an engine node.
func (c *DefaultConverter) convertNode(nodeDef NodeDefinition) (engine.Node, error) { //nolint:unparam // error return reserved for future validation
	node := engine.Node{
		ID:         nodeDef.ID,
		Name:       nodeDef.Name,
		Parameters: copyParams(nodeDef.Parameters),
	}

	// Determine node type
	node.Type = c.resolveNodeType(nodeDef.Type)

	// For third-party nodes, store the original full_type
	if isThirdPartyNodeType(nodeDef.Type) {
		if node.Parameters == nil {
			node.Parameters = make(map[string]any)
		}
		node.Parameters["full_type"] = nodeDef.Type
	}

	// Convert trigger if present
	if nodeDef.Trigger != nil {
		node.Trigger = c.convertTrigger(nodeDef.Trigger)
	}

	return node, nil
}

// resolveNodeType maps DSL type strings to engine.NodeType.
func (c *DefaultConverter) resolveNodeType(dslType string) engine.NodeType {
	switch dslType {
	case "StartNode":
		return engine.StartNode
	case "JoinNode":
		return engine.JoinNode
	case "PublishEvent":
		return engine.PublishEvent
	case "IfNode":
		return engine.IfNode
	default:
		// Third-party registered nodes (e.g., "http-request@v1") are treated as ActionNodes
		return engine.ActionNode
	}
}

// isThirdPartyNodeType returns true if the type is a registered third-party node.
func isThirdPartyNodeType(nodeType string) bool {
	// Third-party nodes contain "@" version suffix or are not system types
	if strings.Contains(nodeType, "@") {
		return true
	}
	switch nodeType {
	case "StartNode", "JoinNode", "PublishEvent", "IfNode":
		return false
	default:
		return true
	}
}

// convertTrigger converts a DSL trigger to an engine trigger.
func (c *DefaultConverter) convertTrigger(triggerDef *TriggerDef) *engine.Trigger {
	trigger := &engine.Trigger{
		Criteria: triggerDef.Criteria,
		InputMap: triggerDef.InputMap,
	}

	switch triggerDef.Type {
	case "event":
		trigger.Type = engine.TriggerEvent
	default:
		trigger.Type = engine.TriggerManual
	}

	return trigger
}

// copyParams creates a shallow copy of parameters map.
func copyParams(params map[string]any) map[string]any {
	if params == nil {
		return nil
	}
	result := make(map[string]any, len(params))
	for k, v := range params {
		result[k] = v
	}
	return result
}
