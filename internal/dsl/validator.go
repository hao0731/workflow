package dsl

import (
	"fmt"
)

// WorkflowValidator validates a parsed workflow definition.
type WorkflowValidator interface {
	Validate(def *WorkflowDefinition) error
}

// CompositeValidator chains multiple validators.
type CompositeValidator struct {
	validators []WorkflowValidator
}

// NewCompositeValidator creates a new CompositeValidator.
func NewCompositeValidator(validators ...WorkflowValidator) *CompositeValidator {
	return &CompositeValidator{validators: validators}
}

// Validate runs all validators in sequence.
func (v *CompositeValidator) Validate(def *WorkflowDefinition) error {
	for _, validator := range v.validators {
		if err := validator.Validate(def); err != nil {
			return err
		}
	}
	return nil
}

// StructureValidator checks required fields and references.
type StructureValidator struct{}

// NewStructureValidator creates a new StructureValidator.
func NewStructureValidator() *StructureValidator {
	return &StructureValidator{}
}

// Validate checks structure requirements.
func (v *StructureValidator) Validate(def *WorkflowDefinition) error {
	// Check workflow ID
	if def.ID == "" {
		return fmt.Errorf("workflow ID is required")
	}

	// Build node ID set and count StartNodes
	nodeIDs := make(map[string]bool)
	startNodeCount := 0

	for i, node := range def.Nodes {
		if node.ID == "" {
			return fmt.Errorf("node ID is required (node index %d)", i)
		}
		if node.Type == "" {
			return fmt.Errorf("node type is required (node %s)", node.ID)
		}
		if nodeIDs[node.ID] {
			return fmt.Errorf("duplicate node ID: %s", node.ID)
		}
		nodeIDs[node.ID] = true

		if node.Type == "StartNode" {
			startNodeCount++
		}
	}

	// Validate StartNode count
	if startNodeCount != 1 {
		return fmt.Errorf("exactly one StartNode required, found %d", startNodeCount)
	}

	// Validate connection references
	for i, conn := range def.Connections {
		if !nodeIDs[conn.From] {
			return fmt.Errorf("connection %d references non-existent node: %s", i, conn.From)
		}
		if !nodeIDs[conn.To] {
			return fmt.Errorf("connection %d references non-existent node: %s", i, conn.To)
		}
	}

	return nil
}

// DAGValidator checks for cycles in the workflow graph.
type DAGValidator struct{}

// NewDAGValidator creates a new DAGValidator.
func NewDAGValidator() *DAGValidator {
	return &DAGValidator{}
}

// Validate checks that the workflow is a valid DAG (no cycles).
func (v *DAGValidator) Validate(def *WorkflowDefinition) error {
	// Build adjacency list
	adj := make(map[string][]string)
	for _, node := range def.Nodes {
		adj[node.ID] = []string{}
	}
	for _, conn := range def.Connections {
		adj[conn.From] = append(adj[conn.From], conn.To)
	}

	// DFS-based cycle detection
	// 0 = unvisited, 1 = visiting (in stack), 2 = visited
	state := make(map[string]int)

	var hasCycle func(node string) bool
	hasCycle = func(node string) bool {
		state[node] = 1 // visiting

		for _, neighbor := range adj[node] {
			if state[neighbor] == 1 {
				// Back edge found - cycle detected
				return true
			}
			if state[neighbor] == 0 {
				if hasCycle(neighbor) {
					return true
				}
			}
		}

		state[node] = 2 // visited
		return false
	}

	for _, node := range def.Nodes {
		if state[node.ID] == 0 {
			if hasCycle(node.ID) {
				return fmt.Errorf("cycle detected in workflow DAG")
			}
		}
	}

	return nil
}
