# Workflow UI Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build a single-page workflow debugging dashboard with DAG visualization, detail panels, and real-time execution streaming.

**Architecture:** React SPA with collapsible panels (left/right/bottom), React Flow + Dagre for graph layout, WebSocket for real-time updates. Backend extends existing Echo API with execution and streaming endpoints.

**Tech Stack:** React 19, TypeScript, Vite, @xyflow/react, @dagrejs/dagre, Echo v4, gorilla/websocket

---

## Phase 1: Backend API Extensions

### Task 1: Add Execution API Models

**Files:**
- Create: `internal/api/models.go`

**Step 1: Create the models file**

```go
package api

import "time"

// ExecutionResponse represents an execution for API responses.
type ExecutionResponse struct {
	ID         string     `json:"id"`
	WorkflowID string     `json:"workflow_id"`
	Status     string     `json:"status"`
	StartedAt  time.Time  `json:"started_at"`
	EndedAt    *time.Time `json:"ended_at,omitempty"`
	DurationMs *float64   `json:"duration_ms,omitempty"`
}

// ExecutionEventResponse represents an event in the execution timeline.
type ExecutionEventResponse struct {
	ID        string         `json:"id"`
	Type      string         `json:"type"`
	NodeID    string         `json:"node_id,omitempty"`
	Data      map[string]any `json:"data,omitempty"`
	Timestamp time.Time      `json:"timestamp"`
}

// WorkflowDetailResponse extends workflow with metadata and stats.
type WorkflowDetailResponse struct {
	ID          string             `json:"id"`
	Name        string             `json:"name"`
	Description string             `json:"description"`
	Version     string             `json:"version"`
	Nodes       []NodeResponse     `json:"nodes"`
	Connections []ConnectionResponse `json:"connections"`
	Stats       WorkflowStats      `json:"stats"`
}

// NodeResponse represents a node in API responses.
type NodeResponse struct {
	ID         string         `json:"id"`
	Type       string         `json:"type"`
	Name       string         `json:"name"`
	Parameters map[string]any `json:"parameters,omitempty"`
}

// ConnectionResponse represents a connection in API responses.
type ConnectionResponse struct {
	FromNode string `json:"from_node"`
	FromPort string `json:"from_port,omitempty"`
	ToNode   string `json:"to_node"`
	ToPort   string `json:"to_port,omitempty"`
}

// WorkflowStats provides workflow statistics.
type WorkflowStats struct {
	NodeCount       int    `json:"node_count"`
	ConnectionCount int    `json:"connection_count"`
	HasEventTrigger bool   `json:"has_event_trigger"`
	TriggerEvent    string `json:"trigger_event,omitempty"`
}

// StreamMessage is sent over WebSocket for real-time updates.
type StreamMessage struct {
	Type      string                  `json:"type"` // "event" | "heartbeat" | "error"
	Event     *ExecutionEventResponse `json:"event,omitempty"`
	Timestamp time.Time               `json:"timestamp"`
	Error     string                  `json:"error,omitempty"`
}
```

**Step 2: Verify file compiles**

Run: `cd /Users/cheriehsieh/Program/orchestration && go build ./internal/api/...`
Expected: No errors

**Step 3: Commit**

```bash
git add internal/api/models.go
git commit -m "feat(api): add execution and workflow detail response models"
```

---

### Task 2: Create Execution Handler

**Files:**
- Create: `internal/api/execution_handler.go`
- Modify: `internal/eventstore/eventstore.go` (add interface method)
- Modify: `internal/eventstore/mongo.go` (implement method)

**Step 1: Extend EventStore interface**

Add to `internal/eventstore/eventstore.go` after line 35:

```go
// GetEventsByExecution returns all events for a given execution ID.
GetEventsByExecution(ctx context.Context, executionID string, since *time.Time) ([]cloudevents.Event, error)
```

**Step 2: Implement in MongoEventStore**

Add to `internal/eventstore/mongo.go`:

```go
// GetEventsByExecution returns all events for a given execution ID, optionally filtered by time.
func (s *MongoEventStore) GetEventsByExecution(ctx context.Context, executionID string, since *time.Time) ([]cloudevents.Event, error) {
	filter := map[string]any{
		"subject": executionID,
	}

	if since != nil {
		filter["time"] = map[string]any{
			"$gt": *since,
		}
	}

	opts := options.Find().SetSort(map[string]int{"time": 1})
	cursor, err := s.collection.Find(ctx, filter, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var stored []StoredEvent
	if err := cursor.All(ctx, &stored); err != nil {
		return nil, err
	}

	events := make([]cloudevents.Event, len(stored))
	for i, se := range stored {
		events[i] = se.ToCloudEvent()
	}
	return events, nil
}
```

**Step 3: Add import to mongo.go**

Add to imports in `internal/eventstore/mongo.go`:

```go
"go.mongodb.org/mongo-driver/mongo/options"
```

**Step 4: Create execution handler**

Create `internal/api/execution_handler.go`:

```go
package api

import (
	"net/http"
	"time"

	"github.com/labstack/echo/v4"

	"github.com/cheriehsieh/orchestration/internal/eventstore"
)

// ExecutionHandler handles execution-related API requests.
type ExecutionHandler struct {
	eventStore eventstore.EventStore
}

// NewExecutionHandler creates a new ExecutionHandler.
func NewExecutionHandler(es eventstore.EventStore) *ExecutionHandler {
	return &ExecutionHandler{eventStore: es}
}

// RegisterRoutes registers execution API routes.
func (h *ExecutionHandler) RegisterRoutes(g *echo.Group) {
	g.GET("/executions/:id", h.GetExecution)
	g.GET("/executions/:id/events", h.GetEvents)
}

// GetExecution handles GET /api/executions/:id
func (h *ExecutionHandler) GetExecution(c echo.Context) error {
	execID := c.Param("id")

	events, err := h.eventStore.GetEventsByExecution(c.Request().Context(), execID, nil)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	if len(events) == 0 {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "execution not found"})
	}

	// Derive execution info from events
	var workflowID, status string
	var startedAt time.Time
	var endedAt *time.Time

	for _, evt := range events {
		switch evt.Type() {
		case "orchestration.execution.started":
			var data map[string]any
			_ = evt.DataAs(&data)
			if wid, ok := data["workflow_id"].(string); ok {
				workflowID = wid
			}
			startedAt = evt.Time()
			status = "running"
		case "orchestration.execution.completed":
			status = "completed"
			t := evt.Time()
			endedAt = &t
		case "orchestration.execution.failed":
			status = "failed"
			t := evt.Time()
			endedAt = &t
		}
	}

	resp := ExecutionResponse{
		ID:         execID,
		WorkflowID: workflowID,
		Status:     status,
		StartedAt:  startedAt,
		EndedAt:    endedAt,
	}

	if endedAt != nil {
		dur := endedAt.Sub(startedAt).Seconds() * 1000
		resp.DurationMs = &dur
	}

	return c.JSON(http.StatusOK, resp)
}

// GetEvents handles GET /api/executions/:id/events
func (h *ExecutionHandler) GetEvents(c echo.Context) error {
	execID := c.Param("id")

	var since *time.Time
	if sinceStr := c.QueryParam("since"); sinceStr != "" {
		t, err := time.Parse(time.RFC3339Nano, sinceStr)
		if err == nil {
			since = &t
		}
	}

	events, err := h.eventStore.GetEventsByExecution(c.Request().Context(), execID, since)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	resp := make([]ExecutionEventResponse, len(events))
	for i, evt := range events {
		var data map[string]any
		_ = evt.DataAs(&data)

		nodeID := ""
		if nid, ok := data["node_id"].(string); ok {
			nodeID = nid
		}

		resp[i] = ExecutionEventResponse{
			ID:        evt.ID(),
			Type:      evt.Type(),
			NodeID:    nodeID,
			Data:      data,
			Timestamp: evt.Time(),
		}
	}

	return c.JSON(http.StatusOK, resp)
}
```

**Step 5: Verify compilation**

Run: `cd /Users/cheriehsieh/Program/orchestration && go build ./internal/...`
Expected: No errors

**Step 6: Commit**

```bash
git add internal/eventstore/eventstore.go internal/eventstore/mongo.go internal/api/execution_handler.go
git commit -m "feat(api): add execution handler with GET /executions/:id and /events endpoints"
```

---

### Task 3: Create WebSocket Stream Handler

**Files:**
- Create: `internal/api/stream_handler.go`

**Step 1: Add gorilla/websocket dependency**

Run: `cd /Users/cheriehsieh/Program/orchestration && go get github.com/gorilla/websocket`

**Step 2: Create stream handler**

Create `internal/api/stream_handler.go`:

