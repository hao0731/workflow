# Event Marketplace Scenarios Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Enable HR workflow to publish `new_employee` event to marketplace, triggering Chat Service workflow automatically.

**Architecture:** DSL workflow API registers event definitions to marketplace on deploy. DSL registry implements `WorkflowMatcher` so EventRouter can find subscriber workflows. End-to-end: HR workflow → PublishEvent → NATS → EventRouter → Chat workflow.

**Tech Stack:** Go 1.25, Echo v4, NATS JetStream, MongoDB, CloudEvents

---

## Task 1: Add EventRegistry Dependency to WorkflowHandler

**Files:**
- Modify: `internal/dsl/api/handler.go:21-61`
- Test: `internal/dsl/api/handler_test.go`

**Step 1: Write the failing test**

```go
// In handler_test.go, add new test:
func TestWorkflowHandler_Create_RegistersEventsToMarketplace(t *testing.T) {
	// Setup mock registry
	mockEventRegistry := marketplace.NewInMemoryEventRegistry()
	mockWorkflowRegistry := dsl.NewWorkflowRegistry()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	handler := api.NewWorkflowHandler(mockWorkflowRegistry, logger,
		api.WithEventRegistry(mockEventRegistry),
	)

	// YAML with events section
	yamlBody := `
id: test-publisher
nodes:
  - id: start
    type: StartNode
connections: []
events:
  - name: test_event
    domain: testing
    description: A test event
`
	req := httptest.NewRequest(http.MethodPost, "/api/workflows", strings.NewReader(yamlBody))
	req.Header.Set("Content-Type", "text/x-yaml")
	rec := httptest.NewRecorder()
	c := echo.New().NewContext(req, rec)

	err := handler.Create(c)
	require.NoError(t, err)
	assert.Equal(t, http.StatusCreated, rec.Code)

	// Verify event was registered
	event, err := mockEventRegistry.Get(context.Background(), "testing", "test_event")
	require.NoError(t, err)
	assert.Equal(t, "test_event", event.Name)
	assert.Equal(t, "testing", event.Domain)
}
```

**Step 2: Run test to verify it fails**

Run: `go test -v ./internal/dsl/api/... -run TestWorkflowHandler_Create_RegistersEventsToMarketplace`
Expected: FAIL with "undefined: api.WithEventRegistry"

**Step 3: Add WithEventRegistry option and field**

```go
// In handler.go, add to WorkflowHandler struct:
type WorkflowHandler struct {
	registry      *dsl.WorkflowRegistry
	parser        dsl.WorkflowParser
	validator     dsl.WorkflowValidator
	converter     dsl.WorkflowConverter
	logger        *slog.Logger
	eventBus      eventbus.Publisher
	eventStore    eventstore.EventStore
	eventRegistry marketplace.EventRegistry // NEW
}

// Add import for marketplace package
import "github.com/cheriehsieh/orchestration/internal/marketplace"

// Add functional option:
func WithEventRegistry(er marketplace.EventRegistry) HandlerOption {
	return func(h *WorkflowHandler) {
		h.eventRegistry = er
	}
}
```

**Step 4: Run test to verify it compiles but still fails**

Run: `go test -v ./internal/dsl/api/... -run TestWorkflowHandler_Create_RegistersEventsToMarketplace`
Expected: FAIL with assertion "event not found"

**Step 5: Implement event registration in Create()**

```go
// In Create(), after RegisterWithSource succeeds (~line 163), add:
// Register events to marketplace
if h.eventRegistry != nil && len(def.Events) > 0 {
	for _, ev := range def.Events {
		eventDef := &marketplace.EventDefinition{
			Name:        ev.Name,
			Domain:      ev.Domain,
			Description: ev.Description,
			Schema:      ev.Schema,
			Owner:       wf.ID,
		}
		if err := h.eventRegistry.Register(c.Request().Context(), eventDef); err != nil {
			if !errors.Is(err, marketplace.ErrEventAlreadyExists) {
				h.logger.Warn("failed to register event",
					slog.String("event", ev.Name),
					slog.Any("error", err),
				)
			}
		}
	}
}
```

**Step 6: Run test to verify it passes**

Run: `go test -v ./internal/dsl/api/... -run TestWorkflowHandler_Create_RegistersEventsToMarketplace`
Expected: PASS

**Step 7: Commit**

