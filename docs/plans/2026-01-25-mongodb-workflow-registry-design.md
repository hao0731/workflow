# MongoDB Workflow Registry Design

**Date**: 2026-01-25  
**Status**: Approved  

## Overview

Migrate the `WorkflowRegistry` from in-memory storage to MongoDB while maintaining both options for flexibility. The design follows established patterns in the codebase (`registry/repository.go`, `marketplace/registry.go`).

## Requirements Summary

| Decision | Choice |
|----------|--------|
| In-memory alongside MongoDB? | Both options (InMemory + Mongo) |
| YAML source storage | Store both YAML source AND converted workflow |
| MongoDB indexing | Basic (unique index on `_id` only) |
| API transition | Interface for testability + config for runtime selection |
| Testing approach | Use `mongo.mtest` |

---

## 1. Interface & Data Model

### `WorkflowStore` Interface

A common interface that both in-memory and MongoDB implementations satisfy:

```go
// internal/dsl/store.go
package dsl

import (
    "context"
    "github.com/cheriehsieh/orchestration/internal/engine"
)

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

### MongoDB Document Schema

```go
// WorkflowDocument represents the MongoDB document structure.
type WorkflowDocument struct {
    ID          string              `bson:"_id"`
    Source      []byte              `bson:"source"`       // Original YAML
    Nodes       []engine.Node       `bson:"nodes"`
    Connections []engine.Connection `bson:"connections"`
    CreatedAt   time.Time           `bson:"created_at"`
    UpdatedAt   time.Time           `bson:"updated_at"`
}
```

---

## 2. MongoDB Implementation

### `MongoWorkflowStore`

Following established patterns in `registry/repository.go` and `marketplace/registry.go`:

```go
// internal/dsl/mongo_store.go
package dsl

type MongoWorkflowStore struct {
    collection *mongo.Collection
}

func NewMongoWorkflowStore(db *mongo.Database) *MongoWorkflowStore {
    coll := db.Collection("workflows")
    
    // Create unique index on _id (basic indexing strategy)
    _, _ = coll.Indexes().CreateOne(context.Background(), mongo.IndexModel{
        Keys:    bson.D{{Key: "_id", Value: 1}},
        Options: options.Index().SetUnique(true),
    })
    
    return &MongoWorkflowStore{collection: coll}
}

// Compile-time interface check
var _ WorkflowStore = (*MongoWorkflowStore)(nil)
```

### Method Implementations

| Method | MongoDB Operation | Best Practice |
|--------|------------------|---------------|
| `Register` | `UpdateOne` with upsert | Atomic upsert using `$set` |
| `GetByID` | `FindOne` | Project only needed fields |
| `GetSource` | `FindOne` | Project only `source` field (efficient) |
| `List` | `Find` | Project only `_id` field |
| `Delete` | `DeleteOne` | Return `ErrWorkflowNotFound` if not found |

### Error Handling

```go
var (
    ErrWorkflowNotFound = errors.New("workflow not found")
)
```

---

## 3. In-Memory Store & Registry Refactor

### `InMemoryWorkflowStore`

Refactored from existing `WorkflowRegistry` storage logic:

```go
// internal/dsl/memory_store.go
package dsl

type InMemoryWorkflowStore struct {
    mu        sync.RWMutex
    workflows map[string]*workflowEntry
}

type workflowEntry struct {
    workflow *engine.Workflow
    source   []byte
}

func NewInMemoryWorkflowStore() *InMemoryWorkflowStore {
    return &InMemoryWorkflowStore{
        workflows: make(map[string]*workflowEntry),
    }
}

// Compile-time interface check
var _ WorkflowStore = (*InMemoryWorkflowStore)(nil)
```

### `WorkflowRegistry` as Facade

The existing `WorkflowRegistry` becomes a high-level facade:

```go
// internal/dsl/registry.go (modified)
type WorkflowRegistry struct {
    store     WorkflowStore  // NEW: injected storage backend
    loader    WorkflowLoader
    parser    WorkflowParser
    validator WorkflowValidator
    converter WorkflowConverter
}