```go
package api

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"github.com/labstack/echo/v4"

	"github.com/cheriehsieh/orchestration/internal/eventstore"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins for dev
	},
}

// StreamHandler handles WebSocket streaming of execution events.
type StreamHandler struct {
	eventStore eventstore.EventStore
	logger     *slog.Logger
}

// NewStreamHandler creates a new StreamHandler.
func NewStreamHandler(es eventstore.EventStore, logger *slog.Logger) *StreamHandler {
	return &StreamHandler{eventStore: es, logger: logger}
}

// RegisterRoutes registers streaming routes.
func (h *StreamHandler) RegisterRoutes(g *echo.Group) {
	g.GET("/executions/:id/stream", h.Stream)
}

// Stream handles WebSocket connections for real-time event streaming.
func (h *StreamHandler) Stream(c echo.Context) error {
	execID := c.Param("id")

	ws, err := upgrader.Upgrade(c.Response(), c.Request(), nil)
	if err != nil {
		h.logger.Error("websocket upgrade failed", slog.Any("error", err))
		return err
	}
	defer ws.Close()

	h.logger.Info("websocket connected", slog.String("execution_id", execID))

	ctx, cancel := context.WithCancel(c.Request().Context())
	defer cancel()

	// Handle client disconnect
	go func() {
		for {
			if _, _, err := ws.ReadMessage(); err != nil {
				cancel()
				return
			}
		}
	}()

	// Send existing events first
	events, err := h.eventStore.GetEventsByExecution(ctx, execID, nil)
	if err == nil {
		for _, evt := range events {
			var data map[string]any
			_ = evt.DataAs(&data)

			nodeID := ""
			if nid, ok := data["node_id"].(string); ok {
				nodeID = nid
			}

			msg := StreamMessage{
				Type: "event",
				Event: &ExecutionEventResponse{
					ID:        evt.ID(),
					Type:      evt.Type(),
					NodeID:    nodeID,
					Data:      data,
					Timestamp: evt.Time(),
				},
				Timestamp: time.Now(),
			}

			if err := ws.WriteJSON(msg); err != nil {
				h.logger.Error("websocket write failed", slog.Any("error", err))
				return nil
			}
		}
	}

	// Poll for new events (simplified implementation)
	var lastTime *time.Time
	if len(events) > 0 {
		t := events[len(events)-1].Time()
		lastTime = &t
	}

	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	heartbeat := time.NewTicker(30 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil

		case <-heartbeat.C:
			msg := StreamMessage{
				Type:      "heartbeat",
				Timestamp: time.Now(),
			}
			if err := ws.WriteJSON(msg); err != nil {
				return nil
			}

		case <-ticker.C:
			newEvents, err := h.eventStore.GetEventsByExecution(ctx, execID, lastTime)
			if err != nil {
				continue
			}

			for _, evt := range newEvents {
				var data map[string]any
				_ = evt.DataAs(&data)

				nodeID := ""
				if nid, ok := data["node_id"].(string); ok {
					nodeID = nid
				}

				msg := StreamMessage{
					Type: "event",
					Event: &ExecutionEventResponse{
						ID:        evt.ID(),
						Type:      evt.Type(),
						NodeID:    nodeID,
						Data:      data,
						Timestamp: evt.Time(),
					},
					Timestamp: time.Now(),
				}

				if err := ws.WriteJSON(msg); err != nil {
					return nil
				}

				t := evt.Time()
				lastTime = &t
			}
		}
	}
}
```

**Step 3: Verify compilation**

Run: `cd /Users/cheriehsieh/Program/orchestration && go build ./internal/api/...`
Expected: No errors

**Step 4: Commit**

```bash
git add internal/api/stream_handler.go go.mod go.sum
git commit -m "feat(api): add WebSocket stream handler for real-time execution events"
```

---

### Task 4: Enhance Workflow Handler with Stats and Source

**Files:**
- Modify: `internal/dsl/api/handler.go`

**Step 1: Add GetSource endpoint**

Add to `internal/dsl/api/handler.go` after line 41:

```go
g.GET("/workflows/:id/source", h.GetSource)
```

**Step 2: Add WorkflowStats to response**

Replace `toWorkflowResponse` function in `internal/dsl/api/handler.go`:

```go
// toWorkflowResponse converts engine.Workflow to API response.
func toWorkflowResponse(wf *engine.Workflow) WorkflowResponse {
	nodes := make([]NodeResponse, len(wf.Nodes))
	for i, n := range wf.Nodes {
		nodes[i] = NodeResponse{
			ID:   n.ID,
			Type: string(n.Type),
			Name: n.Name,
		}
	}

	connections := make([]ConnectionResponse, len(wf.Connections))
	for i, c := range wf.Connections {
		connections[i] = ConnectionResponse{
			FromNode: c.FromNode,
			FromPort: c.FromPort,
			ToNode:   c.ToNode,
			ToPort:   c.ToPort,
		}
	}

	return WorkflowResponse{
		ID:          wf.ID,
		Nodes:       nodes,
		Connections: connections,
	}
}
```

**Step 3: Add GetSource handler**

Add to `internal/dsl/api/handler.go`:

```go
// WorkflowSourceResponse includes YAML source and metadata.
type WorkflowSourceResponse struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Version     string `json:"version"`
	Source      string `json:"source"`
	Stats       struct {
		NodeCount       int    `json:"node_count"`
		ConnectionCount int    `json:"connection_count"`
		HasEventTrigger bool   `json:"has_event_trigger"`
		TriggerEvent    string `json:"trigger_event,omitempty"`
	} `json:"stats"`
}

// GetSource handles GET /workflows/:id/source
func (h *WorkflowHandler) GetSource(c echo.Context) error {
	id := c.Param("id")

	wf, err := h.registry.GetByID(c.Request().Context(), id)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": err.Error()})
	}

	// Build stats
	hasEventTrigger := false
	triggerEvent := ""
	if start := wf.GetStartNode(); start != nil {
		if eventName, domain, ok := start.GetEventTrigger(); ok {
			hasEventTrigger = true
			triggerEvent = eventName + "@" + domain
		}
	}

	resp := WorkflowSourceResponse{
		ID:          wf.ID,
		Name:        "", // Would need to store this in registry
		Description: "",
		Version:     "",
	}
	resp.Stats.NodeCount = len(wf.Nodes)
	resp.Stats.ConnectionCount = len(wf.Connections)
	resp.Stats.HasEventTrigger = hasEventTrigger
	resp.Stats.TriggerEvent = triggerEvent

	return c.JSON(http.StatusOK, resp)
}
```

**Step 4: Verify compilation**

Run: `cd /Users/cheriehsieh/Program/orchestration && go build ./internal/dsl/...`
Expected: No errors

**Step 5: Commit**

```bash
git add internal/dsl/api/handler.go
git commit -m "feat(api): add workflow source endpoint with stats"
```

---

### Task 5: Wire Up API Server

**Files:**
- Modify: `cmd/workflow-api/main.go`

**Step 1: Add imports and dependencies**

Add to imports in `cmd/workflow-api/main.go`:

```go
"github.com/cheriehsieh/orchestration/internal/api"
"github.com/cheriehsieh/orchestration/internal/eventstore"
"go.mongodb.org/mongo-driver/mongo"
"go.mongodb.org/mongo-driver/mongo/options"
```

**Step 2: Add MongoDB connection and handlers**

Replace content of `cmd/workflow-api/main.go`:

```go
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/cheriehsieh/orchestration/internal/api"
	"github.com/cheriehsieh/orchestration/internal/config"
	"github.com/cheriehsieh/orchestration/internal/dsl"
	dslapi "github.com/cheriehsieh/orchestration/internal/dsl/api"
	"github.com/cheriehsieh/orchestration/internal/eventstore"
)

func main() {
	// 1. Load configuration
	cfg := config.Load()

	// 2. Setup logger
	logger := config.SetupLogger(cfg.Env)
	logger.Info("starting workflow API server",
		slog.String("env", cfg.Env),
	)

	// 3. Connect to MongoDB
	mongoURI := os.Getenv("MONGO_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27017"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	mongoClient, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	cancel()
	if err != nil {
		logger.Error("failed to connect to MongoDB", slog.Any("error", err))
		os.Exit(1)
	}
	defer mongoClient.Disconnect(context.Background())

	db := mongoClient.Database("orchestration")
	eventStore := eventstore.NewMongoEventStore(db, "events")

	// 4. Initialize DSL registry
	registry := dsl.NewWorkflowRegistry()

	// 5. Optionally load workflows from directory
	workflowDir := os.Getenv("WORKFLOW_DIR")
	if workflowDir != "" {
		if err := registry.LoadDirectory(context.Background(), workflowDir); err != nil {
			logger.Warn("failed to load workflows from directory",
				slog.String("dir", workflowDir),
				slog.Any("error", err),
			)
		} else {
			logger.Info("loaded workflows from directory",
				slog.String("dir", workflowDir),
				slog.Int("count", len(registry.ListWorkflows())),
			)
		}
	}

	// 6. Setup Echo server
	e := echo.New()
	e.HideBanner = true

	// Middleware
	e.Use(middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		LogStatus: true,
		LogURI:    true,
		LogMethod: true,
		LogValuesFunc: func(c echo.Context, v middleware.RequestLoggerValues) error {
			logger.Info("request",
				slog.String("method", v.Method),
				slog.String("uri", v.URI),
				slog.Int("status", v.Status),
			)
			return nil
		},
	}))
	e.Use(middleware.Recover())
	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete},
		AllowHeaders: []string{echo.HeaderContentType, echo.HeaderAuthorization},
	}))

	// 7. Register routes
	apiGroup := e.Group("/api")

	// Workflow routes
	workflowHandler := dslapi.NewWorkflowHandler(registry, logger)
	workflowHandler.RegisterRoutes(apiGroup)

	// Execution routes
	executionHandler := api.NewExecutionHandler(eventStore)
	executionHandler.RegisterRoutes(apiGroup)

	// Stream routes
	streamHandler := api.NewStreamHandler(eventStore, logger)
	streamHandler.RegisterRoutes(apiGroup)

	// Health check
	e.GET("/health", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})

	// 8. Start server in goroutine
	go func() {
		port := os.Getenv("WORKFLOW_API_PORT")
		if port == "" {
			port = "8083"
		}
		logger.Info("workflow API server listening", slog.String("port", port))
		if err := e.Start(":" + port); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", slog.Any("error", err))
			os.Exit(1)
		}
	}()

	// 9. Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	logger.Info("shutting down workflow API server...")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := e.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown error", slog.Any("error", err))
	}
}
```

