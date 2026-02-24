# Execution Linking Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build bidirectional execution linking so parent workflows can query which child workflows were triggered via Event Marketplace.

**Architecture:** Hybrid data model stores `parentExecutionId` on child + `childExecutionIds` on parent for O(1) lookups. `ExecutionLinkHandler` listens for `ExecutionStarted` events and async-updates parent records. New `executions` collection provides materialized view with linking fields.

**Tech Stack:** Go 1.25+, MongoDB, NATS JetStream, Echo v4, React

---

## Task 1: Execution Model & Store Interface

**Files:**
- Create: `internal/eventstore/execution.go`
- Create: `internal/eventstore/execution_store.go`
- Test: `internal/eventstore/execution_store_test.go`

### Step 1: Write test for Execution model and ExecutionStore interface

```go
// internal/eventstore/execution_store_test.go
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
```

### Step 2: Run test to verify it fails

Run: `go test ./internal/eventstore -run TestMongoExecutionStore_Create -v`
Expected: FAIL with "undefined: NewMongoExecutionStore"

### Step 3: Write Execution model

```go
// internal/eventstore/execution.go
package eventstore

import "time"

// Execution status constants.
const (
	StatusRunning   = "running"
	StatusCompleted = "completed"
	StatusFailed    = "failed"
)

// Execution represents a workflow execution with linking metadata.
type Execution struct {
	ID                string     `bson:"_id" json:"id"`
	WorkflowID        string     `bson:"workflow_id" json:"workflow_id"`
	Status            string     `bson:"status" json:"status"`
	StartedAt         time.Time  `bson:"started_at" json:"started_at"`
	CompletedAt       *time.Time `bson:"completed_at,omitempty" json:"completed_at,omitempty"`
	ParentExecutionID string     `bson:"parent_execution_id,omitempty" json:"parent_execution_id,omitempty"`
	ChildExecutionIDs []string   `bson:"child_execution_ids,omitempty" json:"child_execution_ids,omitempty"`
	TriggeredByEvent  string     `bson:"triggered_by_event,omitempty" json:"triggered_by_event,omitempty"`
}
```

### Step 4: Write ExecutionStore interface and MongoDB implementation

```go
// internal/eventstore/execution_store.go
package eventstore

import (
	"context"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ExecutionStore manages execution state with linking.
type ExecutionStore interface {
	Create(ctx context.Context, exec *Execution) error
	GetByID(ctx context.Context, id string) (*Execution, error)
	GetChildren(ctx context.Context, parentID string) ([]*Execution, error)
	AddChildExecution(ctx context.Context, parentID, childID string) error
	UpdateStatus(ctx context.Context, id, status string) error
}

// MongoExecutionStore implements ExecutionStore using MongoDB.
type MongoExecutionStore struct {
	collection *mongo.Collection
}

// NewMongoExecutionStore creates a new MongoDB-backed execution store.
func NewMongoExecutionStore(db *mongo.Database, collectionName string) *MongoExecutionStore {
	return &MongoExecutionStore{
		collection: db.Collection(collectionName),
	}
}

func (s *MongoExecutionStore) Create(ctx context.Context, exec *Execution) error {
	_, err := s.collection.InsertOne(ctx, exec)
	return err
}

func (s *MongoExecutionStore) GetByID(ctx context.Context, id string) (*Execution, error) {
	var exec Execution
	err := s.collection.FindOne(ctx, bson.M{"_id": id}).Decode(&exec)
	if err != nil {
		return nil, err
	}
	return &exec, nil
}

func (s *MongoExecutionStore) GetChildren(ctx context.Context, parentID string) ([]*Execution, error) {
	cursor, err := s.collection.Find(ctx, bson.M{"parent_execution_id": parentID})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var children []*Execution
	if err := cursor.All(ctx, &children); err != nil {
		return nil, err
	}
	return children, nil
}

func (s *MongoExecutionStore) AddChildExecution(ctx context.Context, parentID, childID string) error {
	_, err := s.collection.UpdateOne(ctx,
		bson.M{"_id": parentID},
		bson.M{"$addToSet": bson.M{"child_execution_ids": childID}},
	)
	return err
}

func (s *MongoExecutionStore) UpdateStatus(ctx context.Context, id, status string) error {
	_, err := s.collection.UpdateOne(ctx,
		bson.M{"_id": id},
		bson.M{"$set": bson.M{"status": status}},
	)
	return err
}

// EnsureIndexes creates required indexes for efficient queries.
func (s *MongoExecutionStore) EnsureIndexes(ctx context.Context) error {
	indexes := []mongo.IndexModel{
		{Keys: bson.D{{Key: "parent_execution_id", Value: 1}}},
		{Keys: bson.D{{Key: "workflow_id", Value: 1}, {Key: "started_at", Value: -1}}},
	}
	_, err := s.collection.Indexes().CreateMany(ctx, indexes)
	return err
}
```