// WithStore sets a custom store backend.
func WithStore(store WorkflowStore) RegistryOption {
    return func(r *WorkflowRegistry) {
        r.store = store
    }
}

// Still satisfies engine.WorkflowRepository
var _ engine.WorkflowRepository = (*WorkflowRegistry)(nil)
```

This preserves backward compatibility—existing code using `WorkflowRegistry` continues working.

---

## 4. Configuration & API Integration

### Environment Configuration

Add to `internal/config/config.go`:

```go
type Config struct {
    // ... existing fields
    WorkflowStore string `env:"WORKFLOW_STORE" envDefault:"memory"` // "memory" or "mongo"
}
```

### Factory Function

```go
// internal/dsl/store_factory.go
package dsl

func NewWorkflowStore(storeType string, db *mongo.Database) (WorkflowStore, error) {
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

### API Bootstrap Update

```go
// cmd/workflow-api/main.go
store, err := dsl.NewWorkflowStore(cfg.WorkflowStore, mongoDB)
if err != nil {
    log.Fatal("failed to create workflow store", slog.String("error", err.Error()))
}
registry := dsl.NewWorkflowRegistry(dsl.WithStore(store))
```

---

## 5. Testing Strategy

### Unit Tests with `mtest`

```go
// internal/dsl/mongo_store_test.go
func TestMongoWorkflowStore_Register(t *testing.T) {
    mt := mtest.New(t, mtest.NewOptions().ClientType(mtest.Mock))
    
    mt.Run("success", func(mt *mtest.T) {
        store := &MongoWorkflowStore{collection: mt.Coll}
        mt.AddMockResponses(mtest.CreateSuccessResponse())
        
        err := store.Register(context.Background(), testWorkflow, testSource)
        assert.NoError(t, err)
    })
    
    mt.Run("duplicate key returns no error (upsert)", func(mt *mtest.T) {
        // Upsert should succeed even for existing documents
    })
}
```

### Test Coverage Goals

- **Zero cases**: Empty ID, nil workflow, empty source
- **Happy path**: Register, GetByID, GetSource, List, Delete
- **Error cases**: GetByID not found, Delete not found
- **Concurrency**: Multiple Register calls (thread-safety)

---

## 6. File Structure

```
internal/dsl/
├── store.go              # NEW: WorkflowStore interface + errors
├── memory_store.go       # NEW: InMemoryWorkflowStore
├── memory_store_test.go  # NEW: Tests for in-memory
├── mongo_store.go        # NEW: MongoWorkflowStore
├── mongo_store_test.go   # NEW: Tests with mtest
├── store_factory.go      # NEW: Factory function
├── registry.go           # MODIFIED: Uses WorkflowStore
├── registry_test.go      # MODIFIED: Updated tests
└── ... (existing files unchanged)
```

---

## 7. Implementation Order (TDD)

1. **Define interface** - Create `WorkflowStore` interface in `store.go`
2. **In-memory store** - Implement `InMemoryWorkflowStore` (refactor from existing)
3. **MongoDB store** - Implement `MongoWorkflowStore` with mtest tests
4. **Refactor registry** - Update `WorkflowRegistry` to use injected store
5. **Factory + config** - Add factory function and config integration
6. **API bootstrap** - Update `cmd/workflow-api/main.go`
7. **Verify** - Run all tests, ensure backward compatibility

---

## 8. MongoDB Performance Best Practices Applied

| Practice | Implementation |
|----------|---------------|
| **Unique Index** | `_id` field (automatically indexed by MongoDB) |
| **Atomic Upsert** | `UpdateOne` with `$set` and `upsert: true` |
| **Projection** | `GetSource` projects only `source` field |
| **Connection Pooling** | Reuse `*mongo.Database` from application pool |
| **Context Propagation** | All methods accept `context.Context` for timeout/cancellation |
| **Error Handling** | Check `mongo.ErrNoDocuments` for not-found cases |
