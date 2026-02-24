package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cheriehsieh/orchestration/internal/eventstore"
)

// mockEventStore is a test double for eventstore.EventStore.
type mockEventStore struct {
	events map[string][]cloudevents.Event
}

func newMockEventStore() *mockEventStore {
	return &mockEventStore{
		events: make(map[string][]cloudevents.Event),
	}
}

func (m *mockEventStore) Append(ctx context.Context, event cloudevents.Event) error {
	subject := event.Subject()
	m.events[subject] = append(m.events[subject], event)
	return nil
}

func (m *mockEventStore) GetBySubject(ctx context.Context, subject string) ([]cloudevents.Event, error) {
	return m.events[subject], nil
}

func (m *mockEventStore) GetExecutionsByWorkflow(ctx context.Context, workflowID string) ([]eventstore.ExecutionSummary, error) {
	return nil, nil
}

func (m *mockEventStore) GetEventsByExecution(ctx context.Context, executionID string, since *time.Time) ([]cloudevents.Event, error) {
	events := m.events[executionID]
	if since == nil {
		return events, nil
	}

	var filtered []cloudevents.Event
	for _, evt := range events {
		if evt.Time().After(*since) {
			filtered = append(filtered, evt)
		}
	}
	return filtered, nil
}

// mockExecutionStoreForAPI is a test double for eventstore.ExecutionStore.
type mockExecutionStoreForAPI struct {
	executions map[string]*eventstore.Execution
}

func newMockExecutionStoreForAPI() *mockExecutionStoreForAPI {
	return &mockExecutionStoreForAPI{
		executions: make(map[string]*eventstore.Execution),
	}
}

func (m *mockExecutionStoreForAPI) Create(_ context.Context, exec *eventstore.Execution) error {
	m.executions[exec.ID] = exec
	return nil
}

func (m *mockExecutionStoreForAPI) GetByID(_ context.Context, id string) (*eventstore.Execution, error) {
	return m.executions[id], nil
}

func (m *mockExecutionStoreForAPI) GetChildren(_ context.Context, parentID string) ([]*eventstore.Execution, error) {
	var children []*eventstore.Execution
	for _, exec := range m.executions {
		if exec.ParentExecutionID == parentID {
			children = append(children, exec)
		}
	}
	return children, nil
}

func (m *mockExecutionStoreForAPI) AddChildExecution(_ context.Context, parentID, childID string) error {
	if parent, ok := m.executions[parentID]; ok {
		parent.ChildExecutionIDs = append(parent.ChildExecutionIDs, childID)
	}
	return nil
}

func (m *mockExecutionStoreForAPI) UpdateStatus(_ context.Context, id, status string) error {
	if exec, ok := m.executions[id]; ok {
		exec.Status = status
	}
	return nil
}

func createTestEvent(id, eventType, subject string, data map[string]any, eventTime time.Time) cloudevents.Event {
	evt := cloudevents.NewEvent()
	evt.SetID(id)
	evt.SetType(eventType)
	evt.SetSubject(subject)
	evt.SetSource("test")
	evt.SetTime(eventTime)
	_ = evt.SetData(cloudevents.ApplicationJSON, data)
	return evt
}

func TestGetExecution_Success(t *testing.T) {
	// Setup
	store := newMockEventStore()
	execID := "exec-123"
	workflowID := "wf-456"
	startTime := time.Now().Add(-10 * time.Minute)
	endTime := time.Now().Add(-5 * time.Minute)

	// Add test events
	store.events[execID] = []cloudevents.Event{
		createTestEvent("evt-1", "orchestration.execution.started", execID, map[string]any{"workflow_id": workflowID}, startTime),
		createTestEvent("evt-2", "orchestration.node.started", execID, map[string]any{"node_id": "node-1"}, startTime.Add(time.Second)),
		createTestEvent("evt-3", "orchestration.node.completed", execID, map[string]any{"node_id": "node-1"}, startTime.Add(2*time.Second)),
		createTestEvent("evt-4", "orchestration.execution.completed", execID, map[string]any{}, endTime),
	}

	handler := NewExecutionHandler(store)

	// Create echo context
	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/executions/"+execID, nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues(execID)

	// Execute
	err := handler.GetExecution(c)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"id":"exec-123"`)
	assert.Contains(t, rec.Body.String(), `"workflow_id":"wf-456"`)
	assert.Contains(t, rec.Body.String(), `"status":"completed"`)
	assert.Contains(t, rec.Body.String(), `"duration_ms"`)
}

func TestGetExecution_Running(t *testing.T) {
	// Setup
	store := newMockEventStore()
	execID := "exec-running"
	workflowID := "wf-789"
	startTime := time.Now().Add(-2 * time.Minute)

	// Add only started event (execution still running)
	store.events[execID] = []cloudevents.Event{
		createTestEvent("evt-1", "orchestration.execution.started", execID, map[string]any{"workflow_id": workflowID}, startTime),
	}

	handler := NewExecutionHandler(store)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/executions/"+execID, nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues(execID)

	err := handler.GetExecution(c)

	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"status":"running"`)
	assert.NotContains(t, rec.Body.String(), `"duration_ms"`)
}

