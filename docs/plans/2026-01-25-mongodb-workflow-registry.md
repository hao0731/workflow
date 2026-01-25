# MongoDB Workflow Registry Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Migrate the `WorkflowRegistry` from in-memory storage to MongoDB while maintaining both options for flexibility.

**Architecture:** Create a `WorkflowStore` interface abstraction, implement `InMemoryWorkflowStore` (refactored from existing code) and `MongoWorkflowStore`, then update `WorkflowRegistry` to use the injected store. Add `WORKFLOW_STORE` config for runtime selection.

**Tech Stack:** Go 1.25, MongoDB driver (`go.mongodb.org/mongo-driver`), mtest for testing

---

## Task 1: Create WorkflowStore Interface

**Files:**
- Create: `internal/dsl/store.go`

**Step 1: Create the interface file**

```go
package dsl

import (
	"context"
	"errors"

	"github.com/cheriehsieh/orchestration/internal/engine"
)

// ErrWorkflowNotFound is returned when a workflow is not found.
var ErrWorkflowNotFound = errors.New("workflow not found")

// WorkflowStore provides persistence for workflow definitions.
type WorkflowStore interface {
	// Register adds or updates a workflow with its source.
	Register(ctx context.Context, wf *engine.Workflow, source []byte) error

	// GetByID retrieves a workflow by its ID.
	GetByID(ctx context.Context, id string) (*engine.Workflow, error)

	// GetSource retrieves the original YAML source for a workflow.
	GetSource(ctx context.Context, id string) ([]byte, error)

	// List returns all workflow IDs.
	List(ctx context.Context) ([]string, error)

	// Delete removes a workflow by its ID.
	Delete(ctx context.Context, id string) error
}
```

**Step 2: Verify file compiles**

Run: `go build ./internal/dsl/...`
Expected: No errors

**Step 3: Commit**

```bash
git add internal/dsl/store.go
git commit -m "feat(dsl): add WorkflowStore interface"
```

---

## Task 2: Implement InMemoryWorkflowStore

**Files:**
- Create: `internal/dsl/memory_store.go`
- Create: `internal/dsl/memory_store_test.go`

**Step 1: Write failing test for Register and GetByID**

```go
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
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/dsl/... -run TestInMemoryWorkflowStore -v`
Expected: FAIL (NewInMemoryWorkflowStore not defined)

**Step 3: Implement InMemoryWorkflowStore**

```go
package dsl

import (
	"context"
	"sync"

	"github.com/cheriehsieh/orchestration/internal/engine"
)

// workflowEntry stores a workflow and its original source.
type workflowEntry struct {
	workflow *engine.Workflow
	source   []byte
}

// InMemoryWorkflowStore provides in-memory storage for workflows.
type InMemoryWorkflowStore struct {
	mu        sync.RWMutex
	workflows map[string]*workflowEntry
}

// NewInMemoryWorkflowStore creates a new in-memory workflow store.
func NewInMemoryWorkflowStore() *InMemoryWorkflowStore {
	return &InMemoryWorkflowStore{
		workflows: make(map[string]*workflowEntry),
	}
}

func (s *InMemoryWorkflowStore) Register(ctx context.Context, wf *engine.Workflow, source []byte) error {
	if wf == nil {
		return errors.New("workflow is nil")
	}
	if wf.ID == "" {
		return errors.New("workflow ID is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.workflows[wf.ID] = &workflowEntry{workflow: wf, source: source}
	return nil
}

func (s *InMemoryWorkflowStore) GetByID(ctx context.Context, id string) (*engine.Workflow, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entry, ok := s.workflows[id]
	if !ok {
		return nil, ErrWorkflowNotFound
	}
	return entry.workflow, nil
}

func (s *InMemoryWorkflowStore) GetSource(ctx context.Context, id string) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entry, ok := s.workflows[id]
	if !ok {
		return nil, ErrWorkflowNotFound
	}
	return entry.source, nil
}

func (s *InMemoryWorkflowStore) List(ctx context.Context) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ids := make([]string, 0, len(s.workflows))
	for id := range s.workflows {
		ids = append(ids, id)
	}
	return ids, nil
}

func (s *InMemoryWorkflowStore) Delete(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.workflows[id]; !ok {
		return ErrWorkflowNotFound
	}
	delete(s.workflows, id)
	return nil
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/dsl/... -run TestInMemoryWorkflowStore -v`
Expected: PASS (all 6 tests)

**Step 5: Commit**

```bash
git add internal/dsl/memory_store.go internal/dsl/memory_store_test.go
git commit -m "feat(dsl): implement InMemoryWorkflowStore"
```

---

## Task 3: Implement MongoWorkflowStore

**Files:**
- Create: `internal/dsl/mongo_store.go`
- Create: `internal/dsl/mongo_store_test.go`

**Step 1: Write failing test for Register**

