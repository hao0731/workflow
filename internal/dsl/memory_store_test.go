package dsl

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cheriehsieh/orchestration/internal/engine"
)

func TestInMemoryWorkflowStore_Register_And_GetByID(t *testing.T) {
	store := NewInMemoryWorkflowStore()
	wf := &engine.Workflow{ID: "test-wf", Nodes: []engine.Node{{ID: "start", Type: engine.StartNode}}}
	source := []byte("id: test-wf")

	err := store.Register(context.Background(), wf, source)
	require.NoError(t, err)

	got, err := store.GetByID(context.Background(), "test-wf")
	require.NoError(t, err)
	assert.Equal(t, "test-wf", got.ID)
}

func TestInMemoryWorkflowStore_GetByID_NotFound(t *testing.T) {
	store := NewInMemoryWorkflowStore()

	_, err := store.GetByID(context.Background(), "nonexistent")

	require.ErrorIs(t, err, ErrWorkflowNotFound)
}

func TestInMemoryWorkflowStore_GetSource(t *testing.T) {
	store := NewInMemoryWorkflowStore()
	wf := &engine.Workflow{ID: "test-wf"}
	source := []byte("id: test-wf\nname: Test")

	_ = store.Register(context.Background(), wf, source)

	got, err := store.GetSource(context.Background(), "test-wf")
	require.NoError(t, err)
	assert.Equal(t, source, got)
}

func TestInMemoryWorkflowStore_List(t *testing.T) {
	store := NewInMemoryWorkflowStore()
	_ = store.Register(context.Background(), &engine.Workflow{ID: "alpha"}, nil)
	_ = store.Register(context.Background(), &engine.Workflow{ID: "beta"}, nil)

	ids, err := store.List(context.Background())

	require.NoError(t, err)
	assert.Len(t, ids, 2)
	assert.Contains(t, ids, "alpha")
	assert.Contains(t, ids, "beta")
}

func TestInMemoryWorkflowStore_Delete(t *testing.T) {
	store := NewInMemoryWorkflowStore()
	_ = store.Register(context.Background(), &engine.Workflow{ID: "test-wf"}, nil)

	err := store.Delete(context.Background(), "test-wf")
	require.NoError(t, err)

	_, err = store.GetByID(context.Background(), "test-wf")
	require.ErrorIs(t, err, ErrWorkflowNotFound)
}

func TestInMemoryWorkflowStore_Delete_NotFound(t *testing.T) {
	store := NewInMemoryWorkflowStore()

	err := store.Delete(context.Background(), "nonexistent")

	require.ErrorIs(t, err, ErrWorkflowNotFound)
}

// Compile-time interface check
var _ WorkflowStore = (*InMemoryWorkflowStore)(nil)
