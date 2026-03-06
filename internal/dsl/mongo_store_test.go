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

func TestMongoWorkflowStore_RegisterRecord_PersistsMetadataFields(t *testing.T) {
	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))

	mt.Run("success", func(mt *mtest.T) {
		store := &MongoWorkflowStore{collection: mt.Coll}
		record := &WorkflowRecord{
			ID:          "test-wf",
			Name:        "Test Workflow",
			Description: "Persists metadata",
			Version:     "1.0.0",
			Source:      []byte("id: test-wf"),
			Events: []EventDefinition{
				{Name: "employee.created", Domain: "hr", Description: "employee created"},
			},
			Workflow: &engine.Workflow{
				ID: "test-wf",
				Nodes: []engine.Node{
					{ID: "start", Type: engine.StartNode, FullType: string(engine.StartNode)},
				},
			},
		}

		mt.AddMockResponses(mtest.CreateSuccessResponse())

		err := store.RegisterRecord(context.Background(), record)
		require.NoError(mt, err)

		started := mt.GetStartedEvent()
		require.NotNil(mt, started)

		updates, err := started.Command.LookupErr("updates")
		require.NoError(mt, err)

		values, err := updates.Array().Values()
		require.NoError(mt, err)
		require.Len(mt, values, 1)

		updateDoc := values[0].Document()
		setDoc := updateDoc.Lookup("u").Document().Lookup("$set").Document()

		assert.Equal(mt, "Test Workflow", setDoc.Lookup("name").StringValue())
		assert.Equal(mt, "Persists metadata", setDoc.Lookup("description").StringValue())
		assert.Equal(mt, "1.0.0", setDoc.Lookup("version").StringValue())
		_, sourceData := setDoc.Lookup("source").Binary()
		assert.Equal(mt, []byte("id: test-wf"), sourceData)

		events, err := setDoc.Lookup("events").Array().Values()
		require.NoError(mt, err)
		require.Len(mt, events, 1)
		assert.Equal(mt, "employee.created", events[0].Document().Lookup("name").StringValue())
		assert.Equal(mt, "hr", events[0].Document().Lookup("domain").StringValue())
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

func TestMongoWorkflowStore_GetRecord(t *testing.T) {
	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))

	mt.Run("found", func(mt *mtest.T) {
		store := &MongoWorkflowStore{collection: mt.Coll}

		mt.AddMockResponses(mtest.CreateCursorResponse(1, "db.workflows", mtest.FirstBatch, bson.D{
			{Key: "_id", Value: "test-wf"},
			{Key: "name", Value: "Test Workflow"},
			{Key: "description", Value: "Persists metadata"},
			{Key: "version", Value: "1.0.0"},
			{Key: "source", Value: []byte("id: test-wf")},
			{Key: "events", Value: bson.A{
				bson.D{
					{Key: "name", Value: "employee.created"},
					{Key: "domain", Value: "hr"},
					{Key: "description", Value: "employee created"},
				},
			}},
			{Key: "nodes", Value: bson.A{
				bson.D{
					{Key: "id", Value: "start"},
					{Key: "type", Value: "StartNode"},
					{Key: "full_type", Value: "StartNode"},
				},
			}},
			{Key: "connections", Value: bson.A{}},
		}))

		record, err := store.GetRecord(context.Background(), "test-wf")
		require.NoError(mt, err)
		require.NotNil(mt, record)
		require.NotNil(mt, record.Workflow)

		assert.Equal(mt, "test-wf", record.ID)
		assert.Equal(mt, "Test Workflow", record.Name)
		assert.Equal(mt, "Persists metadata", record.Description)
		assert.Equal(mt, "1.0.0", record.Version)
		assert.Equal(mt, []byte("id: test-wf"), record.Source)
		assert.Len(mt, record.Events, 1)
		assert.Equal(mt, "employee.created", record.Events[0].Name)
		assert.Equal(mt, "test-wf", record.Workflow.ID)
		assert.Equal(mt, string(engine.StartNode), record.Workflow.GetNode("start").FullType)
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