**Step 3: Verify compilation**

Run: `cd /Users/cheriehsieh/Program/orchestration && go build ./cmd/workflow-api/...`
Expected: No errors

**Step 4: Commit**

```bash
git add cmd/workflow-api/main.go
git commit -m "feat(api): wire up execution and stream handlers in workflow-api server"
```

---

## Phase 2: Frontend Foundation

### Task 6: Install Dagre Dependency

**Files:**
- Modify: `web/package.json`

**Step 1: Install dagre**

Run: `cd /Users/cheriehsieh/Program/orchestration/web && npm install @dagrejs/dagre`

**Step 2: Verify installation**

Run: `cd /Users/cheriehsieh/Program/orchestration/web && npm ls @dagrejs/dagre`
Expected: Shows dagre version

**Step 3: Commit**

```bash
git add web/package.json web/package-lock.json
git commit -m "feat(web): add @dagrejs/dagre for graph layout"
```

---

### Task 7: Create TypeScript Types

**Files:**
- Create: `web/src/types.ts`

**Step 1: Create types file**

Create `web/src/types.ts`:

```typescript
// Workflow types
export interface WorkflowNode {
  id: string;
  type: string;
  name: string;
  parameters?: Record<string, unknown>;
}

export interface Connection {
  from_node: string;
  from_port?: string;
  to_node: string;
  to_port?: string;
}

export interface Workflow {
  id: string;
  name?: string;
  description?: string;
  version?: string;
  nodes: WorkflowNode[];
  connections: Connection[];
}

export interface WorkflowStats {
  node_count: number;
  connection_count: number;
  has_event_trigger: boolean;
  trigger_event?: string;
}

export interface WorkflowWithStats extends Workflow {
  stats: WorkflowStats;
}

// Execution types
export type ExecutionStatus = 'running' | 'completed' | 'failed';
export type NodeStatus = 'pending' | 'scheduled' | 'running' | 'completed' | 'failed';

export interface Execution {
  id: string;
  workflow_id: string;
  status: ExecutionStatus;
  started_at: string;
  ended_at?: string;
  duration_ms?: number;
}

export interface ExecutionEvent {
  id: string;
  type: string;
  node_id?: string;
  data?: Record<string, unknown>;
  timestamp: string;
}

export interface StreamMessage {
  type: 'event' | 'heartbeat' | 'error';
  event?: ExecutionEvent;
  timestamp: string;
  error?: string;
}

// Node Registry types
export interface NodeRegistration {
  full_type: string;
  display_name: string;
  description?: string;
  output_ports: string[];
  enabled: boolean;
  worker_count: number;
  last_heartbeat?: string;
}

// UI State types
export interface PanelState {
  left: boolean;
  right: boolean;
  bottom: boolean;
}

export type RightPanelView = 'inspector' | 'definition' | 'registry';
export type LeftPanelView = 'workflows' | 'registry';
```

**Step 2: Verify TypeScript compiles**

Run: `cd /Users/cheriehsieh/Program/orchestration/web && npx tsc --noEmit`
Expected: No errors

**Step 3: Commit**

```bash
git add web/src/types.ts
git commit -m "feat(web): add TypeScript type definitions"
```

---

### Task 8: Create Enhanced API Client

**Files:**
- Modify: `web/src/api.ts`

**Step 1: Replace api.ts with enhanced version**

Replace `web/src/api.ts`:

```typescript
import type {
  Workflow,
  WorkflowWithStats,
  Execution,
  ExecutionEvent,
  NodeRegistration,
} from './types';

const API_BASE = 'http://localhost:8083/api';
const REGISTRY_BASE = 'http://localhost:8082';

// Workflow APIs
export async function fetchWorkflows(): Promise<Workflow[]> {
  const response = await fetch(`${API_BASE}/workflows`);
  if (!response.ok) {
    throw new Error(`Failed to fetch workflows: ${response.statusText}`);
  }
  return response.json();
}

export async function fetchWorkflow(id: string): Promise<Workflow | null> {
  const response = await fetch(`${API_BASE}/workflows/${encodeURIComponent(id)}`);
  if (response.status === 404) {
    return null;
  }
  if (!response.ok) {
    throw new Error(`Failed to fetch workflow: ${response.statusText}`);
  }
  return response.json();
}

export async function fetchWorkflowSource(id: string): Promise<WorkflowWithStats | null> {
  const response = await fetch(`${API_BASE}/workflows/${encodeURIComponent(id)}/source`);
  if (response.status === 404) {
    return null;
  }
  if (!response.ok) {
    throw new Error(`Failed to fetch workflow source: ${response.statusText}`);
  }
  return response.json();
}

// Execution APIs
export async function fetchExecutions(workflowId: string): Promise<Execution[]> {
  const response = await fetch(`${API_BASE}/workflows/${encodeURIComponent(workflowId)}/executions`);
  if (!response.ok) {
    throw new Error(`Failed to fetch executions: ${response.statusText}`);
  }
  return response.json();
}

export async function fetchExecution(executionId: string): Promise<Execution | null> {
  const response = await fetch(`${API_BASE}/executions/${encodeURIComponent(executionId)}`);
  if (response.status === 404) {
    return null;
  }
  if (!response.ok) {
    throw new Error(`Failed to fetch execution: ${response.statusText}`);
  }
  return response.json();
}

export async function fetchExecutionEvents(
  executionId: string,
  since?: string
): Promise<ExecutionEvent[]> {
  let url = `${API_BASE}/executions/${encodeURIComponent(executionId)}/events`;
  if (since) {
    url += `?since=${encodeURIComponent(since)}`;
  }
  const response = await fetch(url);
  if (!response.ok) {
    throw new Error(`Failed to fetch execution events: ${response.statusText}`);
  }
  return response.json();
}

// Node Registry APIs
export async function fetchNodeTypes(): Promise<NodeRegistration[]> {
  const response = await fetch(`${REGISTRY_BASE}/nodes`);
  if (!response.ok) {
    throw new Error(`Failed to fetch node types: ${response.statusText}`);
  }
  return response.json();
}

export async function fetchNodeType(fullType: string): Promise<NodeRegistration | null> {
  const response = await fetch(`${REGISTRY_BASE}/nodes/${encodeURIComponent(fullType)}`);
  if (response.status === 404) {
    return null;
  }
  if (!response.ok) {
    throw new Error(`Failed to fetch node type: ${response.statusText}`);
  }
  return response.json();
}

// WebSocket stream URL builder
export function getStreamUrl(executionId: string): string {
  return `ws://localhost:8083/api/executions/${encodeURIComponent(executionId)}/stream`;
}
```

**Step 2: Verify TypeScript compiles**

Run: `cd /Users/cheriehsieh/Program/orchestration/web && npx tsc --noEmit`
Expected: No errors

**Step 3: Commit**

```bash
git add web/src/api.ts
git commit -m "feat(web): enhance API client with execution and registry endpoints"
```

---

### Task 9: Create UI Context

**Files:**
- Create: `web/src/context/UIContext.tsx`

**Step 1: Create context directory**

Run: `mkdir -p /Users/cheriehsieh/Program/orchestration/web/src/context`

**Step 2: Create UIContext**

Create `web/src/context/UIContext.tsx`:

```typescript
import { createContext, useContext, useReducer, type ReactNode } from 'react';
import type { PanelState, RightPanelView, LeftPanelView } from '../types';

interface UIState {
  panels: PanelState;
  rightPanelView: RightPanelView;
  leftPanelView: LeftPanelView;
  selectedNodeId: string | null;
  selectedEventId: string | null;
}

type UIAction =
  | { type: 'TOGGLE_PANEL'; panel: keyof PanelState }
  | { type: 'SET_RIGHT_VIEW'; view: RightPanelView }
  | { type: 'SET_LEFT_VIEW'; view: LeftPanelView }
  | { type: 'SELECT_NODE'; nodeId: string | null }
  | { type: 'SELECT_EVENT'; eventId: string | null }
  | { type: 'CLOSE_RIGHT_PANEL' };

const initialState: UIState = {
  panels: { left: true, right: false, bottom: true },
  rightPanelView: 'inspector',
  leftPanelView: 'workflows',
  selectedNodeId: null,
  selectedEventId: null,
};

