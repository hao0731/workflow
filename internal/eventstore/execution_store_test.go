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
	ctx := context.Background()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI("mongodb://localhost:27017"))
	require.NoError(t, err)
	db := client.Database("test_orchestration_" + t.Name())
	return db, func() {
		_ = db.Drop(ctx)
		_ = client.Disconnect(ctx)
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