```bash
git add internal/dsl/api/handler.go internal/dsl/api/handler_test.go
git commit -m "feat(dsl): register events to marketplace on workflow create"
```

---

## Task 2: Add Event Registration to Update Handler

**Files:**
- Modify: `internal/dsl/api/handler.go:233-279`
- Test: `internal/dsl/api/handler_test.go`

**Step 1: Write the failing test**

```go
func TestWorkflowHandler_Update_RegistersEventsToMarketplace(t *testing.T) {
	mockEventRegistry := marketplace.NewInMemoryEventRegistry()
	mockWorkflowRegistry := dsl.NewWorkflowRegistry()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	handler := api.NewWorkflowHandler(mockWorkflowRegistry, logger,
		api.WithEventRegistry(mockEventRegistry),
	)

	// First create workflow without events
	yamlCreate := `id: test-publisher
nodes:
  - id: start
    type: StartNode
connections: []
`
	req := httptest.NewRequest(http.MethodPost, "/api/workflows", strings.NewReader(yamlCreate))
	rec := httptest.NewRecorder()
	_ = handler.Create(echo.New().NewContext(req, rec))

	// Now update with events
	yamlUpdate := `id: test-publisher
nodes:
  - id: start
    type: StartNode
connections: []
events:
  - name: new_event
    domain: testing
`
	req = httptest.NewRequest(http.MethodPut, "/api/workflows/test-publisher", strings.NewReader(yamlUpdate))
	rec = httptest.NewRecorder()
	c := echo.New().NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("test-publisher")

	err := handler.Update(c)
	require.NoError(t, err)

	// Verify event was registered
	event, err := mockEventRegistry.Get(context.Background(), "testing", "new_event")
	require.NoError(t, err)
	assert.Equal(t, "new_event", event.Name)
}
```

**Step 2: Run test to verify it fails**

Run: `go test -v ./internal/dsl/api/... -run TestWorkflowHandler_Update_RegistersEventsToMarketplace`
Expected: FAIL with "event not found"

**Step 3: Add same event registration logic to Update()**

```go
// In Update(), after RegisterWithSource succeeds (~line 271), add same logic as Create:
if h.eventRegistry != nil && len(def.Events) > 0 {
	for _, ev := range def.Events {
		eventDef := &marketplace.EventDefinition{
			Name:        ev.Name,
			Domain:      ev.Domain,
			Description: ev.Description,
			Schema:      ev.Schema,
			Owner:       wf.ID,
		}
		if err := h.eventRegistry.Register(c.Request().Context(), eventDef); err != nil {
			if !errors.Is(err, marketplace.ErrEventAlreadyExists) {
				h.logger.Warn("failed to register event",
					slog.String("event", ev.Name),
					slog.Any("error", err),
				)
			}
		}
	}
}
```

**Step 4: Run test to verify it passes**

Run: `go test -v ./internal/dsl/api/... -run TestWorkflowHandler_Update_RegistersEventsToMarketplace`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/dsl/api/handler.go internal/dsl/api/handler_test.go
git commit -m "feat(dsl): register events to marketplace on workflow update"
```

---

## Task 3: Implement FindByEventTrigger in DSL Registry

**Files:**
- Modify: `internal/dsl/registry.go`
- Test: `internal/dsl/registry_test.go`

**Step 1: Write the failing test**

```go
func TestWorkflowRegistry_FindByEventTrigger(t *testing.T) {
	registry := NewWorkflowRegistry()

	// Register workflow with event trigger
	wfWithTrigger := &engine.Workflow{
		ID: "subscriber-workflow",
		Nodes: []engine.Node{
			{
				ID:   "start",
				Type: engine.StartNode,
				Trigger: &engine.Trigger{
					Type: engine.TriggerEvent,
					Criteria: map[string]any{
						"event_name": "order_created",
						"domain":     "ecommerce",
					},
				},
			},
		},
	}
	_ = registry.Register(wfWithTrigger)

	// Register workflow without trigger
	wfNoTrigger := &engine.Workflow{
		ID: "manual-workflow",
		Nodes: []engine.Node{
			{ID: "start", Type: engine.StartNode},
		},
	}
	_ = registry.Register(wfNoTrigger)

	// Find by matching event
	found, err := registry.FindByEventTrigger(context.Background(), "order_created", "ecommerce")
	require.NoError(t, err)
	require.Len(t, found, 1)
	assert.Equal(t, "subscriber-workflow", found[0].ID)

	// Find by non-matching event
	found, err = registry.FindByEventTrigger(context.Background(), "other_event", "other")
	require.NoError(t, err)
	assert.Len(t, found, 0)
}
```

**Step 2: Run test to verify it fails**

Run: `go test -v ./internal/dsl/... -run TestWorkflowRegistry_FindByEventTrigger`
Expected: FAIL with "undefined: registry.FindByEventTrigger"

**Step 3: Implement FindByEventTrigger method**

```go
// In registry.go, add method:

