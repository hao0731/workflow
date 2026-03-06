package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"log/slog"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	workflowapi "github.com/cheriehsieh/orchestration/internal/api"
	"github.com/cheriehsieh/orchestration/internal/dsl"
	"github.com/cheriehsieh/orchestration/internal/engine"
	"github.com/cheriehsieh/orchestration/internal/marketplace"
	"github.com/cheriehsieh/orchestration/internal/messaging"
)

// mockEventBus captures published events for testing.
type mockEventBus struct {
	events []cloudevents.Event
}

func (m *mockEventBus) Publish(_ context.Context, event cloudevents.Event) error {
	m.events = append(m.events, event)
	return nil
}

type mockDedupEventStore struct {
	keys map[string]time.Duration
}

func newMockDedupEventStore() *mockDedupEventStore {
	return &mockDedupEventStore{
		keys: make(map[string]time.Duration),
	}
}

func (s *mockDedupEventStore) Append(_ context.Context, _ cloudevents.Event) error {
	return nil
}

func (s *mockDedupEventStore) GetBySubject(_ context.Context, _ string) ([]cloudevents.Event, error) {
	return nil, nil
}

func (s *mockDedupEventStore) GetEventsByExecution(_ context.Context, _ string, _ *time.Time) ([]cloudevents.Event, error) {
	return nil, nil
}

func (s *mockDedupEventStore) ExistsByDedupKey(_ context.Context, dedupKey string) (bool, error) {
	_, ok := s.keys[dedupKey]
	return ok, nil
}

func (s *mockDedupEventStore) SaveDedupRecord(_ context.Context, dedupKey string, ttl time.Duration) error {
	s.keys[dedupKey] = ttl
	return nil
}

type recordAwareTestStore struct {
	base    *dsl.InMemoryWorkflowStore
	records map[string]*dsl.WorkflowRecord
}

func newRecordAwareTestStore() *recordAwareTestStore {
	return &recordAwareTestStore{
		base:    dsl.NewInMemoryWorkflowStore(),
		records: make(map[string]*dsl.WorkflowRecord),
	}
}

func (s *recordAwareTestStore) Register(ctx context.Context, wf *engine.Workflow, source []byte) error {
	return s.base.Register(ctx, wf, source)
}

func (s *recordAwareTestStore) RegisterRecord(ctx context.Context, record *dsl.WorkflowRecord) error {
	if err := s.base.Register(ctx, record.Workflow, record.Source); err != nil {
		return err
	}
	s.records[record.ID] = record
	return nil
}

func (s *recordAwareTestStore) GetByID(ctx context.Context, id string) (*engine.Workflow, error) {
	return s.base.GetByID(ctx, id)
}

func (s *recordAwareTestStore) GetRecord(_ context.Context, id string) (*dsl.WorkflowRecord, error) {
	record, ok := s.records[id]
	if !ok {
		return nil, dsl.ErrWorkflowNotFound
	}
	return record, nil
}

func (s *recordAwareTestStore) GetSource(ctx context.Context, id string) ([]byte, error) {
	return s.base.GetSource(ctx, id)
}

func (s *recordAwareTestStore) List(ctx context.Context) ([]string, error) {
	return s.base.List(ctx)
}

func (s *recordAwareTestStore) Delete(ctx context.Context, id string) error {
	delete(s.records, id)
	return s.base.Delete(ctx, id)
}

func setupTestHandler() (*WorkflowHandler, *echo.Echo) {
	registry := dsl.NewWorkflowRegistry()
	logger := slog.Default()
	handler := NewWorkflowHandler(registry, logger)

	e := echo.New()
	handler.RegisterRoutes(e.Group(""))

	return handler, e
}

