# Workflow Execution Integration Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add `POST /api/workflows/:id/execute` endpoint to trigger workflow execution via NATS.

**Architecture:** The workflow-api connects to NATS and publishes an `orchestration.execution.started` CloudEvent. The engine's Orchestrator receives this event and executes the workflow.

**Tech Stack:** Go, Echo v4, NATS JetStream, CloudEvents SDK

---

## Task 1: Add NATS Connection to workflow-api

**Files:**
- Modify: `cmd/workflow-api/main.go`

**Step 1: Add NATS imports**

Add these imports:
```go
"github.com/nats-io/nats.go"
"github.com/cheriehsieh/orchestration/internal/eventbus"
```

**Step 2: Add NATS connection after MongoDB connection**

After line 51 (`eventStore := eventstore.NewMongoEventStore...`), add:
```go
// Connect to NATS
nc, err := nats.Connect(cfg.NATSURL)
if err != nil {
    logger.Error("failed to connect to NATS", slog.Any("error", err))
    os.Exit(1)
}
defer nc.Close()

js, err := nc.JetStream()
if err != nil {
    logger.Error("failed to create JetStream context", slog.Any("error", err))
    os.Exit(1)
}

// Create EventBus for publishing execution events
executionEventBus := eventbus.NewNATSEventBus(js, "workflow.events.execution", "workflow-api", eventbus.WithLogger(logger))
logger.Info("connected to NATS", slog.String("url", cfg.NATSURL))
```

**Step 3: Update WorkflowHandler creation**

Change line 107 from:
```go
workflowHandler := dslapi.NewWorkflowHandler(registry, logger)
```
to:
```go
workflowHandler := dslapi.NewWorkflowHandler(registry, logger, dslapi.WithEventBus(executionEventBus))
```

**Step 4: Verify build**

