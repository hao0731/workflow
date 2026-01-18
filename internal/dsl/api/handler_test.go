package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"log/slog"

	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cheriehsieh/orchestration/internal/dsl"
)

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