```go
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
		require.NoError(t, err)
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
		require.NoError(t, err)
		assert.Equal(t, "test-wf", wf.ID)
	})

	mt.Run("not found", func(mt *mtest.T) {
		store := &MongoWorkflowStore{collection: mt.Coll}

		mt.AddMockResponses(mtest.CreateCursorResponse(0, "db.workflows", mtest.FirstBatch))

		_, err := store.GetByID(context.Background(), "nonexistent")
		require.ErrorIs(t, err, ErrWorkflowNotFound)
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
		require.NoError(t, err)
		assert.Equal(t, expectedSource, source)
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
		require.NoError(t, err)
		assert.Len(t, ids, 2)
	})
}

func TestMongoWorkflowStore_Delete(t *testing.T) {
	mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))

	mt.Run("success", func(mt *mtest.T) {
		store := &MongoWorkflowStore{collection: mt.Coll}

		mt.AddMockResponses(bson.D{{Key: "ok", Value: 1}, {Key: "n", Value: 1}})

		err := store.Delete(context.Background(), "test-wf")
		require.NoError(t, err)
	})

	mt.Run("not found", func(mt *mtest.T) {
		store := &MongoWorkflowStore{collection: mt.Coll}

		mt.AddMockResponses(bson.D{{Key: "ok", Value: 1}, {Key: "n", Value: 0}})

		err := store.Delete(context.Background(), "nonexistent")
		require.ErrorIs(t, err, ErrWorkflowNotFound)
	})
}

// Compile-time interface check
var _ WorkflowStore = (*MongoWorkflowStore)(nil)
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/dsl/... -run TestMongoWorkflowStore -v`
Expected: FAIL (MongoWorkflowStore not defined)

**Step 3: Implement MongoWorkflowStore**

```go
package dsl

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/cheriehsieh/orchestration/internal/engine"
)

// WorkflowDocument represents the MongoDB document structure.
type WorkflowDocument struct {
	ID          string              `bson:"_id"`
	Source      []byte              `bson:"source"`
	Nodes       []engine.Node       `bson:"nodes"`
	Connections []engine.Connection `bson:"connections"`
	CreatedAt   time.Time           `bson:"created_at"`
	UpdatedAt   time.Time           `bson:"updated_at"`
}

// MongoWorkflowStore provides MongoDB storage for workflows.
type MongoWorkflowStore struct {
	collection *mongo.Collection
}

// NewMongoWorkflowStore creates a new MongoDB workflow store.
func NewMongoWorkflowStore(db *mongo.Database) *MongoWorkflowStore {
	coll := db.Collection("workflows")

	// Create unique index on _id (basic indexing strategy)
	_, _ = coll.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys:    bson.D{{Key: "_id", Value: 1}},
		Options: options.Index().SetUnique(true),
	})

	return &MongoWorkflowStore{collection: coll}
}

func (s *MongoWorkflowStore) Register(ctx context.Context, wf *engine.Workflow, source []byte) error {
	if wf == nil {
		return errors.New("workflow is nil")
	}
	if wf.ID == "" {
		return errors.New("workflow ID is required")
	}

	now := time.Now()
	doc := WorkflowDocument{
		ID:          wf.ID,
		Source:      source,
		Nodes:       wf.Nodes,
		Connections: wf.Connections,
		UpdatedAt:   now,
	}

	opts := options.Update().SetUpsert(true)
	_, err := s.collection.UpdateOne(ctx,
		bson.M{"_id": wf.ID},
		bson.M{
			"$set":         doc,
			"$setOnInsert": bson.M{"created_at": now},
		},
		opts,
	)
	return err
}

func (s *MongoWorkflowStore) GetByID(ctx context.Context, id string) (*engine.Workflow, error) {
	var doc WorkflowDocument

	err := s.collection.FindOne(ctx, bson.M{"_id": id}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrWorkflowNotFound
	}
	if err != nil {
		return nil, err
	}

	return &engine.Workflow{
		ID:          doc.ID,
		Nodes:       doc.Nodes,
		Connections: doc.Connections,
	}, nil
}

func (s *MongoWorkflowStore) GetSource(ctx context.Context, id string) ([]byte, error) {
	var doc struct {
		Source []byte `bson:"source"`
	}

	opts := options.FindOne().SetProjection(bson.M{"source": 1})
	err := s.collection.FindOne(ctx, bson.M{"_id": id}, opts).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrWorkflowNotFound
	}
	if err != nil {
		return nil, err
	}

	return doc.Source, nil
}

func (s *MongoWorkflowStore) List(ctx context.Context) ([]string, error) {
	opts := options.Find().SetProjection(bson.M{"_id": 1})
	cursor, err := s.collection.Find(ctx, bson.M{}, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var results []struct {
		ID string `bson:"_id"`
	}
	if err := cursor.All(ctx, &results); err != nil {
		return nil, err
	}

	ids := make([]string, len(results))
	for i, r := range results {
		ids[i] = r.ID
	}
	return ids, nil
}

func (s *MongoWorkflowStore) Delete(ctx context.Context, id string) error {
	result, err := s.collection.DeleteOne(ctx, bson.M{"_id": id})
	if err != nil {
		return err
	}
	if result.DeletedCount == 0 {
		return ErrWorkflowNotFound
	}
	return nil
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/dsl/... -run TestMongoWorkflowStore -v`
Expected: PASS (all tests)