func setupRecordAwareTestHandler() (*WorkflowHandler, *echo.Echo) {
	registry := dsl.NewWorkflowRegistry(dsl.WithStore(newRecordAwareTestStore()))
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

func TestWorkflowHandler_GetSource_ReturnsStoredDefinitionMetadata(t *testing.T) {
	_, e := setupRecordAwareTestHandler()

	body := []byte(`
id: source-workflow
name: Source Workflow
description: Stored metadata should round-trip
version: 1.2.3
nodes:
  - id: start
    type: StartNode
    trigger:
      type: event
      criteria:
        event_name: employee.created
        domain: hr
connections: []
events:
  - name: employee.created
    domain: hr
    description: employee created
`)

	createReq := httptest.NewRequest(http.MethodPost, "/workflows", bytes.NewReader(body))
	e.ServeHTTP(httptest.NewRecorder(), createReq)

	getReq := httptest.NewRequest(http.MethodGet, "/workflows/source-workflow/source", nil)
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, getReq)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp WorkflowSourceResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "source-workflow", resp.ID)
	assert.Equal(t, "Source Workflow", resp.Name)
	assert.Equal(t, "Stored metadata should round-trip", resp.Description)
	assert.Equal(t, "1.2.3", resp.Version)
	assert.Contains(t, resp.Source, "name: Source Workflow")
	assert.True(t, resp.Stats.HasEventTrigger)
	assert.Equal(t, "employee.created@hr", resp.Stats.TriggerEvent)
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
	nodes, ok := wf["nodes"].([]any)
	require.True(t, ok, "nodes should be []any")
	assert.Len(t, nodes, 2)
}

func TestWorkflowHandler_Create_PersistsDefinitionMetadata(t *testing.T) {
	handler, e := setupRecordAwareTestHandler()

	body := []byte(`
id: create-metadata
name: Create Metadata
description: metadata should be persisted on create
version: 2.0.0
nodes:
  - id: start
    type: StartNode
connections: []
events:
  - name: workflow.created
    domain: testing
    description: workflow created
`)

	req := httptest.NewRequest(http.MethodPost, "/workflows", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code)

	record, err := handler.registry.GetRecord(context.Background(), "create-metadata")
	require.NoError(t, err)
	require.NotNil(t, record)
	assert.Equal(t, "Create Metadata", record.Name)
	assert.Equal(t, "metadata should be persisted on create", record.Description)
	assert.Equal(t, "2.0.0", record.Version)
	require.Len(t, record.Events, 1)
	assert.Equal(t, "workflow.created", record.Events[0].Name)
}

func TestWorkflowHandler_Update_PersistsDefinitionMetadata(t *testing.T) {
	handler, e := setupRecordAwareTestHandler()

	createBody := []byte(`
id: update-metadata
name: Update Metadata
description: original description
version: 1.0.0
nodes:
  - id: start
    type: StartNode
connections: []
`)
	createReq := httptest.NewRequest(http.MethodPost, "/workflows", bytes.NewReader(createBody))
	e.ServeHTTP(httptest.NewRecorder(), createReq)

	updateBody := []byte(`
id: update-metadata
name: Update Metadata v2
description: updated description
version: 2.0.0
nodes:
  - id: start
    type: StartNode
connections: []
events:
  - name: workflow.updated
    domain: testing
    description: workflow updated
`)
	updateReq := httptest.NewRequest(http.MethodPut, "/workflows/update-metadata", bytes.NewReader(updateBody))
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, updateReq)

	require.Equal(t, http.StatusOK, rec.Code)

	record, err := handler.registry.GetRecord(context.Background(), "update-metadata")
	require.NoError(t, err)
	require.NotNil(t, record)
	assert.Equal(t, "Update Metadata v2", record.Name)
	assert.Equal(t, "updated description", record.Description)
	assert.Equal(t, "2.0.0", record.Version)
	require.Len(t, record.Events, 1)
	assert.Equal(t, "workflow.updated", record.Events[0].Name)
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
	assert.Equal(t, messaging.EventTypeRuntimeExecutionStartedV1, eventBus.events[0].Type())
	assert.Equal(t, "exec-test", eventBus.events[0].Extensions()["workflowid"])
	assert.Equal(t, resp["execution_id"], eventBus.events[0].Extensions()["executionid"])
	assert.Equal(t, "workflow-api/rest", eventBus.events[0].Extensions()["producer"])
}

