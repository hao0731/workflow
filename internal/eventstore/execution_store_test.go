package eventstore

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func setupTestMongo(t *testing.T) (*mongo.Database, func()) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	client, err := mongo.Connect(ctx, options.Client().ApplyURI("mongodb://localhost:27017"))
	if err != nil {
		t.Skipf("MongoDB not available: %v", err)
	}
	if err := client.Ping(ctx, nil); err != nil {
		t.Skipf("MongoDB not available: %v", err)
	}

	db := client.Database("test_orchestration_" + t.Name())
	return db, func() {
		_ = db.Drop(context.Background())
		_ = client.Disconnect(context.Background())
	}
}

func TestMongoExecutionStore_Create(t *testing.T) {
	db, cleanup := setupTestMongo(t)
	defer cleanup()

	store := NewMongoExecutionStore(db, "executions")
	ctx := context.Background()

	exec := &Execution{
		ID:         "exec-123",
		WorkflowID: "wf-456",
		Status:     StatusRunning,
		StartedAt:  time.Now(),
	}

	err := store.Create(ctx, exec)
	require.NoError(t, err)

	// Verify can retrieve
	found, err := store.GetByID(ctx, "exec-123")
	require.NoError(t, err)
	assert.Equal(t, "exec-123", found.ID)
	assert.Equal(t, "wf-456", found.WorkflowID)
	assert.Equal(t, StatusRunning, found.Status)
}

func TestMongoExecutionStore_GetChildren(t *testing.T) {
	db, cleanup := setupTestMongo(t)
	defer cleanup()

	store := NewMongoExecutionStore(db, "executions")
	ctx := context.Background()

	// Create parent
	parent := &Execution{ID: "parent-1", WorkflowID: "wf-parent", Status: StatusRunning, StartedAt: time.Now()}
	require.NoError(t, store.Create(ctx, parent))

	// Create children with parent reference
	child1 := &Execution{ID: "child-1", WorkflowID: "wf-child", Status: StatusRunning, StartedAt: time.Now(), ParentExecutionID: "parent-1"}
	child2 := &Execution{ID: "child-2", WorkflowID: "wf-child", Status: StatusCompleted, StartedAt: time.Now(), ParentExecutionID: "parent-1"}
	require.NoError(t, store.Create(ctx, child1))
	require.NoError(t, store.Create(ctx, child2))

	// Query children
	children, err := store.GetChildren(ctx, "parent-1")
	require.NoError(t, err)
	assert.Len(t, children, 2)
}

func TestMongoExecutionStore_AddChildExecution(t *testing.T) {
	db, cleanup := setupTestMongo(t)
	defer cleanup()

	store := NewMongoExecutionStore(db, "executions")
	ctx := context.Background()

	// Create parent
	parent := &Execution{ID: "parent-add", WorkflowID: "wf-parent", Status: StatusRunning, StartedAt: time.Now()}
	require.NoError(t, store.Create(ctx, parent))

	// Add child references
	require.NoError(t, store.AddChildExecution(ctx, "parent-add", "child-a"))
	require.NoError(t, store.AddChildExecution(ctx, "parent-add", "child-b"))
	// Duplicate should be ignored (addToSet)
	require.NoError(t, store.AddChildExecution(ctx, "parent-add", "child-a"))

	// Verify
	found, err := store.GetByID(ctx, "parent-add")
	require.NoError(t, err)
	assert.Len(t, found.ChildExecutionIDs, 2)
	assert.Contains(t, found.ChildExecutionIDs, "child-a")
	assert.Contains(t, found.ChildExecutionIDs, "child-b")
}