Run: `go build ./cmd/workflow-api/...`
Expected: Build fails (WithEventBus doesn't exist yet)

---

## Task 2: Add EventBus Option to WorkflowHandler

**Files:**
- Modify: `internal/dsl/api/handler.go`

**Step 1: Add eventbus import**

Add to imports:
```go
"github.com/cheriehsieh/orchestration/internal/eventbus"
```

**Step 2: Add eventBus field and option**

After line 20 (`logger *slog.Logger`), add field:
```go
eventBus eventbus.Publisher
```

After `NewWorkflowHandler` function (around line 32), add:
```go
// HandlerOption is a functional option for WorkflowHandler.
type HandlerOption func(*WorkflowHandler)

// WithEventBus sets the event bus for publishing execution events.
func WithEventBus(eb eventbus.Publisher) HandlerOption {
    return func(h *WorkflowHandler) {
        h.eventBus = eb
    }
}
```

**Step 3: Update NewWorkflowHandler signature**

Change function signature to accept options:
```go
func NewWorkflowHandler(registry *dsl.WorkflowRegistry, logger *slog.Logger, opts ...HandlerOption) *WorkflowHandler {
    h := &WorkflowHandler{
        registry:  registry,
        parser:    dsl.NewYAMLParser(),
        validator: dsl.NewCompositeValidator(dsl.NewStructureValidator(), dsl.NewDAGValidator()),
        converter: dsl.NewDefaultConverter(),
        logger:    logger,
    }
    for _, opt := range opts {
        opt(h)
    }
    return h
}
```

**Step 4: Verify build**

Run: `go build ./cmd/workflow-api/...`
Expected: PASS

**Step 5: Commit**

```bash
git add cmd/workflow-api/main.go internal/dsl/api/handler.go
git commit -m "feat(api): add NATS connection and EventBus option to WorkflowHandler"
```

---

## Task 3: Write Failing Test for ExecuteWorkflow

**Files:**
- Modify: `internal/dsl/api/handler_test.go`

**Step 1: Add mock EventBus**

Add after imports:
```go
// mockEventBus captures published events for testing.
type mockEventBus struct {
    events []cloudevents.Event
}

func (m *mockEventBus) Publish(ctx context.Context, event cloudevents.Event) error {
    m.events = append(m.events, event)
    return nil
}
```

Add import:
```go
cloudevents "github.com/cloudevents/sdk-go/v2"
```

**Step 2: Write test for successful execution**

```go
func TestWorkflowHandler_Execute_Success(t *testing.T) {
    registry := dsl.NewWorkflowRegistry()
    logger := slog.Default()
    eventBus := &mockEventBus{}
    handler := NewWorkflowHandler(registry, logger, WithEventBus(eventBus))

    e := echo.New()
    handler.RegisterRoutes(e.Group(""))

    // Create workflow first
    wfBody := []byte(`
id: exec-test
nodes:
  - id: start
    type: StartNode
connections: []
`)
    createReq := httptest.NewRequest(http.MethodPost, "/workflows", bytes.NewReader(wfBody))
    e.ServeHTTP(httptest.NewRecorder(), createReq)

    // Execute workflow
    execBody := []byte(`{"input": {"message": "hello"}}`)
    execReq := httptest.NewRequest(http.MethodPost, "/workflows/exec-test/execute", bytes.NewReader(execBody))
    execReq.Header.Set("Content-Type", "application/json")
    rec := httptest.NewRecorder()

    e.ServeHTTP(rec, execReq)

    require.Equal(t, http.StatusCreated, rec.Code)

    var resp map[string]any
    require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
    assert.NotEmpty(t, resp["execution_id"])
    assert.Equal(t, "exec-test", resp["workflow_id"])
    assert.Equal(t, "started", resp["status"])

    // Verify event was published
    require.Len(t, eventBus.events, 1)
    assert.Equal(t, "orchestration.execution.started", eventBus.events[0].Type())
}
```

**Step 3: Run test to verify it fails**

Run: `go test ./internal/dsl/api/... -run TestWorkflowHandler_Execute_Success -v`
Expected: FAIL - route not found

---

## Task 4: Implement ExecuteWorkflow Handler

**Files:**
- Modify: `internal/dsl/api/handler.go`

**Step 1: Add imports**

Add to imports:
```go
"fmt"
"time"

cloudevents "github.com/cloudevents/sdk-go/v2"
"github.com/google/uuid"
"github.com/cheriehsieh/orchestration/internal/engine"
```

**Step 2: Add ExecuteRequest and ExecuteResponse types**

After `ConnectionResponse` struct (around line 79), add:
```go
// ExecuteRequest is the request body for executing a workflow.
type ExecuteRequest struct {
    Input map[string]any `json:"input"`
}

// ExecuteResponse is the response for a workflow execution.
type ExecuteResponse struct {
    ExecutionID string    `json:"execution_id"`
    WorkflowID  string    `json:"workflow_id"`
    Status      string    `json:"status"`
    StartedAt   time.Time `json:"started_at"`
}
```

**Step 3: Register the execute route**

In `RegisterRoutes`, add after line 41:
```go
g.POST("/workflows/:id/execute", h.ExecuteWorkflow)
```

**Step 4: Implement ExecuteWorkflow method**

Add new method (around line 280):
```go
// ExecuteWorkflow handles POST /workflows/:id/execute
func (h *WorkflowHandler) ExecuteWorkflow(c echo.Context) error {
    workflowID := c.Param("id")

    // Verify workflow exists
    _, err := h.registry.GetByID(c.Request().Context(), workflowID)
    if err != nil {
        return c.JSON(http.StatusNotFound, map[string]string{"error": "workflow not found"})
    }

    // Parse request body
    var req ExecuteRequest
    if err := c.Bind(&req); err != nil {
        return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid request body"})
    }

    // Check if eventBus is configured
    if h.eventBus == nil {
        return c.JSON(http.StatusServiceUnavailable, map[string]string{
            "error": "execution not available - event bus not configured",
        })
    }

    // Generate execution ID
    execID := fmt.Sprintf("exec-%d", time.Now().Unix())

    // Create CloudEvent
    event := cloudevents.NewEvent()
    event.SetID(uuid.New().String())
    event.SetSource("orchestration.workflow-api")
    event.SetType(engine.ExecutionStarted)
    event.SetSubject(execID)
    _ = event.SetData(cloudevents.ApplicationJSON, engine.ExecutionStartedData{
        WorkflowID: workflowID,
        InputData:  req.Input,
    })

    // Publish to NATS
    if err := h.eventBus.Publish(c.Request().Context(), event); err != nil {
        h.logger.Error("failed to publish execution event",
            slog.String("workflow_id", workflowID),
            slog.Any("error", err),
        )
        return c.JSON(http.StatusInternalServerError, map[string]string{"error": "failed to start execution"})
    }

    h.logger.Info("workflow execution started",
        slog.String("workflow_id", workflowID),
        slog.String("execution_id", execID),
    )

    return c.JSON(http.StatusCreated, ExecuteResponse{
        ExecutionID: execID,
        WorkflowID:  workflowID,
        Status:      "started",
        StartedAt:   time.Now(),
    })
}
```

**Step 5: Run test to verify it passes**

Run: `go test ./internal/dsl/api/... -run TestWorkflowHandler_Execute_Success -v`
Expected: PASS

**Step 6: Commit**

```bash
git add internal/dsl/api/handler.go internal/dsl/api/handler_test.go
git commit -m "feat(api): add POST /workflows/:id/execute endpoint"
```

---

## Task 5: Add Error Case Tests

**Files:**
- Modify: `internal/dsl/api/handler_test.go`

**Step 1: Test workflow not found**

```go
func TestWorkflowHandler_Execute_NotFound(t *testing.T) {
    registry := dsl.NewWorkflowRegistry()
    logger := slog.Default()
    eventBus := &mockEventBus{}
    handler := NewWorkflowHandler(registry, logger, WithEventBus(eventBus))

    e := echo.New()
    handler.RegisterRoutes(e.Group(""))

    execBody := []byte(`{"input": {}}`)
    req := httptest.NewRequest(http.MethodPost, "/workflows/nonexistent/execute", bytes.NewReader(execBody))
    req.Header.Set("Content-Type", "application/json")
    rec := httptest.NewRecorder()

    e.ServeHTTP(rec, req)

    assert.Equal(t, http.StatusNotFound, rec.Code)
    assert.Empty(t, eventBus.events)
}
```

**Step 2: Test no event bus configured**

```go
func TestWorkflowHandler_Execute_NoEventBus(t *testing.T) {
    registry := dsl.NewWorkflowRegistry()
    logger := slog.Default()
    handler := NewWorkflowHandler(registry, logger) // No EventBus

    e := echo.New()
    handler.RegisterRoutes(e.Group(""))

    // Create workflow
    wfBody := []byte(`
id: no-bus-test
nodes:
  - id: start
    type: StartNode
connections: []
`)
    createReq := httptest.NewRequest(http.MethodPost, "/workflows", bytes.NewReader(wfBody))
    e.ServeHTTP(httptest.NewRecorder(), createReq)

    // Try to execute
    execBody := []byte(`{"input": {}}`)
    req := httptest.NewRequest(http.MethodPost, "/workflows/no-bus-test/execute", bytes.NewReader(execBody))
    req.Header.Set("Content-Type", "application/json")
    rec := httptest.NewRecorder()

    e.ServeHTTP(rec, req)

    assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}
```

**Step 3: Run all tests**

Run: `go test ./internal/dsl/api/... -v`
Expected: All PASS

**Step 4: Commit**

```bash
git add internal/dsl/api/handler_test.go
git commit -m "test(api): add error case tests for execute endpoint"
```

---

## Task 6: End-to-End Verification

**Step 1: Start infrastructure**

```bash
docker compose up -d mongodb nats
```

**Step 2: Start engine (terminal 1)**

```bash
go run ./cmd/engine/main.go
```

Expected: Engine starts, connects to MongoDB and NATS

**Step 3: Start workflow-api (terminal 2)**

```bash
go run ./cmd/workflow-api/main.go
```

Expected: `connected to NATS` log message

**Step 4: Create a workflow**

```bash
curl -X POST http://localhost:8083/api/workflows \
  -H "Content-Type: text/yaml" \
  -d 'id: e2e-test
name: E2E Test
version: "1.0"
nodes:
  - id: start
    type: StartNode
  - id: action
    type: ActionNode
connections:
  - from: start
    to: action'
```

Expected: `{"id":"e2e-test","message":"workflow created"}`

**Step 5: Execute the workflow**

```bash
curl -X POST http://localhost:8083/api/workflows/e2e-test/execute \
  -H "Content-Type: application/json" \
  -d '{"input": {"message": "hello world"}}'
```

Expected: `{"execution_id":"exec-...","workflow_id":"e2e-test","status":"started",...}`

**Step 6: Verify in engine logs**

Look for:
- `orchestration.execution.started` received
- `▶ START node executed`
- `⚙ ACTION node executed`

**Step 7: Check execution via API**

```bash
curl http://localhost:8083/api/executions/{execution_id}
```

Expected: Execution details with status

**Step 8: Final commit**

```bash
git add .
git commit -m "feat: workflow-api to engine integration complete"
```