### Step 5: Run test to verify it passes

Run: `go test ./internal/eventstore -run TestMongoExecutionStore_Create -v`
Expected: PASS

### Step 6: Commit

```bash
git add internal/eventstore/execution.go internal/eventstore/execution_store.go internal/eventstore/execution_store_test.go
git commit -m "feat(eventstore): add ExecutionStore for execution linking"
```

---

## Task 2: ExecutionStore Additional Tests

**Files:**
- Modify: `internal/eventstore/execution_store_test.go`

### Step 1: Write tests for GetChildren and AddChildExecution

```go
// Add to internal/eventstore/execution_store_test.go

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
```

### Step 2: Run tests to verify they pass

Run: `go test ./internal/eventstore -run TestMongoExecutionStore -v`
Expected: PASS (all 3 tests)

### Step 3: Commit

```bash
git add internal/eventstore/execution_store_test.go
git commit -m "test(eventstore): add GetChildren and AddChildExecution tests"
```

---

## Task 3: ExecutionLinkHandler

**Files:**
- Create: `internal/scheduler/link_handler.go`
- Test: `internal/scheduler/link_handler_test.go`

### Step 1: Write failing test for ExecutionLinkHandler

```go
// internal/scheduler/link_handler_test.go
package scheduler

import (
	"context"
	"testing"
	"time"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cheriehsieh/orchestration/internal/engine"
	"github.com/cheriehsieh/orchestration/internal/eventstore"
)

type mockExecutionStore struct {
	executions map[string]*eventstore.Execution
	children   map[string][]string
}

func newMockExecutionStore() *mockExecutionStore {
	return &mockExecutionStore{
		executions: make(map[string]*eventstore.Execution),
		children:   make(map[string][]string),
	}
}

func (m *mockExecutionStore) Create(ctx context.Context, exec *eventstore.Execution) error {
	m.executions[exec.ID] = exec
	return nil
}

func (m *mockExecutionStore) GetByID(ctx context.Context, id string) (*eventstore.Execution, error) {
	return m.executions[id], nil
}

func (m *mockExecutionStore) GetChildren(ctx context.Context, parentID string) ([]*eventstore.Execution, error) {
	return nil, nil
}

func (m *mockExecutionStore) AddChildExecution(ctx context.Context, parentID, childID string) error {
	m.children[parentID] = append(m.children[parentID], childID)
	return nil
}

func (m *mockExecutionStore) UpdateStatus(ctx context.Context, id, status string) error {
	if exec, ok := m.executions[id]; ok {
		exec.Status = status
	}
	return nil
}

func TestExecutionLinkHandler_HandleWithParent(t *testing.T) {
	store := newMockExecutionStore()
	handler := NewExecutionLinkHandler(store)

	// Create parent execution first
	store.executions["parent-123"] = &eventstore.Execution{
		ID:         "parent-123",
		WorkflowID: "hr-onboarding",
		Status:     eventstore.StatusRunning,
		StartedAt:  time.Now(),
	}

	// Create ExecutionStarted event with parent reference
	event := cloudevents.NewEvent()
	event.SetID("evt-1")
	event.SetType(engine.ExecutionStarted)
	event.SetSubject("child-456")
	event.SetSource(engine.EventSource)
	event.SetExtension("workflowid", "chat-new-member")
	event.SetExtension("parentexecutionid", "parent-123")
	event.SetExtension("triggeredbyevent", "hr.new_employee")
	_ = event.SetData(cloudevents.ApplicationJSON, engine.ExecutionStartedData{WorkflowID: "chat-new-member"})

	ctx := context.Background()
	err := handler.Handle(ctx, event)
	require.NoError(t, err)

	// Verify child was created with parent reference
	child := store.executions["child-456"]
	require.NotNil(t, child)
	assert.Equal(t, "parent-123", child.ParentExecutionID)
	assert.Equal(t, "hr.new_employee", child.TriggeredByEvent)

	// Verify parent was updated with child reference
	assert.Contains(t, store.children["parent-123"], "child-456")
}

func TestExecutionLinkHandler_HandleNoParent(t *testing.T) {
	store := newMockExecutionStore()
	handler := NewExecutionLinkHandler(store)

	// Create ExecutionStarted event without parent (root execution)
	event := cloudevents.NewEvent()
	event.SetID("evt-2")
	event.SetType(engine.ExecutionStarted)
	event.SetSubject("root-789")
	event.SetSource(engine.EventSource)
	event.SetExtension("workflowid", "hr-onboarding")
	_ = event.SetData(cloudevents.ApplicationJSON, engine.ExecutionStartedData{WorkflowID: "hr-onboarding"})

	ctx := context.Background()
	err := handler.Handle(ctx, event)
	require.NoError(t, err)

	// Verify execution was created
	exec := store.executions["root-789"]
	require.NotNil(t, exec)
	assert.Empty(t, exec.ParentExecutionID)

	// Verify no parent update happened
	assert.Empty(t, store.children)
}
```