// FindByEventTrigger finds workflows with StartNode triggers matching the event.
// Implements scheduler.WorkflowMatcher interface.
func (r *WorkflowRegistry) FindByEventTrigger(ctx context.Context, eventName, domain string) ([]*engine.Workflow, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var matches []*engine.Workflow
	for _, wf := range r.workflows {
		start := wf.GetStartNode()
		if start == nil {
			continue
		}
		evName, evDomain, ok := start.GetEventTrigger()
		if !ok {
			continue
		}
		if evName == eventName && evDomain == domain {
			matches = append(matches, wf)
		}
	}
	return matches, nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test -v ./internal/dsl/... -run TestWorkflowRegistry_FindByEventTrigger`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/dsl/registry.go internal/dsl/registry_test.go
git commit -m "feat(dsl): implement FindByEventTrigger for WorkflowMatcher interface"
```

---

## Task 4: Wire EventRegistry in Workflow API Server

**Files:**
- Modify: `cmd/workflow-api/main.go:145-155`

**Step 1: Verify current wiring**

Check current handler creation doesn't include eventRegistry.

**Step 2: Add eventRegistry to handler**

```go
// Around line 145, modify workflowHandler creation:
workflowHandler := dslapi.NewWorkflowHandler(registry, logger,
	dslapi.WithEventBus(executionEventBus),
	dslapi.WithEventStore(eventStore),
	dslapi.WithEventRegistry(eventRegistry), // ADD THIS LINE
)
```

**Step 3: Run API to verify it compiles**

Run: `go build ./cmd/workflow-api/...`
Expected: SUCCESS

**Step 4: Commit**

```bash
git add cmd/workflow-api/main.go
git commit -m "feat(api): wire event registry to workflow handler"
```

---

## Task 5: Create Example Workflow Files

**Files:**
- Create: `examples/marketplace/hr-onboarding.yaml`
- Create: `examples/marketplace/chat-new-member.yaml`

**Step 1: Create HR onboarding workflow**

```yaml
# examples/marketplace/hr-onboarding.yaml
id: hr-onboarding
name: "HR Employee Onboarding"
description: "Registers new employees and publishes event to marketplace"
version: "1.0.0"

events:
  - name: new_employee
    domain: hr
    description: "Fired when a new employee is onboarded"
    schema:
      type: object
      properties:
        name:
          type: string
        employee_id:
          type: string
      required:
        - name
        - employee_id

nodes:
  - id: start
    type: StartNode
    name: "Start"

  - id: on-boarding
    type: ActionNode
    name: "On-boarding Task"
    parameters:
      message: "Processing onboarding for {{.input.name}} ({{.input.employee_id}})"

  - id: publish-new-employee
    type: PublishEvent
    name: "Publish New Employee Event"
    parameters:
      event_name: new_employee
      domain: hr
      payload:
        name: "{{.input.name}}"
        employee_id: "{{.input.employee_id}}"

connections:
  - from: start
    to: on-boarding
  - from: on-boarding
    to: publish-new-employee
```

**Step 2: Create Chat service workflow**

```yaml
# examples/marketplace/chat-new-member.yaml
id: chat-new-member
name: "Chat New Channel Member"
description: "Adds new employees to chat channels when they join"
version: "1.0.0"

nodes:
  - id: start
    type: StartNode
    name: "New Employee Trigger"
    trigger:
      type: event
      criteria:
        event_name: new_employee
        domain: hr
      input_map:
        employee_name: "{{.event.name}}"
        employee_id: "{{.event.employee_id}}"

  - id: new-channel-member
    type: ActionNode
    name: "Add to Channel"
    parameters:
      message: "Adding {{.input.employee_name}} ({{.input.employee_id}}) to general channel"

connections:
  - from: start
    to: new-channel-member
```

**Step 3: Commit**

```bash
mkdir -p examples/marketplace
git add examples/marketplace/
git commit -m "docs: add marketplace workflow examples (HR → Chat)"
```

---

## Task 6: Wire DSL Registry as WorkflowMatcher in Engine

**Files:**
- Modify: `cmd/engine/main.go`

**Step 1: Replace InMemoryWorkflowRepo with DSL registry**

The engine currently uses `InMemoryWorkflowRepo`. Modify to use DSL `WorkflowRegistry` as `WorkflowMatcher`.

```go
// In main.go, around EventRouter creation (~line 180-190):
// Instead of:
//   eventRouter := scheduler.NewEventRouter(js, workflowRepo, eventStore, eventBus, ...)
// Change workflowRepo to use the DSL registry that implements WorkflowMatcher
```

Note: The DSL registry needs to be shared between workflow-api and engine, or engine needs its own DSL registry instance that loads from MongoDB.

**Step 2: Add DSL registry initialization to engine**

```go
// Add to imports:
import "github.com/cheriehsieh/orchestration/internal/dsl"

// In main(), add DSL registry:
dslStore, err := dsl.NewWorkflowStoreFromConfig("mongodb", db)
if err != nil {
	logger.Error("failed to create DSL store", slog.Any("error", err))
	os.Exit(1)
}
dslRegistry := dsl.NewWorkflowRegistry(dsl.WithStore(dslStore))

// Use dslRegistry as WorkflowMatcher for EventRouter
eventRouter := scheduler.NewEventRouter(
	js,
	dslRegistry, // implements WorkflowMatcher
	eventStore,
	eventBus,
	scheduler.WithEventRouterLogger(logger),
)
```

**Step 3: Build to verify**

Run: `go build ./cmd/engine/...`
Expected: SUCCESS

**Step 4: Commit**

```bash
git add cmd/engine/main.go
git commit -m "feat(engine): use DSL registry as WorkflowMatcher for event routing"
```

---

## Task 7: End-to-End Manual Verification

**Prerequisites:**
```bash
# Start infrastructure
docker-compose up -d

# Terminal 1: Start workflow API
go run ./cmd/workflow-api/main.go

# Terminal 2: Start engine
go run ./cmd/engine/main.go
```

**Step 1: Deploy HR workflow**

```bash
curl -X POST http://localhost:8083/api/workflows \
  -H "Content-Type: text/x-yaml" \
  --data-binary @examples/marketplace/hr-onboarding.yaml
```
Expected: `{"id":"hr-onboarding","message":"workflow created"}`

**Step 2: Verify event registered**

```bash
curl http://localhost:8083/api/events/hr/new_employee | jq
```
Expected: Event definition with name, domain, schema

**Step 3: Deploy Chat workflow**

```bash
curl -X POST http://localhost:8083/api/workflows \
  -H "Content-Type: text/x-yaml" \
  --data-binary @examples/marketplace/chat-new-member.yaml
```
Expected: `{"id":"chat-new-member","message":"workflow created"}`

**Step 4: Trigger HR workflow**

```bash
curl -X POST http://localhost:8083/api/workflows/hr-onboarding/execute \
  -H "Content-Type: application/json" \
  -d '{"input": {"name": "Alice Smith", "employee_id": "EMP-001"}}'
```
Expected: Execution started response

**Step 5: Check engine logs**

Look for:
- `"published marketplace event"` - HR workflow published
- `"starting workflow from marketplace event"` - Chat triggered
- ActionNode execution logs

**Step 6: Final commit**

```bash
git commit --allow-empty -m "chore: verified end-to-end marketplace event flow"
```

---

## Summary

| Task | Description | Test Command |
|------|-------------|--------------|
| 1 | Add EventRegistry to handler Create | `go test ./internal/dsl/api/... -run Create_RegistersEvents` |
| 2 | Add EventRegistry to handler Update | `go test ./internal/dsl/api/... -run Update_RegistersEvents` |
| 3 | Implement FindByEventTrigger | `go test ./internal/dsl/... -run FindByEventTrigger` |
| 4 | Wire in workflow-api | `go build ./cmd/workflow-api/...` |
| 5 | Example YAML files | N/A |
| 6 | Wire in engine | `go build ./cmd/engine/...` |
| 7 | E2E verification | Manual |