func TestGetExecution_Failed(t *testing.T) {
	// Setup
	store := newMockEventStore()
	execID := "exec-failed"
	workflowID := "wf-fail"
	startTime := time.Now().Add(-5 * time.Minute)
	endTime := time.Now().Add(-3 * time.Minute)

	store.events[execID] = []cloudevents.Event{
		createTestEvent("evt-1", "orchestration.execution.started", execID, map[string]any{"workflow_id": workflowID}, startTime),
		createTestEvent("evt-2", "orchestration.execution.failed", execID, map[string]any{"error": "something went wrong"}, endTime),
	}

	handler := NewExecutionHandler(store)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/executions/"+execID, nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues(execID)

	err := handler.GetExecution(c)

	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"status":"failed"`)
}

func TestGetExecution_NotFound(t *testing.T) {
	store := newMockEventStore()
	handler := NewExecutionHandler(store)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/executions/nonexistent", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("nonexistent")

	err := handler.GetExecution(c)

	require.NoError(t, err)
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Body.String(), `"error":"execution not found"`)
}

func TestGetEvents_Success(t *testing.T) {
	store := newMockEventStore()
	execID := "exec-events"
	startTime := time.Now().Add(-10 * time.Minute)

	store.events[execID] = []cloudevents.Event{
		createTestEvent("evt-1", "orchestration.execution.started", execID, map[string]any{"workflow_id": "wf-1"}, startTime),
		createTestEvent("evt-2", "orchestration.node.started", execID, map[string]any{"node_id": "node-a"}, startTime.Add(time.Second)),
		createTestEvent("evt-3", "orchestration.node.completed", execID, map[string]any{"node_id": "node-a"}, startTime.Add(2*time.Second)),
	}

	handler := NewExecutionHandler(store)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/executions/"+execID+"/events", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues(execID)

	err := handler.GetEvents(c)

	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"id":"evt-1"`)
	assert.Contains(t, rec.Body.String(), `"id":"evt-2"`)
	assert.Contains(t, rec.Body.String(), `"id":"evt-3"`)
	assert.Contains(t, rec.Body.String(), `"node_id":"node-a"`)
}

func TestGetEvents_WithSinceFilter(t *testing.T) {
	store := newMockEventStore()
	execID := "exec-filtered"
	baseTime := time.Now().UTC().Add(-10 * time.Minute)

	store.events[execID] = []cloudevents.Event{
		createTestEvent("evt-1", "orchestration.execution.started", execID, map[string]any{}, baseTime),
		createTestEvent("evt-2", "orchestration.node.started", execID, map[string]any{}, baseTime.Add(time.Minute)),
		createTestEvent("evt-3", "orchestration.node.completed", execID, map[string]any{}, baseTime.Add(2*time.Minute)),
	}

	handler := NewExecutionHandler(store)

	e := echo.New()
	sinceTime := baseTime.Add(30 * time.Second).Format(time.RFC3339Nano)
	req := httptest.NewRequest(http.MethodGet, "/api/executions/"+execID+"/events", nil)
	q := req.URL.Query()
	q.Add("since", sinceTime)
	req.URL.RawQuery = q.Encode()
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues(execID)

	err := handler.GetEvents(c)

	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	// evt-1 should be filtered out (before since time)
	assert.NotContains(t, rec.Body.String(), `"id":"evt-1"`)
	// evt-2 and evt-3 should be included (after since time)
	assert.Contains(t, rec.Body.String(), `"id":"evt-2"`)
	assert.Contains(t, rec.Body.String(), `"id":"evt-3"`)
}

func TestGetEvents_EmptyResult(t *testing.T) {
	store := newMockEventStore()
	handler := NewExecutionHandler(store)

	e := echo.New()
	req := httptest.NewRequest(http.MethodGet, "/api/executions/nonexistent/events", nil)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id")
	c.SetParamValues("nonexistent")

	err := handler.GetEvents(c)

	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "[]\n", rec.Body.String())
}

func TestRegisterRoutes(t *testing.T) {
	store := newMockEventStore()
	handler := NewExecutionHandler(store)

	e := echo.New()
	g := e.Group("/api")
	handler.RegisterRoutes(g)

	routes := e.Routes()
	var foundGetExecution, foundGetEvents bool
	for _, r := range routes {
		if r.Path == "/api/executions/:id" && r.Method == http.MethodGet {
			foundGetExecution = true
		}
		if r.Path == "/api/executions/:id/events" && r.Method == http.MethodGet {
			foundGetEvents = true
		}
	}

	assert.True(t, foundGetExecution, "GET /api/executions/:id route should be registered")
	assert.True(t, foundGetEvents, "GET /api/executions/:id/events route should be registered")
}

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