### Step 2: Run test to verify it fails

Run: `go test ./internal/scheduler -run TestExecutionLinkHandler -v`
Expected: FAIL with "undefined: NewExecutionLinkHandler"

### Step 3: Write ExecutionLinkHandler implementation

```go
// internal/scheduler/link_handler.go
package scheduler

import (
	"context"
	"log/slog"
	"time"

	cloudevents "github.com/cloudevents/sdk-go/v2"

	"github.com/cheriehsieh/orchestration/internal/engine"
	"github.com/cheriehsieh/orchestration/internal/eventstore"
)

// ExecutionLinkHandler handles ExecutionStarted events to maintain parent-child links.
type ExecutionLinkHandler struct {
	executionStore eventstore.ExecutionStore
	logger         *slog.Logger
}

// ExecutionLinkHandlerOption is a functional option.
type ExecutionLinkHandlerOption func(*ExecutionLinkHandler)

// WithLinkHandlerLogger sets a custom logger.
func WithLinkHandlerLogger(logger *slog.Logger) ExecutionLinkHandlerOption {
	return func(h *ExecutionLinkHandler) {
		h.logger = logger
	}
}

// NewExecutionLinkHandler creates a new handler for execution linking.
func NewExecutionLinkHandler(store eventstore.ExecutionStore, opts ...ExecutionLinkHandlerOption) *ExecutionLinkHandler {
	h := &ExecutionLinkHandler{
		executionStore: store,
		logger:         slog.Default(),
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// Handle processes an ExecutionStarted event and creates/links executions.
func (h *ExecutionLinkHandler) Handle(ctx context.Context, event cloudevents.Event) error {
	if event.Type() != engine.ExecutionStarted {
		return nil
	}

	executionID := event.Subject()
	workflowID, _ := event.Extensions()["workflowid"].(string)
	parentExecutionID, _ := event.Extensions()["parentexecutionid"].(string)
	triggeredByEvent, _ := event.Extensions()["triggeredbyevent"].(string)

	// Create execution record
	exec := &eventstore.Execution{
		ID:                executionID,
		WorkflowID:        workflowID,
		Status:            eventstore.StatusRunning,
		StartedAt:         event.Time(),
		ParentExecutionID: parentExecutionID,
		TriggeredByEvent:  triggeredByEvent,
	}

	if exec.StartedAt.IsZero() {
		exec.StartedAt = time.Now()
	}

	if err := h.executionStore.Create(ctx, exec); err != nil {
		h.logger.Error("failed to create execution record",
			slog.String("execution_id", executionID),
			slog.Any("error", err),
		)
		return err
	}

	// If has parent, update parent's child list
	if parentExecutionID != "" {
		if err := h.executionStore.AddChildExecution(ctx, parentExecutionID, executionID); err != nil {
			h.logger.Warn("failed to update parent child list",
				slog.String("parent_id", parentExecutionID),
				slog.String("child_id", executionID),
				slog.Any("error", err),
			)
			// Non-fatal: linking is observability, not critical path
		}

		h.logger.Info("linked child execution to parent",
			slog.String("child_id", executionID),
			slog.String("parent_id", parentExecutionID),
			slog.String("triggered_by", triggeredByEvent),
		)
	}

	return nil
}
```