function uiReducer(state: UIState, action: UIAction): UIState {
  switch (action.type) {
    case 'TOGGLE_PANEL':
      return {
        ...state,
        panels: { ...state.panels, [action.panel]: !state.panels[action.panel] },
      };
    case 'SET_RIGHT_VIEW':
      return { ...state, rightPanelView: action.view, panels: { ...state.panels, right: true } };
    case 'SET_LEFT_VIEW':
      return { ...state, leftPanelView: action.view };
    case 'SELECT_NODE':
      return {
        ...state,
        selectedNodeId: action.nodeId,
        rightPanelView: action.nodeId ? 'inspector' : state.rightPanelView,
        panels: { ...state.panels, right: action.nodeId !== null },
      };
    case 'SELECT_EVENT':
      return { ...state, selectedEventId: action.eventId };
    case 'CLOSE_RIGHT_PANEL':
      return {
        ...state,
        panels: { ...state.panels, right: false },
        selectedNodeId: null,
      };
    default:
      return state;
  }
}

const UIContext = createContext<{
  state: UIState;
  dispatch: React.Dispatch<UIAction>;
} | null>(null);

export function UIProvider({ children }: { children: ReactNode }) {
  const [state, dispatch] = useReducer(uiReducer, initialState);

  return (
    <UIContext.Provider value={{ state, dispatch }}>
      {children}
    </UIContext.Provider>
  );
}

export function useUI() {
  const context = useContext(UIContext);
  if (!context) {
    throw new Error('useUI must be used within a UIProvider');
  }
  return context;
}
```

**Step 3: Verify TypeScript compiles**

Run: `cd /Users/cheriehsieh/Program/orchestration/web && npx tsc --noEmit`
Expected: No errors

**Step 4: Commit**

```bash
git add web/src/context/UIContext.tsx
git commit -m "feat(web): add UI context for panel and selection state"
```

---

### Task 10: Create Workflow Context

**Files:**
- Create: `web/src/context/WorkflowContext.tsx`

**Step 1: Create WorkflowContext**

Create `web/src/context/WorkflowContext.tsx`:

```typescript
import { createContext, useContext, useState, useEffect, type ReactNode } from 'react';
import type { Workflow, Execution } from '../types';
import { fetchWorkflow, fetchWorkflows, fetchExecutions } from '../api';

interface WorkflowState {
  workflows: Workflow[];
  currentWorkflow: Workflow | null;
  currentWorkflowId: string | null;
  executions: Execution[];
  currentExecutionId: string | null;
  loading: boolean;
  error: string | null;
}

interface WorkflowContextValue extends WorkflowState {
  setCurrentWorkflowId: (id: string | null) => void;
  setCurrentExecutionId: (id: string | null) => void;
  refreshWorkflows: () => Promise<void>;
  refreshExecutions: () => Promise<void>;
}

const WorkflowContext = createContext<WorkflowContextValue | null>(null);

export function WorkflowProvider({ children }: { children: ReactNode }) {
  const [workflows, setWorkflows] = useState<Workflow[]>([]);
  const [currentWorkflow, setCurrentWorkflow] = useState<Workflow | null>(null);
  const [currentWorkflowId, setCurrentWorkflowId] = useState<string | null>(null);
  const [executions, setExecutions] = useState<Execution[]>([]);
  const [currentExecutionId, setCurrentExecutionId] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  // Load workflows list
  const refreshWorkflows = async () => {
    try {
      setLoading(true);
      setError(null);
      const wfs = await fetchWorkflows();
      setWorkflows(wfs);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load workflows');
    } finally {
      setLoading(false);
    }
  };

  // Load executions for current workflow
  const refreshExecutions = async () => {
    if (!currentWorkflowId) {
      setExecutions([]);
      return;
    }
    try {
      const execs = await fetchExecutions(currentWorkflowId);
      setExecutions(execs);
    } catch (err) {
      console.error('Failed to load executions:', err);
      setExecutions([]);
    }
  };

  // Load workflow details when ID changes
  useEffect(() => {
    if (!currentWorkflowId) {
      setCurrentWorkflow(null);
      setExecutions([]);
      setCurrentExecutionId(null);
      return;
    }

    const loadWorkflow = async () => {
      try {
        setLoading(true);
        const wf = await fetchWorkflow(currentWorkflowId);
        setCurrentWorkflow(wf);
        await refreshExecutions();
      } catch (err) {
        setError(err instanceof Error ? err.message : 'Failed to load workflow');
      } finally {
        setLoading(false);
      }
    };

    loadWorkflow();
  }, [currentWorkflowId]);

  // Initial load
  useEffect(() => {
    refreshWorkflows();
  }, []);

  // Auto-select first workflow
  useEffect(() => {
    if (workflows.length > 0 && !currentWorkflowId) {
      setCurrentWorkflowId(workflows[0].id);
    }
  }, [workflows, currentWorkflowId]);

  return (
    <WorkflowContext.Provider
      value={{
        workflows,
        currentWorkflow,
        currentWorkflowId,
        executions,
        currentExecutionId,
        loading,
        error,
        setCurrentWorkflowId,
        setCurrentExecutionId,
        refreshWorkflows,
        refreshExecutions,
      }}
    >
      {children}
    </WorkflowContext.Provider>
  );
}

export function useWorkflow() {
  const context = useContext(WorkflowContext);
  if (!context) {
    throw new Error('useWorkflow must be used within a WorkflowProvider');
  }
  return context;
}
```

**Step 2: Verify TypeScript compiles**

Run: `cd /Users/cheriehsieh/Program/orchestration/web && npx tsc --noEmit`
Expected: No errors

**Step 3: Commit**

```bash
git add web/src/context/WorkflowContext.tsx
git commit -m "feat(web): add workflow context for workflow and execution state"
```

---

### Task 11: Create Execution Context with WebSocket

**Files:**
- Create: `web/src/context/ExecutionContext.tsx`

**Step 1: Create ExecutionContext**

Create `web/src/context/ExecutionContext.tsx`:

```typescript
import {
  createContext,
  useContext,
  useState,
  useEffect,
  useCallback,
  type ReactNode,
} from 'react';
import type { ExecutionEvent, StreamMessage, NodeStatus } from '../types';
import { fetchExecutionEvents, getStreamUrl } from '../api';

type ConnectionStatus = 'disconnected' | 'connecting' | 'connected';

interface ExecutionContextValue {
  events: ExecutionEvent[];
  nodeStatuses: Map<string, NodeStatus>;
  connectionStatus: ConnectionStatus;
  clearEvents: () => void;
}

const ExecutionContext = createContext<ExecutionContextValue | null>(null);

interface Props {
  executionId: string | null;
  children: ReactNode;
}

export function ExecutionProvider({ executionId, children }: Props) {
  const [events, setEvents] = useState<ExecutionEvent[]>([]);
  const [connectionStatus, setConnectionStatus] = useState<ConnectionStatus>('disconnected');

  const clearEvents = useCallback(() => {
    setEvents([]);
  }, []);

  // Derive node statuses from events
  const nodeStatuses = new Map<string, NodeStatus>();
  events.forEach((evt) => {
    if (!evt.node_id) return;
    if (evt.type.includes('scheduled')) {
      nodeStatuses.set(evt.node_id, 'scheduled');
    } else if (evt.type.includes('started')) {
      nodeStatuses.set(evt.node_id, 'running');
    } else if (evt.type.includes('completed')) {
      nodeStatuses.set(evt.node_id, 'completed');
    } else if (evt.type.includes('failed')) {
      nodeStatuses.set(evt.node_id, 'failed');
    }
  });

  useEffect(() => {
    if (!executionId) {
      setEvents([]);
      setConnectionStatus('disconnected');
      return;
    }

    let ws: WebSocket | null = null;
    let reconnectTimeout: ReturnType<typeof setTimeout>;

    const connect = () => {
      setConnectionStatus('connecting');

      ws = new WebSocket(getStreamUrl(executionId));

      ws.onopen = () => {
        setConnectionStatus('connected');
      };

      ws.onmessage = (msg) => {
        try {
          const data: StreamMessage = JSON.parse(msg.data);
          if (data.type === 'event' && data.event) {
            setEvents((prev) => {
              // Avoid duplicates
              if (prev.some((e) => e.id === data.event!.id)) {
                return prev;
              }
              return [...prev, data.event!];
            });
          }
        } catch (err) {
          console.error('Failed to parse WebSocket message:', err);
        }
      };

      ws.onclose = () => {
        setConnectionStatus('disconnected');
        // Reconnect after 2 seconds
        reconnectTimeout = setTimeout(connect, 2000);
      };

      ws.onerror = (err) => {
        console.error('WebSocket error:', err);
        ws?.close();
      };
    };

    // Load existing events first, then connect
    fetchExecutionEvents(executionId)
      .then((existingEvents) => {
        setEvents(existingEvents);
        connect();
      })
      .catch((err) => {
        console.error('Failed to load initial events:', err);
        connect();
      });

    return () => {
      clearTimeout(reconnectTimeout);
      ws?.close();
    };
  }, [executionId]);

  return (
    <ExecutionContext.Provider
      value={{
        events,
        nodeStatuses,
        connectionStatus,
        clearEvents,
      }}
    >
      {children}
    </ExecutionContext.Provider>
  );
}

