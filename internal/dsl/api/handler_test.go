package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"log/slog"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cheriehsieh/orchestration/internal/dsl"
	"github.com/cheriehsieh/orchestration/internal/marketplace"
)

// mockEventBus captures published events for testing.
type mockEventBus struct {
	events []cloudevents.Event
}

func (m *mockEventBus) Publish(_ context.Context, event cloudevents.Event) error {
	m.events = append(m.events, event)
	return nil
}

func setupTestHandler() (*WorkflowHandler, *echo.Echo) {
	registry := dsl.NewWorkflowRegistry()
	logger := slog.Default()
	handler := NewWorkflowHandler(registry, logger)

	e := echo.New()
	handler.RegisterRoutes(e.Group(""))

	return handler, e
}

// Test: Create workflow successfully
func TestWorkflowHandler_Create_Success(t *testing.T) {
	_, e := setupTestHandler()

	body := []byte(`
id: test-workflow
nodes:
  - id: start
    type: StartNode
  - id: action
    type: http-request@v1
connections:
  - from: start
    to: action
`)

	req := httptest.NewRequest(http.MethodPost, "/workflows", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/x-yaml")
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "test-workflow", resp["id"])
	assert.Equal(t, "workflow created", resp["message"])
}

// Test: Create with dryrun validates without saving
func TestWorkflowHandler_Create_Dryrun(t *testing.T) {
	handler, e := setupTestHandler()

	body := []byte(`
id: dryrun-workflow
nodes:
  - id: start
    type: StartNode
connections: []
`)

	req := httptest.NewRequest(http.MethodPost, "/workflows?dryrun=true", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/x-yaml")
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, true, resp["valid"])
	assert.Equal(t, "dryrun-workflow", resp["id"])

	// Verify workflow was NOT saved
	ids := handler.registry.ListWorkflows()
	assert.NotContains(t, ids, "dryrun-workflow")
}

// Test: Create with invalid YAML returns error
func TestWorkflowHandler_Create_InvalidYAML(t *testing.T) {
	_, e := setupTestHandler()

	body := []byte(`invalid: yaml: {{broken`)

	req := httptest.NewRequest(http.MethodPost, "/workflows", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

// Test: Create with validation error returns error
func TestWorkflowHandler_Create_ValidationError(t *testing.T) {
	_, e := setupTestHandler()

	body := []byte(`
id: no-start-node
nodes:
  - id: action
    type: http-request@v1
connections: []
`)

	req := httptest.NewRequest(http.MethodPost, "/workflows", bytes.NewReader(body))
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)

	var resp map[string]string
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Contains(t, resp["error"], "StartNode")
}

// Test: List workflows
func TestWorkflowHandler_List(t *testing.T) {
	handler, e := setupTestHandler()

	// Create two workflows first
	wf1 := []byte(`
id: workflow-1
nodes:
  - id: start
    type: StartNode
connections: []
`)
	wf2 := []byte(`
id: workflow-2
nodes:
  - id: start
    type: StartNode
connections: []
`)

	req1 := httptest.NewRequest(http.MethodPost, "/workflows", bytes.NewReader(wf1))
	e.ServeHTTP(httptest.NewRecorder(), req1)

	req2 := httptest.NewRequest(http.MethodPost, "/workflows", bytes.NewReader(wf2))
	e.ServeHTTP(httptest.NewRecorder(), req2)

	// List
	req := httptest.NewRequest(http.MethodGet, "/workflows", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	var workflows []map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &workflows))
	assert.Len(t, workflows, 2)

	_ = handler // suppress unused warning
}

// Test: Get workflow by ID
func TestWorkflowHandler_Get(t *testing.T) {
	_, e := setupTestHandler()

	// Create workflow first
	body := []byte(`
id: get-me
nodes:
  - id: start
    type: StartNode
connections: []
`)
	req := httptest.NewRequest(http.MethodPost, "/workflows", bytes.NewReader(body))
	e.ServeHTTP(httptest.NewRecorder(), req)

	// Get
	getReq := httptest.NewRequest(http.MethodGet, "/workflows/get-me", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, getReq)

	assert.Equal(t, http.StatusOK, rec.Code)

	var wf map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &wf))
	assert.Equal(t, "get-me", wf["id"])
}