### Step 4: Run tests to verify they pass

Run: `go test ./internal/scheduler -run TestExecutionLinkHandler -v`
Expected: PASS

### Step 5: Commit

```bash
git add internal/scheduler/link_handler.go internal/scheduler/link_handler_test.go
git commit -m "feat(scheduler): add ExecutionLinkHandler for parent-child linking"
```

---

## Task 4: EventRouter Enhancement

**Files:**
- Modify: `internal/scheduler/router.go:155-165`

### Step 1: Write test for triggeredbyevent extension

```go
// Add to internal/scheduler/router_test.go

func TestEventRouter_SetsTriggeredByEvent(t *testing.T) {
	// This test verifies the EventRouter sets triggeredbyevent extension
	// when starting workflows from marketplace events
	// (Tested as part of integration, verify in handleMessage)
}
```

### Step 2: Modify EventRouter to set triggeredbyevent

In `internal/scheduler/router.go`, locate `handleMessage` and add the extension:

```go
// Around line 160, after setting parentexecutionid
if parentExecutionID != "" {
    startEvent.SetExtension("parentexecutionid", parentExecutionID)
}
startEvent.SetExtension("triggeredbyevent", domain+"."+eventName)  // ADD THIS LINE
```

### Step 3: Run existing router tests

Run: `go test ./internal/scheduler -run TestEventRouter -v`
Expected: PASS

### Step 4: Commit

```bash
git add internal/scheduler/router.go
git commit -m "feat(router): add triggeredbyevent extension for tracing"
```

---

## Task 5: REST API - GetChildren Endpoint

**Files:**
- Modify: `internal/api/execution_handler.go`
- Test: `internal/api/execution_handler_test.go`

### Step 1: Write failing test for GetChildren

```go
// Add to internal/api/execution_handler_test.go

func TestGetChildren_Success(t *testing.T) {
	store := newMockEventStore()
	execStore := newMockExecutionStoreForAPI()

	// Setup parent and children
	execStore.executions["parent-1"] = &eventstore.Execution{
		ID:                "parent-1",
		WorkflowID:        "hr-onboarding",
		Status:            "completed",
		ChildExecutionIDs: []string{"child-1", "child-2"},
	}
	execStore.executions["child-1"] = &eventstore.Execution{
		ID:                "child-1",
		WorkflowID:        "chat-new-member",
		Status:            "completed",
		ParentExecutionID: "parent-1",
		TriggeredByEvent:  "hr.new_employee",
	}
	execStore.executions["child-2"] = &eventstore.Execution{
		ID:                "child-2",
		WorkflowID:        "it-provisioning",
		Status:            "running",
		ParentExecutionID: "parent-1",
		TriggeredByEvent:  "hr.new_employee",
	}

	handler := NewExecutionHandler(store, WithExecutionStore(execStore))

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/executions/parent-1/children", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("parent-1")

	err := handler.GetChildren(c)

	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"child-1"`)
	assert.Contains(t, rec.Body.String(), `"child-2"`)
	assert.Contains(t, rec.Body.String(), `"triggered_by_event":"hr.new_employee"`)
}