export function useExecution() {
  const context = useContext(ExecutionContext);
  if (!context) {
    throw new Error('useExecution must be used within an ExecutionProvider');
  }
  return context;
}
```

**Step 2: Verify TypeScript compiles**

Run: `cd /Users/cheriehsieh/Program/orchestration/web && npx tsc --noEmit`
Expected: No errors

**Step 3: Commit**

```bash
git add web/src/context/ExecutionContext.tsx
git commit -m "feat(web): add execution context with WebSocket streaming"
```

---

### Task 12: Create Dagre Layout Hook

**Files:**
- Create: `web/src/hooks/useDagreLayout.ts`

**Step 1: Create hooks directory**

Run: `mkdir -p /Users/cheriehsieh/Program/orchestration/web/src/hooks`

**Step 2: Create useDagreLayout hook**

Create `web/src/hooks/useDagreLayout.ts`:

```typescript
import { useMemo } from 'react';
import Dagre from '@dagrejs/dagre';
import type { Node, Edge } from '@xyflow/react';

interface LayoutOptions {
  direction?: 'LR' | 'TB';
  nodeWidth?: number;
  nodeHeight?: number;
  nodeSep?: number;
  rankSep?: number;
}

export function useDagreLayout(
  nodes: Node[],
  edges: Edge[],
  options: LayoutOptions = {}
) {
  const {
    direction = 'LR',
    nodeWidth = 180,
    nodeHeight = 70,
    nodeSep = 80,
    rankSep = 120,
  } = options;

  return useMemo(() => {
    if (nodes.length === 0) {
      return { nodes: [], edges: [] };
    }

    const g = new Dagre.graphlib.Graph().setDefaultEdgeLabel(() => ({}));

    g.setGraph({
      rankdir: direction,
      nodesep: nodeSep,
      ranksep: rankSep,
      marginx: 40,
      marginy: 40,
    });

    nodes.forEach((node) => {
      g.setNode(node.id, { width: nodeWidth, height: nodeHeight });
    });

    edges.forEach((edge) => {
      g.setEdge(edge.source, edge.target);
    });

    Dagre.layout(g);

    const layoutedNodes = nodes.map((node) => {
      const position = g.node(node.id);
      return {
        ...node,
        position: {
          x: position.x - nodeWidth / 2,
          y: position.y - nodeHeight / 2,
        },
      };
    });

    return { nodes: layoutedNodes, edges };
  }, [nodes, edges, direction, nodeWidth, nodeHeight, nodeSep, rankSep]);
}
```

**Step 3: Verify TypeScript compiles**

Run: `cd /Users/cheriehsieh/Program/orchestration/web && npx tsc --noEmit`
Expected: No errors

**Step 4: Commit**

```bash
git add web/src/hooks/useDagreLayout.ts
git commit -m "feat(web): add Dagre layout hook for automatic DAG positioning"
```

---

## Phase 3: Core UI Components

### Task 13: Create Custom Node Component

**Files:**
- Create: `web/src/components/graph/CustomNode.tsx`

**Step 1: Create graph directory**

Run: `mkdir -p /Users/cheriehsieh/Program/orchestration/web/src/components/graph`

**Step 2: Create CustomNode**

Create `web/src/components/graph/CustomNode.tsx`:

```typescript
import { memo } from 'react';
import { Handle, Position, type NodeProps } from '@xyflow/react';
import type { NodeStatus } from '../../types';

export interface CustomNodeData {
  label: string;
  type: string;
  status: NodeStatus;
  outputPorts?: string[];
}

const statusColors: Record<NodeStatus, string> = {
  pending: '#6b7280',
  scheduled: '#3b82f6',
  running: '#3b82f6',
  completed: '#22c55e',
  failed: '#ef4444',
};

const statusGlow: Record<NodeStatus, string> = {
  pending: 'none',
  scheduled: '0 0 15px #3b82f6',
  running: '0 0 20px #3b82f6',
  completed: 'none',
  failed: '0 0 10px #ef4444',
};

function CustomNode({ data, selected }: NodeProps<CustomNodeData>) {
  const status = data.status || 'pending';
  const bgColor = statusColors[status];
  const glow = statusGlow[status];

  return (
    <div
      style={{
        background: bgColor,
        color: '#fff',
        border: selected ? '2px solid #fff' : '2px solid #4b5563',
        borderRadius: '8px',
        padding: '10px 16px',
        minWidth: '160px',
        boxShadow: glow,
        transition: 'all 0.2s ease',
      }}
    >
      <Handle type="target" position={Position.Left} />

      <div style={{ fontWeight: 600, fontSize: '0.875rem' }}>{data.label}</div>
      <div style={{ fontSize: '0.75rem', opacity: 0.8, marginTop: '2px' }}>
        {data.type}
      </div>

      {data.outputPorts && data.outputPorts.length > 1 && (
        <div style={{ display: 'flex', gap: '8px', marginTop: '6px', fontSize: '0.65rem' }}>
          {data.outputPorts.map((port) => (
            <span
              key={port}
              style={{
                background: 'rgba(255,255,255,0.2)',
                padding: '2px 6px',
                borderRadius: '4px',
              }}
            >
              {port}
            </span>
          ))}
        </div>
      )}

      <Handle type="source" position={Position.Right} />
    </div>
  );
}

export default memo(CustomNode);
```

**Step 3: Verify TypeScript compiles**

Run: `cd /Users/cheriehsieh/Program/orchestration/web && npx tsc --noEmit`
Expected: No errors

**Step 4: Commit**

```bash
git add web/src/components/graph/CustomNode.tsx
git commit -m "feat(web): add custom node component with status colors"
```

---

### Task 14: Create Workflow Canvas Component

**Files:**
- Create: `web/src/components/graph/WorkflowCanvas.tsx`

**Step 1: Create WorkflowCanvas**

Create `web/src/components/graph/WorkflowCanvas.tsx`:

```typescript
import { useMemo, useCallback } from 'react';
import {
  ReactFlow,
  Background,
  Controls,
  MiniMap,
  type Node,
  type Edge,
} from '@xyflow/react';
import '@xyflow/react/dist/style.css';

import CustomNode, { type CustomNodeData } from './CustomNode';
import { useDagreLayout } from '../../hooks/useDagreLayout';
import { useWorkflow } from '../../context/WorkflowContext';
import { useExecution } from '../../context/ExecutionContext';
import { useUI } from '../../context/UIContext';
import type { NodeStatus } from '../../types';

const nodeTypes = {
  custom: CustomNode,
};

const statusColors: Record<NodeStatus, string> = {
  pending: '#6b7280',
  scheduled: '#3b82f6',
  running: '#3b82f6',
  completed: '#22c55e',
  failed: '#ef4444',
};

export default function WorkflowCanvas() {
  const { currentWorkflow } = useWorkflow();
  const { nodeStatuses } = useExecution();
  const { dispatch } = useUI();

  // Convert workflow to React Flow nodes/edges
  const { rawNodes, rawEdges } = useMemo(() => {
    if (!currentWorkflow) {
      return { rawNodes: [], rawEdges: [] };
    }

    const rawNodes: Node<CustomNodeData>[] = currentWorkflow.nodes.map((n) => ({
      id: n.id,
      type: 'custom',
      position: { x: 0, y: 0 }, // Will be set by Dagre
      data: {
        label: n.name || n.id,
        type: n.type,
        status: (nodeStatuses.get(n.id) || 'pending') as NodeStatus,
      },
    }));

    const rawEdges: Edge[] = currentWorkflow.connections.map((c, i) => ({
      id: `e-${c.from_node}-${c.to_node}-${i}`,
      source: c.from_node,
      target: c.to_node,
      animated: nodeStatuses.get(c.from_node) === 'running',
      style: { stroke: '#6b7280' },
    }));

    return { rawNodes, rawEdges };
  }, [currentWorkflow, nodeStatuses]);

  // Apply Dagre layout
  const { nodes, edges } = useDagreLayout(rawNodes, rawEdges);

  const onNodeClick = useCallback(
    (_: React.MouseEvent, node: Node) => {
      dispatch({ type: 'SELECT_NODE', nodeId: node.id });
    },
    [dispatch]
  );

  const onPaneClick = useCallback(() => {
    dispatch({ type: 'SELECT_NODE', nodeId: null });
  }, [dispatch]);

  if (!currentWorkflow) {
    return (
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          height: '100%',
          color: '#6b7280',
        }}
      >
        Select a workflow to view
      </div>
    );
  }

  return (
    <ReactFlow
      nodes={nodes}
      edges={edges}
      nodeTypes={nodeTypes}
      onNodeClick={onNodeClick}
      onPaneClick={onPaneClick}
      fitView
      fitViewOptions={{ padding: 0.2 }}
    >
      <Background color="#374151" gap={16} />
      <Controls />
      <MiniMap
        nodeColor={(n) => statusColors[(n.data as CustomNodeData)?.status || 'pending']}
        style={{ background: '#1f2937' }}
      />
    </ReactFlow>
  );
}
```

**Step 2: Verify TypeScript compiles**

Run: `cd /Users/cheriehsieh/Program/orchestration/web && npx tsc --noEmit`
Expected: No errors

**Step 3: Commit**

```bash
git add web/src/components/graph/WorkflowCanvas.tsx
git commit -m "feat(web): add workflow canvas with Dagre layout and status visualization"
```

---

### Task 15: Create Layout Components

**Files:**
- Create: `web/src/components/layout/Header.tsx`
- Create: `web/src/components/layout/LeftPanel.tsx`
- Create: `web/src/components/layout/RightPanel.tsx`
- Create: `web/src/components/layout/BottomPanel.tsx`

**Step 1: Create layout directory**

Run: `mkdir -p /Users/cheriehsieh/Program/orchestration/web/src/components/layout`

**Step 2: Create Header**

Create `web/src/components/layout/Header.tsx`:

```typescript
import { useWorkflow } from '../../context/WorkflowContext';
import { useExecution } from '../../context/ExecutionContext';