func TestWorkflowHandler_Execute_IdempotentDedupReturnsExistingExecutionID(t *testing.T) {
	registry := dsl.NewWorkflowRegistry()
	logger := slog.Default()
	eventBus := &mockEventBus{}
	dedupStore := newMockDedupEventStore()
	handler := NewWorkflowHandler(registry, logger, WithEventBus(eventBus), WithEventStore(dedupStore))

	e := echo.New()
	handler.RegisterRoutes(e.Group(""))

	wfBody := []byte(`
id: exec-idempotent
nodes:
  - id: start
    type: StartNode
connections: []
`)
	createReq := httptest.NewRequest(http.MethodPost, "/workflows", bytes.NewReader(wfBody))
	e.ServeHTTP(httptest.NewRecorder(), createReq)

	execBody := []byte(`{"input": {"employeeId": "emp-1001"}}`)
	req1 := httptest.NewRequest(http.MethodPost, "/workflows/exec-idempotent/execute", bytes.NewReader(execBody))
	req1.Header.Set("Content-Type", "application/json")
	req1.Header.Set("Idempotency-Key", "employee:emp-1001:onboard:v1")
	rec1 := httptest.NewRecorder()
	e.ServeHTTP(rec1, req1)
	require.Equal(t, http.StatusCreated, rec1.Code)

	req2 := httptest.NewRequest(http.MethodPost, "/workflows/exec-idempotent/execute", bytes.NewReader(execBody))
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Idempotency-Key", "employee:emp-1001:onboard:v1")
	rec2 := httptest.NewRecorder()
	e.ServeHTTP(rec2, req2)
	require.Equal(t, http.StatusCreated, rec2.Code)

	var resp1 map[string]any
	var resp2 map[string]any
	require.NoError(t, json.Unmarshal(rec1.Body.Bytes(), &resp1))
	require.NoError(t, json.Unmarshal(rec2.Body.Bytes(), &resp2))

	assert.Equal(t, resp1["execution_id"], resp2["execution_id"])
	require.Len(t, eventBus.events, 1)
	assert.Equal(t, "employee:emp-1001:onboard:v1", eventBus.events[0].Extensions()["idempotencykey"])
}

func TestWorkflowHandler_StartExecution_IdempotentCommandReturnsExistingExecutionID(t *testing.T) {
	registry := dsl.NewWorkflowRegistry()
	logger := slog.Default()
	eventBus := &mockEventBus{}
	dedupStore := newMockDedupEventStore()
	handler := NewWorkflowHandler(registry, logger, WithEventBus(eventBus), WithEventStore(dedupStore))

	e := echo.New()
	handler.RegisterRoutes(e.Group(""))

	wfBody := []byte(`
id: exec-command-idempotent
nodes:
  - id: start
    type: StartNode
connections: []
`)
	createReq := httptest.NewRequest(http.MethodPost, "/workflows", bytes.NewReader(wfBody))
	e.ServeHTTP(httptest.NewRecorder(), createReq)

	command := workflowapi.ExecutionStartCommand{
		WorkflowID:     "exec-command-idempotent",
		Producer:       "hr-system",
		IdempotencyKey: "cmd:hr-system:employee:emp-1001:onboard:v1",
		Input:          map[string]any{"employeeId": "emp-1001"},
	}

	first, err := handler.StartExecution(context.Background(), command)
	require.NoError(t, err)

	second, err := handler.StartExecution(context.Background(), command)
	require.NoError(t, err)

	assert.Equal(t, first.ExecutionID, second.ExecutionID)
	require.Len(t, eventBus.events, 1)
	assert.Equal(t, "cmd:hr-system:employee:emp-1001:onboard:v1", eventBus.events[0].Extensions()["idempotencykey"])
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
