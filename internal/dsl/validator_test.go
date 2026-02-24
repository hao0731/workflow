package dsl

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test: Valid workflow passes validation (Happy Path)
func TestStructureValidator_Validate_PassesValidWorkflow(t *testing.T) {
	def := &WorkflowDefinition{
		ID: "valid-workflow",
		Nodes: []NodeDefinition{
			{ID: "start", Type: "StartNode"},
			{ID: "action", Type: "http-request@v1"},
		},
		Connections: []ConnectionDef{
			{From: "start", To: "action"},
		},
	}

	validator := NewStructureValidator()
	err := validator.Validate(def)

	assert.NoError(t, err)
}

// Test: Missing workflow ID returns error (Zero case)
func TestStructureValidator_Validate_ErrorOnMissingID(t *testing.T) {
	def := &WorkflowDefinition{
		ID: "",
		Nodes: []NodeDefinition{
			{ID: "start", Type: "StartNode"},
		},
	}

	validator := NewStructureValidator()
	err := validator.Validate(def)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "workflow ID is required")
}

// Test: Missing node ID returns error
func TestStructureValidator_Validate_ErrorOnMissingNodeID(t *testing.T) {
	def := &WorkflowDefinition{
		ID: "test",
		Nodes: []NodeDefinition{
			{ID: "", Type: "StartNode"},
		},
	}

	validator := NewStructureValidator()
	err := validator.Validate(def)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "node ID is required")
}

// Test: Missing node type returns error
func TestStructureValidator_Validate_ErrorOnMissingNodeType(t *testing.T) {
	def := &WorkflowDefinition{
		ID: "test",
		Nodes: []NodeDefinition{
			{ID: "start", Type: ""},
		},
	}

	validator := NewStructureValidator()
	err := validator.Validate(def)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "node type is required")
}

// Test: Duplicate node IDs return error
func TestStructureValidator_Validate_ErrorOnDuplicateNodeID(t *testing.T) {
	def := &WorkflowDefinition{
		ID: "test",
		Nodes: []NodeDefinition{
			{ID: "start", Type: "StartNode"},
			{ID: "start", Type: "ActionNode"}, // Duplicate
		},
	}

	validator := NewStructureValidator()
	err := validator.Validate(def)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "duplicate node ID")
}

// Test: Connection referencing non-existent node returns error
func TestStructureValidator_Validate_ErrorOnInvalidConnectionFrom(t *testing.T) {
	def := &WorkflowDefinition{
		ID: "test",
		Nodes: []NodeDefinition{
			{ID: "start", Type: "StartNode"},
		},
		Connections: []ConnectionDef{
			{From: "nonexistent", To: "start"},
		},
	}

	validator := NewStructureValidator()
	err := validator.Validate(def)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "references non-existent node")
}

// Test: Connection referencing non-existent target returns error
func TestStructureValidator_Validate_ErrorOnInvalidConnectionTo(t *testing.T) {
	def := &WorkflowDefinition{
		ID: "test",
		Nodes: []NodeDefinition{
			{ID: "start", Type: "StartNode"},
		},
		Connections: []ConnectionDef{
			{From: "start", To: "nonexistent"},
		},
	}

	validator := NewStructureValidator()
	err := validator.Validate(def)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "references non-existent node")
}

// Test: Multiple StartNodes return error
func TestStructureValidator_Validate_ErrorOnMultipleStartNodes(t *testing.T) {
	def := &WorkflowDefinition{
		ID: "test",
		Nodes: []NodeDefinition{
			{ID: "start1", Type: "StartNode"},
			{ID: "start2", Type: "StartNode"},
		},
	}

	validator := NewStructureValidator()
	err := validator.Validate(def)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "exactly one StartNode required")
}

// Test: No StartNode returns error
func TestStructureValidator_Validate_ErrorOnNoStartNode(t *testing.T) {
	def := &WorkflowDefinition{
		ID: "test",
		Nodes: []NodeDefinition{
			{ID: "action", Type: "http-request@v1"},
		},
	}

	validator := NewStructureValidator()
	err := validator.Validate(def)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "exactly one StartNode required")
}

// Test: CompositeValidator chains validators
func TestCompositeValidator_Validate_ChainsValidators(t *testing.T) {
	def := &WorkflowDefinition{
		ID: "", // Will fail StructureValidator
		Nodes: []NodeDefinition{
			{ID: "start", Type: "StartNode"},
		},
	}

	composite := NewCompositeValidator(NewStructureValidator())
	err := composite.Validate(def)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "workflow ID is required")
}

// Test: DAGValidator detects cycles
func TestDAGValidator_Validate_ErrorOnCycle(t *testing.T) {
	def := &WorkflowDefinition{
		ID: "cyclic-workflow",
		Nodes: []NodeDefinition{
			{ID: "start", Type: "StartNode"},
			{ID: "a", Type: "action@v1"},
			{ID: "b", Type: "action@v1"},
		},
		Connections: []ConnectionDef{
			{From: "start", To: "a"},
			{From: "a", To: "b"},
			{From: "b", To: "a"}, // Cycle: b -> a
		},
	}

	validator := NewDAGValidator()
	err := validator.Validate(def)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "cycle detected")
}

// Test: DAGValidator passes for valid DAG
func TestDAGValidator_Validate_PassesValidDAG(t *testing.T) {
	def := &WorkflowDefinition{
		ID: "valid-dag",
		Nodes: []NodeDefinition{
			{ID: "start", Type: "StartNode"},
			{ID: "a", Type: "action@v1"},
			{ID: "b", Type: "action@v1"},
			{ID: "c", Type: "action@v1"},
		},
		Connections: []ConnectionDef{
			{From: "start", To: "a"},
			{From: "start", To: "b"},
			{From: "a", To: "c"},
			{From: "b", To: "c"},
		},
	}

	validator := NewDAGValidator()
	err := validator.Validate(def)

	assert.NoError(t, err)
}