export default function Header() {
  const {
    workflows,
    currentWorkflowId,
    setCurrentWorkflowId,
    executions,
    currentExecutionId,
    setCurrentExecutionId,
  } = useWorkflow();
  const { connectionStatus } = useExecution();

  return (
    <header className="header">
      <h1>Workflow Dashboard</h1>

      <div style={{ display: 'flex', alignItems: 'center', gap: '1rem' }}>
        <div className="workflow-selector">
          <label>Workflow:</label>
          <select
            value={currentWorkflowId || ''}
            onChange={(e) => setCurrentWorkflowId(e.target.value || null)}
          >
            <option value="">Select workflow...</option>
            {workflows.map((wf) => (
              <option key={wf.id} value={wf.id}>
                {wf.id}
              </option>
            ))}
          </select>
        </div>

        <div className="workflow-selector">
          <label>Execution:</label>
          <select
            value={currentExecutionId || ''}
            onChange={(e) => setCurrentExecutionId(e.target.value || null)}
            disabled={executions.length === 0}
          >
            <option value="">Select execution...</option>
            {executions.map((exec) => (
              <option key={exec.id} value={exec.id}>
                {exec.id.slice(0, 12)}... ({exec.status})
              </option>
            ))}
          </select>
        </div>

        {currentExecutionId && (
          <span
            style={{
              fontSize: '0.75rem',
              padding: '4px 8px',
              borderRadius: '4px',
              background:
                connectionStatus === 'connected'
                  ? '#166534'
                  : connectionStatus === 'connecting'
                    ? '#1e40af'
                    : '#991b1b',
              color: '#fff',
            }}
          >
            {connectionStatus === 'connected'
              ? 'Live'
              : connectionStatus === 'connecting'
                ? 'Connecting...'
                : 'Disconnected'}
          </span>
        )}
      </div>
    </header>
  );
}
```

**Step 3: Create LeftPanel**

Create `web/src/components/layout/LeftPanel.tsx`:

```typescript
import { type ReactNode } from 'react';
import { useUI } from '../../context/UIContext';

interface Props {
  children: ReactNode;
}

export default function LeftPanel({ children }: Props) {
  const { state, dispatch } = useUI();

  if (!state.panels.left) {
    return (
      <button
        className="panel-toggle panel-toggle-left"
        onClick={() => dispatch({ type: 'TOGGLE_PANEL', panel: 'left' })}
        title="Show left panel"
      >
        &raquo;
      </button>
    );
  }

  return (
    <aside className="sidebar">
      <div className="panel-header">
        <button
          className="panel-close"
          onClick={() => dispatch({ type: 'TOGGLE_PANEL', panel: 'left' })}
          title="Hide panel"
        >
          &laquo;
        </button>
      </div>
      {children}
    </aside>
  );
}
```

**Step 4: Create RightPanel**

Create `web/src/components/layout/RightPanel.tsx`:

```typescript
import { type ReactNode } from 'react';
import { useUI } from '../../context/UIContext';

interface Props {
  children: ReactNode;
}

export default function RightPanel({ children }: Props) {
  const { state, dispatch } = useUI();

  if (!state.panels.right) {
    return null;
  }

  return (
    <aside className="right-panel">
      <div className="panel-header">
        <button
          className="panel-close"
          onClick={() => dispatch({ type: 'CLOSE_RIGHT_PANEL' })}
          title="Close panel"
        >
          &times;
        </button>
      </div>
      {children}
    </aside>
  );
}
```

**Step 5: Create BottomPanel**

Create `web/src/components/layout/BottomPanel.tsx`:

```typescript
import { type ReactNode } from 'react';
import { useUI } from '../../context/UIContext';

interface Props {
  children: ReactNode;
}

export default function BottomPanel({ children }: Props) {
  const { state, dispatch } = useUI();

  if (!state.panels.bottom) {
    return (
      <button
        className="panel-toggle panel-toggle-bottom"
        onClick={() => dispatch({ type: 'TOGGLE_PANEL', panel: 'bottom' })}
        title="Show timeline"
      >
        Timeline &uarr;
      </button>
    );
  }

  return (
    <div className="bottom-panel">
      <div className="panel-header">
        <span>Execution Timeline</span>
        <button
          className="panel-close"
          onClick={() => dispatch({ type: 'TOGGLE_PANEL', panel: 'bottom' })}
          title="Hide timeline"
        >
          &darr;
        </button>
      </div>
      {children}
    </div>
  );
}
```

**Step 6: Verify TypeScript compiles**

Run: `cd /Users/cheriehsieh/Program/orchestration/web && npx tsc --noEmit`
Expected: No errors

**Step 7: Commit**

```bash
git add web/src/components/layout/
git commit -m "feat(web): add collapsible layout panel components"
```

---

### Task 16: Create Panel Content Components

**Files:**
- Create: `web/src/components/panels/WorkflowList.tsx`
- Create: `web/src/components/panels/NodeInspector.tsx`
- Create: `web/src/components/panels/ExecutionTimeline.tsx`

**Step 1: Create panels directory**

Run: `mkdir -p /Users/cheriehsieh/Program/orchestration/web/src/components/panels`

**Step 2: Create WorkflowList**

Create `web/src/components/panels/WorkflowList.tsx`:

```typescript
import { useState } from 'react';
import { useWorkflow } from '../../context/WorkflowContext';

export default function WorkflowList() {
  const { workflows, currentWorkflowId, setCurrentWorkflowId, loading } = useWorkflow();
  const [search, setSearch] = useState('');

  const filtered = workflows.filter((wf) =>
    wf.id.toLowerCase().includes(search.toLowerCase())
  );

  return (
    <div className="workflow-list">
      <h3>Workflows</h3>

      <input
        type="text"
        placeholder="Search workflows..."
        value={search}
        onChange={(e) => setSearch(e.target.value)}
        className="search-input"
      />

      {loading && <p style={{ color: '#6b7280' }}>Loading...</p>}

      {filtered.length === 0 && !loading && (
        <p style={{ color: '#6b7280' }}>No workflows found</p>
      )}

      {filtered.map((wf) => (
        <div
          key={wf.id}
          className={`workflow-item ${currentWorkflowId === wf.id ? 'selected' : ''}`}
          onClick={() => setCurrentWorkflowId(wf.id)}
        >
          <div style={{ fontWeight: 500 }}>{wf.id}</div>
          <div style={{ color: '#9ca3af', fontSize: '0.75rem' }}>
            {wf.nodes.length} nodes
          </div>
        </div>
      ))}
    </div>
  );
}
```

**Step 3: Create NodeInspector**

Create `web/src/components/panels/NodeInspector.tsx`:

```typescript
import { useMemo } from 'react';
import { useWorkflow } from '../../context/WorkflowContext';
import { useExecution } from '../../context/ExecutionContext';
import { useUI } from '../../context/UIContext';