**Step 5: Commit**

```bash
git add internal/dsl/mongo_store.go internal/dsl/mongo_store_test.go
git commit -m "feat(dsl): implement MongoWorkflowStore with mtest"
```

---

## Task 4: Add Store Factory and Configuration

**Files:**
- Create: `internal/dsl/store_factory.go`
- Modify: `internal/config/config.go:26-47`

**Step 1: Create store factory**

```go
package dsl

import (
	"errors"
	"fmt"

	"go.mongodb.org/mongo-driver/mongo"
)

// NewWorkflowStoreFromConfig creates a WorkflowStore based on the store type.
func NewWorkflowStoreFromConfig(storeType string, db *mongo.Database) (WorkflowStore, error) {
	switch storeType {
	case "mongo":
		if db == nil {
			return nil, errors.New("MongoDB database required for mongo store")
		}
		return NewMongoWorkflowStore(db), nil
	case "memory", "":
		return NewInMemoryWorkflowStore(), nil
	default:
		return nil, fmt.Errorf("unknown workflow store type: %s", storeType)
	}
}
```

**Step 2: Verify factory compiles**

Run: `go build ./internal/dsl/...`
Expected: No errors

**Step 3: Add WorkflowStore to config**

In `internal/config/config.go`, add field inside the Config struct (after line 25):

```go
	// Workflow
	WorkflowStore string
```

And in the Load function (after line 46):

```go
		// Workflow
		WorkflowStore: getEnv("WORKFLOW_STORE", "memory"),
```

**Step 4: Commit**

```bash
git add internal/dsl/store_factory.go internal/config/config.go
git commit -m "feat(dsl): add store factory and WORKFLOW_STORE config"
```

---

## Task 5: Refactor WorkflowRegistry to Use WorkflowStore

**Files:**
- Modify: `internal/dsl/registry.go`
- Modify: `internal/dsl/registry_test.go`

**Step 1: Update WorkflowRegistry struct**

Replace the internal `workflows` map with `store WorkflowStore` field. Update `NewWorkflowRegistry` to create a default in-memory store. Add `WithStore` option.

Key changes to `registry.go`:
1. Replace `workflows map[string]*engine.Workflow` with `store WorkflowStore`
2. Remove the `mu sync.RWMutex` (no longer needed, store handles concurrency)
3. Update all methods to delegate to `store`
4. Add `WithStore` option function

**Step 2: Run existing tests to ensure they still pass**

Run: `go test ./internal/dsl/... -v`
Expected: PASS (all existing registry tests should pass with the refactored code)

**Step 3: Commit**

```bash
git add internal/dsl/registry.go internal/dsl/registry_test.go
git commit -m "refactor(dsl): WorkflowRegistry delegates to WorkflowStore"
```

---

## Task 6: Update API Bootstrap (cmd/workflow-api/main.go)

**Files:**
- Modify: `cmd/workflow-api/main.go:52-53`

**Step 1: Update registry initialization**

Replace:
```go
registry := dsl.NewWorkflowRegistry()
```

With:
```go
// Initialize workflow store based on config
store, err := dsl.NewWorkflowStoreFromConfig(cfg.WorkflowStore, db)
if err != nil {
    logger.Error("failed to create workflow store", slog.Any("error", err))
    os.Exit(1)
}
registry := dsl.NewWorkflowRegistry(dsl.WithStore(store))
logger.Info("workflow store initialized", slog.String("type", cfg.WorkflowStore))
```

**Step 2: Verify build succeeds**

Run: `go build ./cmd/workflow-api/...`
Expected: No errors

**Step 3: Commit**

```bash
git add cmd/workflow-api/main.go
git commit -m "feat(api): use configurable workflow store"
```

---

## Task 7: Final Verification

**Step 1: Run all tests**

Run: `go test ./... -v`
Expected: PASS (all tests including new ones)

**Step 2: Run linter**

Run: `golangci-lint run ./...`
Expected: No errors

**Step 3: Test with MongoDB locally (manual)**

```bash
# Set env to use mongo store
export WORKFLOW_STORE=mongo
export MONGO_URI=mongodb://localhost:27017

# Start the API
go run ./cmd/workflow-api

# In another terminal, create a workflow
curl -X POST http://localhost:8083/api/workflows \
  -H "Content-Type: application/x-yaml" \
  -d 'id: manual-test
nodes:
  - id: start
    type: StartNode
connections: []'

# Verify it was stored
curl http://localhost:8083/api/workflows/manual-test
```

**Step 4: Final commit**

```bash
git add .
git commit -m "chore: final verification complete"
```

---

## Verification Summary

| Test Type | Command | Expected |
|-----------|---------|----------|
| Unit tests (all) | `go test ./internal/dsl/... -v` | All PASS |
| Build | `go build ./...` | No errors |
| Lint | `golangci-lint run ./...` | No errors |
| Manual (memory) | Start API with `WORKFLOW_STORE=memory`, create workflow via curl | Works |
| Manual (mongo) | Start API with `WORKFLOW_STORE=mongo`, create workflow via curl | Works |