func TestGetChildren_NoChildren(t *testing.T) {
	store := newMockEventStore()
	execStore := newMockExecutionStoreForAPI()

	execStore.executions["leaf-1"] = &eventstore.Execution{
		ID:         "leaf-1",
		WorkflowID: "simple-wf",
		Status:     "completed",
	}

	handler := NewExecutionHandler(store, WithExecutionStore(execStore))

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/executions/leaf-1/children", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("leaf-1")

	err := handler.GetChildren(c)

	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"children":[]`)
}
```

### Step 2: Run test to verify it fails

Run: `go test ./internal/api -run TestGetChildren -v`
Expected: FAIL with "undefined: WithExecutionStore" or "handler.GetChildren undefined"

### Step 3: Implement GetChildren endpoint

Update `internal/api/execution_handler.go`:

```go
// Add to ExecutionHandler struct
type ExecutionHandler struct {
    eventStore     eventstore.EventStore
    executionStore eventstore.ExecutionStore // ADD
}

// Add functional option
func WithExecutionStore(store eventstore.ExecutionStore) ExecutionHandlerOption {
    return func(h *ExecutionHandler) {
        h.executionStore = store
    }
}

// Add GetChildren method
func (h *ExecutionHandler) GetChildren(c echo.Context) error {
    id := c.Param("id")

    if h.executionStore == nil {
        return c.JSON(http.StatusNotImplemented, map[string]string{
            "error": "execution store not configured",
        })
    }

    children, err := h.executionStore.GetChildren(c.Request().Context(), id)
    if err != nil {
        return c.JSON(http.StatusInternalServerError, map[string]string{
            "error": err.Error(),
        })
    }

    if children == nil {
        children = []*eventstore.Execution{}
    }

    return c.JSON(http.StatusOK, map[string]any{
        "parent_execution_id": id,
        "children":            children,
    })
}

// Update RegisterRoutes
func (h *ExecutionHandler) RegisterRoutes(g *echo.Group) {
    g.GET("/executions/:id", h.GetExecution)
    g.GET("/executions/:id/events", h.GetEvents)
    g.GET("/executions/:id/children", h.GetChildren) // ADD
}
```

### Step 4: Run tests to verify they pass

Run: `go test ./internal/api -run TestGetChildren -v`
Expected: PASS

### Step 5: Commit

```bash
git add internal/api/execution_handler.go internal/api/execution_handler_test.go
git commit -m "feat(api): add GET /executions/:id/children endpoint"
```

---

## Task 6: Wire Up ExecutionLinkHandler in Engine

**Files:**
- Modify: `cmd/engine/main.go`

### Step 1: Add ExecutionStore and LinkHandler initialization

Locate the engine initialization section and add:

```go
// After eventStore initialization
executionStore := eventstore.NewMongoExecutionStore(db, "executions")
if err := executionStore.EnsureIndexes(ctx); err != nil {
    logger.Warn("failed to ensure execution indexes", slog.Any("error", err))
}

// Create link handler
linkHandler := scheduler.NewExecutionLinkHandler(executionStore, scheduler.WithLinkHandlerLogger(logger))

// Subscribe to execution.started events for linking
// (Add to scheduler or create separate consumer)
```

### Step 2: Run the engine and verify no errors

Run: `go run ./cmd/engine`
Expected: Engine starts without errors

### Step 3: Commit

```bash
git add cmd/engine/main.go
git commit -m "feat(engine): wire ExecutionLinkHandler for execution linking"
```

---

## Task 7: Integration Verification

### Step 1: Start the engine

```bash
cd /Users/cheriehsieh/Program/orchestration
go run ./cmd/engine
```

### Step 2: Deploy test workflows

```bash
# HR Onboarding (publisher)
curl -X POST http://localhost:8083/api/workflows \
  -H "Content-Type: application/yaml" \
  --data-binary @examples/marketplace/hr-onboarding.yaml

# Chat Member (subscriber)
curl -X POST http://localhost:8083/api/workflows \
  -H "Content-Type: application/yaml" \
  --data-binary @examples/marketplace/chat-new-member.yaml
```

### Step 3: Trigger parent workflow

```bash
curl -X POST http://localhost:8083/api/workflows/hr-onboarding/execute \
  -H "Content-Type: application/json" \
  -d '{"name": "Alice Smith", "employee_id": "EMP-001"}'
```

Note the returned `execution_id`.

### Step 4: Verify children endpoint

```bash
curl http://localhost:8083/api/executions/<parent-execution-id>/children
```

Expected response:
```json
{
  "parent_execution_id": "<parent-id>",
  "children": [
    {
      "id": "<child-id>",
      "workflow_id": "chat-new-member",
      "status": "completed",
      "triggered_by_event": "hr.new_employee"
    }
  ]
}
```

### Step 5: Commit all tests pass

```bash
go test ./... -v
git add .
git commit -m "test: verify execution linking integration"
```

---

## Summary

| Task | Description | Status |
|------|-------------|--------|
| 1 | ExecutionStore model & interface | ⬜ |
| 2 | ExecutionStore additional tests | ⬜ |
| 3 | ExecutionLinkHandler | ⬜ |
| 4 | EventRouter enhancement | ⬜ |
| 5 | REST API GetChildren | ⬜ |
| 6 | Wire up in engine | ⬜ |
| 7 | Integration verification | ⬜ |