export default function NodeInspector() {
  const { currentWorkflow } = useWorkflow();
  const { nodeStatuses, events } = useExecution();
  const { state } = useUI();

  const selectedNode = useMemo(() => {
    if (!currentWorkflow || !state.selectedNodeId) return null;
    return currentWorkflow.nodes.find((n) => n.id === state.selectedNodeId);
  }, [currentWorkflow, state.selectedNodeId]);

  const nodeConnections = useMemo(() => {
    if (!currentWorkflow || !state.selectedNodeId) return { inputs: [], outputs: [] };

    const inputs = currentWorkflow.connections
      .filter((c) => c.to_node === state.selectedNodeId)
      .map((c) => ({ from: c.from_node, port: c.from_port || 'default' }));

    const outputs = currentWorkflow.connections
      .filter((c) => c.from_node === state.selectedNodeId)
      .map((c) => ({ to: c.to_node, port: c.from_port || 'default' }));

    return { inputs, outputs };
  }, [currentWorkflow, state.selectedNodeId]);

  const nodeEvents = useMemo(() => {
    if (!state.selectedNodeId) return [];
    return events.filter((e) => e.node_id === state.selectedNodeId);
  }, [events, state.selectedNodeId]);

  if (!selectedNode) {
    return (
      <div className="node-inspector">
        <h3>Node Inspector</h3>
        <p style={{ color: '#6b7280' }}>Select a node to view details</p>
      </div>
    );
  }

  const status = nodeStatuses.get(selectedNode.id) || 'pending';

  return (
    <div className="node-inspector">
      <h3>Node Inspector</h3>

      <div className="inspector-section">
        <div className="inspector-label">ID</div>
        <div className="inspector-value">{selectedNode.id}</div>
      </div>

      <div className="inspector-section">
        <div className="inspector-label">Type</div>
        <div className="inspector-value">{selectedNode.type}</div>
      </div>

      <div className="inspector-section">
        <div className="inspector-label">Name</div>
        <div className="inspector-value">{selectedNode.name || '—'}</div>
      </div>

      <div className="inspector-section">
        <div className="inspector-label">Status</div>
        <span className={`status-badge ${status}`}>{status}</span>
      </div>

      {selectedNode.parameters && Object.keys(selectedNode.parameters).length > 0 && (
        <div className="inspector-section">
          <div className="inspector-label">Parameters</div>
          <pre className="json-view">
            {JSON.stringify(selectedNode.parameters, null, 2)}
          </pre>
        </div>
      )}

      <div className="inspector-section">
        <div className="inspector-label">Connections</div>
        {nodeConnections.inputs.length > 0 && (
          <div style={{ marginBottom: '0.5rem' }}>
            <span style={{ color: '#9ca3af' }}>Inputs from:</span>
            {nodeConnections.inputs.map((c, i) => (
              <div key={i} style={{ marginLeft: '1rem' }}>
                {c.from} ({c.port})
              </div>
            ))}
          </div>
        )}
        {nodeConnections.outputs.length > 0 && (
          <div>
            <span style={{ color: '#9ca3af' }}>Outputs to:</span>
            {nodeConnections.outputs.map((c, i) => (
              <div key={i} style={{ marginLeft: '1rem' }}>
                {c.to} ({c.port})
              </div>
            ))}
          </div>
        )}
      </div>

      {nodeEvents.length > 0 && (
        <div className="inspector-section">
          <div className="inspector-label">Events ({nodeEvents.length})</div>
          {nodeEvents.slice(-3).map((evt) => (
            <div key={evt.id} style={{ fontSize: '0.75rem', marginBottom: '0.25rem' }}>
              {evt.type.split('.').pop()} - {new Date(evt.timestamp).toLocaleTimeString()}
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
```

**Step 4: Create ExecutionTimeline**

Create `web/src/components/panels/ExecutionTimeline.tsx`:

```typescript
import { useRef, useEffect, useState } from 'react';
import { useExecution } from '../../context/ExecutionContext';
import { useUI } from '../../context/UIContext';

export default function ExecutionTimeline() {
  const { events } = useExecution();
  const { dispatch } = useUI();
  const listRef = useRef<HTMLDivElement>(null);
  const [autoScroll, setAutoScroll] = useState(true);
  const [expandedEvents, setExpandedEvents] = useState<Set<string>>(new Set());

  // Auto-scroll to bottom when new events arrive
  useEffect(() => {
    if (autoScroll && listRef.current) {
      listRef.current.scrollTop = listRef.current.scrollHeight;
    }
  }, [events, autoScroll]);

  const toggleExpanded = (eventId: string) => {
    setExpandedEvents((prev) => {
      const next = new Set(prev);
      if (next.has(eventId)) {
        next.delete(eventId);
      } else {
        next.add(eventId);
      }
      return next;
    });
  };

  const handleEventClick = (nodeId: string | undefined) => {
    if (nodeId) {
      dispatch({ type: 'SELECT_NODE', nodeId });
    }
  };

  const getEventIcon = (type: string) => {
    if (type.includes('started')) return '○';
    if (type.includes('completed')) return '●';
    if (type.includes('failed')) return '✕';
    if (type.includes('scheduled')) return '◐';
    return '•';
  };

  const getEventColor = (type: string) => {
    if (type.includes('failed')) return '#ef4444';
    if (type.includes('completed')) return '#22c55e';
    if (type.includes('scheduled') || type.includes('started')) return '#3b82f6';
    return '#6b7280';
  };

  if (events.length === 0) {
    return (
      <div className="timeline-empty">
        No execution events. Select an execution to view its timeline.
      </div>
    );
  }

  let prevTime: Date | null = null;

  return (
    <div className="timeline-container">
      <div className="timeline-controls">
        <label>
          <input
            type="checkbox"
            checked={autoScroll}
            onChange={(e) => setAutoScroll(e.target.checked)}
          />
          Auto-scroll
        </label>
      </div>

      <div className="timeline-list" ref={listRef}>
        {events.map((evt) => {
          const time = new Date(evt.timestamp);
          const delta = prevTime ? time.getTime() - prevTime.getTime() : 0;
          prevTime = time;

          const isExpanded = expandedEvents.has(evt.id);
          const eventLabel = evt.type.split('.').slice(-2).join('.');

          return (
            <div
              key={evt.id}
              className="timeline-event"
              onClick={() => handleEventClick(evt.node_id)}
            >
              <div className="timeline-time">
                {time.toLocaleTimeString('en-US', {
                  hour12: false,
                  hour: '2-digit',
                  minute: '2-digit',
                  second: '2-digit',
                })}
                .{String(time.getMilliseconds()).padStart(3, '0')}
              </div>

              <div
                className="timeline-icon"
                style={{ color: getEventColor(evt.type) }}
              >
                {getEventIcon(evt.type)}
              </div>

              <div className="timeline-content">
                <div className="timeline-label">
                  {eventLabel}
                  {evt.node_id && (
                    <span className="timeline-node">: {evt.node_id}</span>
                  )}
                  {delta > 0 && (
                    <span className="timeline-delta">[+{delta}ms]</span>
                  )}
                </div>

                {evt.data && Object.keys(evt.data).length > 0 && (
                  <>
                    <button
                      className="timeline-expand"
                      onClick={(e) => {
                        e.stopPropagation();
                        toggleExpanded(evt.id);
                      }}
                    >
                      {isExpanded ? '▼' : '▶'} Data
                    </button>

                    {isExpanded && (
                      <pre className="timeline-data">
                        {JSON.stringify(evt.data, null, 2)}
                      </pre>
                    )}
                  </>
                )}
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}
```

**Step 5: Verify TypeScript compiles**

Run: `cd /Users/cheriehsieh/Program/orchestration/web && npx tsc --noEmit`
Expected: No errors

**Step 6: Commit**

```bash
git add web/src/components/panels/
git commit -m "feat(web): add workflow list, node inspector, and timeline panels"
```

---

### Task 17: Update App.tsx and Styles

**Files:**
- Modify: `web/src/App.tsx`
- Modify: `web/src/App.css`

**Step 1: Replace App.tsx**

Replace `web/src/App.tsx`:

```typescript
import { UIProvider, useUI } from './context/UIContext';
import { WorkflowProvider, useWorkflow } from './context/WorkflowContext';
import { ExecutionProvider } from './context/ExecutionContext';

import Header from './components/layout/Header';
import LeftPanel from './components/layout/LeftPanel';
import RightPanel from './components/layout/RightPanel';
import BottomPanel from './components/layout/BottomPanel';
import WorkflowCanvas from './components/graph/WorkflowCanvas';
import WorkflowList from './components/panels/WorkflowList';
import NodeInspector from './components/panels/NodeInspector';
import ExecutionTimeline from './components/panels/ExecutionTimeline';

import './App.css';

function AppContent() {
  const { currentExecutionId } = useWorkflow();
  const { state } = useUI();

  return (
    <ExecutionProvider executionId={currentExecutionId}>
      <div className="app">
        <Header />

        <div className="main">
          <LeftPanel>
            <WorkflowList />
          </LeftPanel>

          <div className="graph-container">
            <WorkflowCanvas />
          </div>

          <RightPanel>
            {state.rightPanelView === 'inspector' && <NodeInspector />}
          </RightPanel>
        </div>

        <BottomPanel>
          <ExecutionTimeline />
        </BottomPanel>
      </div>
    </ExecutionProvider>
  );
}

export default function App() {
  return (
    <UIProvider>
      <WorkflowProvider>
        <AppContent />
      </WorkflowProvider>
    </UIProvider>
  );
}
```

**Step 2: Update App.css**

Replace `web/src/App.css`:

```css
:root {
  font-family: 'Inter', -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
  line-height: 1.5;
  color: #f3f4f6;
  background-color: #111827;
}

* {
  margin: 0;
  padding: 0;
  box-sizing: border-box;
}

.app {
  display: flex;
  flex-direction: column;
  height: 100vh;
}

/* Header */
.header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 0.75rem 1.5rem;
  background: #1f2937;
  border-bottom: 1px solid #374151;
  flex-shrink: 0;
}

.header h1 {
  font-size: 1.125rem;
  font-weight: 600;
}

/* Main layout */
.main {
  display: flex;
  flex: 1;
  overflow: hidden;
  position: relative;
}

/* Sidebar / Left Panel */
.sidebar {
  width: 280px;
  background: #1f2937;
  border-right: 1px solid #374151;
  overflow-y: auto;
  flex-shrink: 0;
}

/* Right Panel */
.right-panel {
  width: 360px;
  background: #1f2937;
  border-left: 1px solid #374151;
  overflow-y: auto;
  flex-shrink: 0;
}

/* Bottom Panel */
.bottom-panel {
  height: 220px;
  background: #1f2937;
  border-top: 1px solid #374151;
  display: flex;
  flex-direction: column;
  flex-shrink: 0;
}

/* Graph container */
.graph-container {
  flex: 1;
  background: #111827;
  min-width: 0;
}

/* Panel headers */
.panel-header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 0.5rem 1rem;
  border-bottom: 1px solid #374151;
  font-size: 0.75rem;
  text-transform: uppercase;
  color: #9ca3af;
  letter-spacing: 0.05em;
}

.panel-close {
  background: transparent;
  border: none;
  color: #9ca3af;
  font-size: 1rem;
  cursor: pointer;
  padding: 0.25rem 0.5rem;
}

.panel-close:hover {
  color: #f3f4f6;
}

/* Panel toggle buttons */
.panel-toggle {
  position: absolute;
  background: #374151;
  border: 1px solid #4b5563;
  color: #9ca3af;
  cursor: pointer;
  font-size: 0.75rem;
  z-index: 10;
}

.panel-toggle:hover {
  background: #4b5563;
  color: #f3f4f6;
}

.panel-toggle-left {
  left: 0;
  top: 50%;
  transform: translateY(-50%);
  padding: 1rem 0.25rem;
  border-radius: 0 4px 4px 0;
}

.panel-toggle-bottom {
  bottom: 0;
  left: 50%;
  transform: translateX(-50%);
  padding: 0.25rem 1rem;
  border-radius: 4px 4px 0 0;
}

/* Selectors */
.workflow-selector {
  display: flex;
  align-items: center;
  gap: 0.5rem;
}

.workflow-selector label {
  font-size: 0.75rem;
  color: #9ca3af;
}

.workflow-selector select {
  padding: 0.375rem 0.75rem;
  background: #374151;
  color: #f3f4f6;
  border: 1px solid #4b5563;
  border-radius: 4px;
  cursor: pointer;
  font-size: 0.875rem;
}

/* Search input */
.search-input {
  width: 100%;
  padding: 0.5rem;
  background: #374151;
  color: #f3f4f6;
  border: 1px solid #4b5563;
  border-radius: 4px;
  font-size: 0.875rem;
  margin-bottom: 0.75rem;
}

.search-input:focus {
  outline: none;
  border-color: #3b82f6;
}

/* Workflow list */
.workflow-list {
  padding: 1rem;
}

.workflow-list h3 {
  font-size: 0.75rem;
  color: #9ca3af;
  margin-bottom: 0.75rem;
  text-transform: uppercase;
  letter-spacing: 0.05em;
}

.workflow-item {
  padding: 0.75rem;
  margin-bottom: 0.5rem;
  background: #374151;
  border-radius: 6px;
  cursor: pointer;
  transition: all 0.15s;
}

.workflow-item:hover {
  background: #4b5563;
}

.workflow-item.selected {
  background: #3b82f6;
}

/* Node Inspector */
.node-inspector {
  padding: 1rem;
}

.node-inspector h3 {
  font-size: 0.75rem;
  color: #9ca3af;
  margin-bottom: 1rem;
  text-transform: uppercase;
  letter-spacing: 0.05em;
}

.inspector-section {
  margin-bottom: 1rem;
}

.inspector-label {
  font-size: 0.75rem;
  color: #9ca3af;
  margin-bottom: 0.25rem;
}

.inspector-value {
  font-weight: 500;
}

.json-view {
  background: #374151;
  padding: 0.5rem;
  border-radius: 4px;
  font-size: 0.75rem;
  overflow-x: auto;
  white-space: pre-wrap;
  word-break: break-all;
}

/* Status badges */
.status-badge {
  display: inline-block;
  padding: 0.125rem 0.5rem;
  border-radius: 9999px;
  font-size: 0.75rem;
  font-weight: 500;
  text-transform: uppercase;
}

.status-badge.pending { background: #374151; color: #9ca3af; }
.status-badge.scheduled { background: #1e40af; color: #93c5fd; }
.status-badge.running { background: #1e40af; color: #93c5fd; }
.status-badge.completed { background: #166534; color: #86efac; }
.status-badge.failed { background: #991b1b; color: #fca5a5; }

/* Timeline */
.timeline-container {
  display: flex;
  flex-direction: column;
  height: 100%;
}

.timeline-controls {
  padding: 0.5rem 1rem;
  border-bottom: 1px solid #374151;
  font-size: 0.75rem;
}

.timeline-controls label {
  display: flex;
  align-items: center;
  gap: 0.5rem;
  color: #9ca3af;
}

.timeline-list {
  flex: 1;
  overflow-y: auto;
  padding: 0.5rem;
}

.timeline-empty {
  display: flex;
  align-items: center;
  justify-content: center;
  height: 100%;
  color: #6b7280;
  font-size: 0.875rem;
}

.timeline-event {
  display: flex;
  gap: 0.75rem;
  padding: 0.5rem;
  border-radius: 4px;
  cursor: pointer;
  transition: background 0.15s;
}

.timeline-event:hover {
  background: #374151;
}

.timeline-time {
  font-family: monospace;
  font-size: 0.75rem;
  color: #6b7280;
  flex-shrink: 0;
  width: 100px;
}

.timeline-icon {
  font-size: 0.875rem;
  flex-shrink: 0;
}

.timeline-content {
  flex: 1;
  min-width: 0;
}

.timeline-label {
  font-size: 0.875rem;
}

.timeline-node {
  color: #9ca3af;
}

.timeline-delta {
  color: #6b7280;
  font-size: 0.75rem;
  margin-left: 0.5rem;
}

.timeline-expand {
  background: transparent;
  border: none;
  color: #3b82f6;
  font-size: 0.75rem;
  cursor: pointer;
  padding: 0.25rem 0;
  margin-top: 0.25rem;
}

.timeline-expand:hover {
  color: #60a5fa;
}

.timeline-data {
  background: #374151;
  padding: 0.5rem;
  border-radius: 4px;
  font-size: 0.75rem;
  margin-top: 0.25rem;
  overflow-x: auto;
  white-space: pre-wrap;
  word-break: break-all;
}

/* React Flow overrides */
.react-flow__attribution {
  display: none !important;
}
```

**Step 3: Verify build**

Run: `cd /Users/cheriehsieh/Program/orchestration/web && npm run build`
Expected: Build succeeds

**Step 4: Commit**

```bash
git add web/src/App.tsx web/src/App.css
git commit -m "feat(web): integrate all components into main app layout"
```

---

### Task 18: Add Context Exports

**Files:**
- Create: `web/src/context/index.ts`

**Step 1: Create index file**

Create `web/src/context/index.ts`:

```typescript
export { UIProvider, useUI } from './UIContext';
export { WorkflowProvider, useWorkflow } from './WorkflowContext';
export { ExecutionProvider, useExecution } from './ExecutionContext';
```

**Step 2: Verify build**

Run: `cd /Users/cheriehsieh/Program/orchestration/web && npm run build`
Expected: Build succeeds

**Step 3: Commit**

```bash
git add web/src/context/index.ts
git commit -m "feat(web): add context barrel export"
```

---

## Phase 4: Final Integration

### Task 19: End-to-End Test

**Step 1: Start backend services**

Run in terminal 1:
```bash
cd /Users/cheriehsieh/Program/orchestration
docker-compose up -d  # Start MongoDB and NATS
go run ./cmd/workflow-api/main.go
```

**Step 2: Start frontend dev server**

Run in terminal 2:
```bash
cd /Users/cheriehsieh/Program/orchestration/web
npm run dev
```

**Step 3: Verify in browser**

Open: http://localhost:5173

Expected behavior:
- Workflow selector shows available workflows
- Graph displays with proper DAG layout
- Clicking node opens inspector panel
- Timeline shows execution events when execution selected

**Step 4: Commit final state**

```bash
git add -A
git commit -m "feat: complete workflow UI implementation"
```

---

## Summary

**Backend changes:**
- `internal/api/models.go` - Response types
- `internal/api/execution_handler.go` - Execution endpoints
- `internal/api/stream_handler.go` - WebSocket streaming
- `internal/eventstore/eventstore.go` - Extended interface
- `internal/eventstore/mongo.go` - New query method
- `internal/dsl/api/handler.go` - Source endpoint
- `cmd/workflow-api/main.go` - Wire up handlers

**Frontend changes:**
- `web/src/types.ts` - TypeScript definitions
- `web/src/api.ts` - Enhanced API client
- `web/src/context/` - UI, Workflow, Execution contexts
- `web/src/hooks/useDagreLayout.ts` - Graph layout
- `web/src/components/graph/` - CustomNode, WorkflowCanvas
- `web/src/components/layout/` - Header, panels
- `web/src/components/panels/` - WorkflowList, NodeInspector, Timeline
- `web/src/App.tsx` - Main app composition
- `web/src/App.css` - Complete styling

**Total: 19 tasks, ~25 files**
