package dsl

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo/integration/mtest"

	"github.com/cheriehsieh/orchestration/internal/engine"
)

func TestMongoWorkflowStore_Register(t *testing.T) {
	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))

	mt.Run("success", func(mt *mtest.T) {
		store := &MongoWorkflowStore{collection: mt.Coll}
		wf := &engine.Workflow{ID: "test-wf", Nodes: []engine.Node{{ID: "start", Type: engine.StartNode}}}
		source := []byte("id: test-wf")

		mt.AddMockResponses(mtest.CreateSuccessResponse())

		err := store.Register(context.Background(), wf, source)
		require.NoError(mt, err)
	})
}

func TestMongoWorkflowStore_GetByID(t *testing.T) {
	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))

	mt.Run("found", func(mt *mtest.T) {
		store := &MongoWorkflowStore{collection: mt.Coll}

		mt.AddMockResponses(mtest.CreateCursorResponse(1, "db.workflows", mtest.FirstBatch, bson.D{
			{Key: "_id", Value: "test-wf"},
			{Key: "nodes", Value: bson.A{bson.D{{Key: "id", Value: "start"}, {Key: "type", Value: "StartNode"}}}},
			{Key: "connections", Value: bson.A{}},
		}))

		wf, err := store.GetByID(context.Background(), "test-wf")
		require.NoError(mt, err)
		assert.Equal(mt, "test-wf", wf.ID)
	})

	mt.Run("not found", func(mt *mtest.T) {
		store := &MongoWorkflowStore{collection: mt.Coll}

		mt.AddMockResponses(mtest.CreateCursorResponse(0, "db.workflows", mtest.FirstBatch))

		_, err := store.GetByID(context.Background(), "nonexistent")
		require.ErrorIs(mt, err, ErrWorkflowNotFound)
	})
}

func TestMongoWorkflowStore_GetSource(t *testing.T) {
	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))

	mt.Run("found", func(mt *mtest.T) {
		store := &MongoWorkflowStore{collection: mt.Coll}
		expectedSource := []byte("id: test-wf")

		mt.AddMockResponses(mtest.CreateCursorResponse(1, "db.workflows", mtest.FirstBatch, bson.D{
			{Key: "_id", Value: "test-wf"},
			{Key: "source", Value: expectedSource},
		}))

		source, err := store.GetSource(context.Background(), "test-wf")
		require.NoError(mt, err)
		assert.Equal(mt, expectedSource, source)
	})
}

func TestMongoWorkflowStore_List(t *testing.T) {
	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))

	mt.Run("returns all IDs", func(mt *mtest.T) {
		store := &MongoWorkflowStore{collection: mt.Coll}

		first := mtest.CreateCursorResponse(1, "db.workflows", mtest.FirstBatch,
			bson.D{{Key: "_id", Value: "alpha"}},
		)
		getMore := mtest.CreateCursorResponse(1, "db.workflows", mtest.NextBatch,
			bson.D{{Key: "_id", Value: "beta"}},
		)
		killCursors := mtest.CreateCursorResponse(0, "db.workflows", mtest.NextBatch)
		mt.AddMockResponses(first, getMore, killCursors)

		ids, err := store.List(context.Background())
		require.NoError(mt, err)
		assert.Len(mt, ids, 2)
	})
}

func TestMongoWorkflowStore_Delete(t *testing.T) {
	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))

	mt.Run("success", func(mt *mtest.T) {
		store := &MongoWorkflowStore{collection: mt.Coll}

		mt.AddMockResponses(bson.D{{Key: "ok", Value: 1}, {Key: "n", Value: 1}})

		err := store.Delete(context.Background(), "test-wf")
		require.NoError(mt, err)
	})

	mt.Run("not found", func(mt *mtest.T) {
		store := &MongoWorkflowStore{collection: mt.Coll}

		mt.AddMockResponses(bson.D{{Key: "ok", Value: 1}, {Key: "n", Value: 0}})

		err := store.Delete(context.Background(), "nonexistent")
		require.ErrorIs(mt, err, ErrWorkflowNotFound)
	})
}

// Compile-time interface check
var _ WorkflowStore = (*MongoWorkflowStore)(nil)
