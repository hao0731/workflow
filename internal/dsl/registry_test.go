package dsl

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test: Registry loads and retrieves workflow
func TestWorkflowRegistry_LoadFile_And_GetByID(t *testing.T) {
	// Setup: Create temp workflow file
	tempDir := t.TempDir()
	workflowPath := filepath.Join(tempDir, "test-workflow.yaml")
	content := []byte(`
id: test-workflow
name: Test Workflow
nodes:
  - id: start
    type: StartNode
  - id: action
    type: http-request@v1
connections:
  - from: start
    to: action
`)
	require.NoError(t, os.WriteFile(workflowPath, content, 0644))

	// Act
	registry := NewWorkflowRegistry()
	err := registry.LoadFile(context.Background(), workflowPath)
	require.NoError(t, err)

	wf, err := registry.GetByID(context.Background(), "test-workflow")

	// Assert
	require.NoError(t, err)
	assert.Equal(t, "test-workflow", wf.ID)
	assert.Len(t, wf.Nodes, 2)
}

// Test: GetByID returns error for unknown workflow
func TestWorkflowRegistry_GetByID_ReturnsErrorForUnknown(t *testing.T) {
	registry := NewWorkflowRegistry()
	_, err := registry.GetByID(context.Background(), "nonexistent")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "workflow not found")
}

// Test: LoadDirectory loads all YAML files
func TestWorkflowRegistry_LoadDirectory(t *testing.T) {
	// Setup: Create temp directory with multiple workflow files
	tempDir := t.TempDir()

	wf1 := filepath.Join(tempDir, "workflow1.yaml")
	require.NoError(t, os.WriteFile(wf1, []byte(`
id: workflow-1
nodes:
  - id: start
    type: StartNode
connections: []
`), 0644))

	wf2 := filepath.Join(tempDir, "workflow2.yaml")
	require.NoError(t, os.WriteFile(wf2, []byte(`
id: workflow-2
nodes:
  - id: start
    type: StartNode
connections: []
`), 0644))

	// Non-YAML file should be ignored
	require.NoError(t, os.WriteFile(filepath.Join(tempDir, "readme.txt"), []byte("ignore me"), 0644))

	// Act
	registry := NewWorkflowRegistry()
	err := registry.LoadDirectory(context.Background(), tempDir)
	require.NoError(t, err)

	// Assert
	wf1Result, err := registry.GetByID(context.Background(), "workflow-1")
	require.NoError(t, err)
	assert.Equal(t, "workflow-1", wf1Result.ID)

	wf2Result, err := registry.GetByID(context.Background(), "workflow-2")
	require.NoError(t, err)
	assert.Equal(t, "workflow-2", wf2Result.ID)
}

// Test: LoadFile returns error for invalid workflow
func TestWorkflowRegistry_LoadFile_ReturnsErrorForInvalidWorkflow(t *testing.T) {
	tempDir := t.TempDir()
	workflowPath := filepath.Join(tempDir, "invalid.yaml")
	// Missing StartNode
	content := []byte(`
id: invalid-workflow
nodes:
  - id: action
    type: http-request@v1
connections: []
`)
	require.NoError(t, os.WriteFile(workflowPath, content, 0644))

	registry := NewWorkflowRegistry()
	err := registry.LoadFile(context.Background(), workflowPath)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "validation failed")
}

// Test: ListWorkflows returns all loaded workflow IDs
func TestWorkflowRegistry_ListWorkflows(t *testing.T) {
	tempDir := t.TempDir()

	wf1 := filepath.Join(tempDir, "wf1.yaml")
	require.NoError(t, os.WriteFile(wf1, []byte(`
id: alpha
nodes:
  - id: start
    type: StartNode
connections: []
`), 0644))

	wf2 := filepath.Join(tempDir, "wf2.yaml")
	require.NoError(t, os.WriteFile(wf2, []byte(`
id: beta
nodes:
  - id: start
    type: StartNode
connections: []
`), 0644))

	registry := NewWorkflowRegistry()
	require.NoError(t, registry.LoadDirectory(context.Background(), tempDir))

	ids := registry.ListWorkflows()

	assert.Len(t, ids, 2)
	assert.Contains(t, ids, "alpha")
	assert.Contains(t, ids, "beta")
}