// Test: Get non-existent workflow returns 404
func TestWorkflowHandler_Get_NotFound(t *testing.T) {
	_, e := setupTestHandler()

	req := httptest.NewRequest(http.MethodGet, "/workflows/nonexistent", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// Test: Delete workflow
func TestWorkflowHandler_Delete(t *testing.T) {
	handler, e := setupTestHandler()

	// Create workflow first
	body := []byte(`
id: delete-me
nodes:
  - id: start
    type: StartNode
connections: []
`)
	req := httptest.NewRequest(http.MethodPost, "/workflows", bytes.NewReader(body))
	e.ServeHTTP(httptest.NewRecorder(), req)

	// Verify exists
	assert.Contains(t, handler.registry.ListWorkflows(), "delete-me")

	// Delete
	delReq := httptest.NewRequest(http.MethodDelete, "/workflows/delete-me", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, delReq)

	assert.Equal(t, http.StatusOK, rec.Code)

	// Verify deleted
	assert.NotContains(t, handler.registry.ListWorkflows(), "delete-me")
}

// Test: Update workflow
func TestWorkflowHandler_Update(t *testing.T) {
	_, e := setupTestHandler()

	// Create workflow first
	createBody := []byte(`
id: update-me
nodes:
  - id: start
    type: StartNode
    name: Original
connections: []
`)
	createReq := httptest.NewRequest(http.MethodPost, "/workflows", bytes.NewReader(createBody))
	e.ServeHTTP(httptest.NewRecorder(), createReq)

	// Update
	updateBody := []byte(`
id: update-me
nodes:
  - id: start
    type: StartNode
    name: Updated
  - id: action
    type: http-request@v1
connections:
  - from: start
    to: action
`)
	updateReq := httptest.NewRequest(http.MethodPut, "/workflows/update-me", bytes.NewReader(updateBody))
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, updateReq)

	assert.Equal(t, http.StatusOK, rec.Code)

	// Verify update
	getReq := httptest.NewRequest(http.MethodGet, "/workflows/update-me", nil)
	getRec := httptest.NewRecorder()
	e.ServeHTTP(getRec, getReq)

	var wf map[string]any
	require.NoError(t, json.Unmarshal(getRec.Body.Bytes(), &wf))
	nodes := wf["nodes"].([]any)
	assert.Len(t, nodes, 2)
}

// Test: Execute workflow successfully
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

// Test: Execute workflow not found
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

// Test: Execute without event bus configured
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

// Test: Create workflow registers events to marketplace
func TestWorkflowHandler_Create_RegistersEventsToMarketplace(t *testing.T) {
	mockEventRegistry := marketplace.NewInMemoryEventRegistry()
	registry := dsl.NewWorkflowRegistry()
	logger := slog.Default()
	handler := NewWorkflowHandler(registry, logger, WithEventRegistry(mockEventRegistry))

	e := echo.New()
	handler.RegisterRoutes(e.Group(""))

	// YAML with events section
	body := []byte(`
id: test-publisher
nodes:
  - id: start
    type: StartNode
connections: []
events:
  - name: test_event
    domain: testing
    description: A test event
`)
	req := httptest.NewRequest(http.MethodPost, "/workflows", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/x-yaml")
	rec := httptest.NewRecorder()

	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code)

	// Verify event was registered
	event, err := mockEventRegistry.Get(context.Background(), "testing", "test_event")
	require.NoError(t, err)
	assert.Equal(t, "test_event", event.Name)
	assert.Equal(t, "testing", event.Domain)
	assert.Equal(t, "A test event", event.Description)
	assert.Equal(t, "test-publisher", event.Owner)
}

// Test: Update workflow registers events to marketplace
func TestWorkflowHandler_Update_RegistersEventsToMarketplace(t *testing.T) {
	mockEventRegistry := marketplace.NewInMemoryEventRegistry()
	registry := dsl.NewWorkflowRegistry()
	logger := slog.Default()
	handler := NewWorkflowHandler(registry, logger, WithEventRegistry(mockEventRegistry))

	e := echo.New()
	handler.RegisterRoutes(e.Group(""))

	// First create workflow without events
	createBody := []byte(`
id: test-publisher
nodes:
  - id: start
    type: StartNode
connections: []
`)
	createReq := httptest.NewRequest(http.MethodPost, "/workflows", bytes.NewReader(createBody))
	e.ServeHTTP(httptest.NewRecorder(), createReq)

	// Now update with events
	updateBody := []byte(`
id: test-publisher
nodes:
  - id: start
    type: StartNode
connections: []
events:
  - name: new_event
    domain: testing
`)
	updateReq := httptest.NewRequest(http.MethodPut, "/workflows/test-publisher", bytes.NewReader(updateBody))
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, updateReq)

	require.Equal(t, http.StatusOK, rec.Code)

	// Verify event was registered
	event, err := mockEventRegistry.Get(context.Background(), "testing", "new_event")
	require.NoError(t, err)
	assert.Equal(t, "new_event", event.Name)
	assert.Equal(t, "testing", event.Domain)
}
