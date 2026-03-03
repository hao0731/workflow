package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gocql/gocql"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"go.mongodb.org/mongo-driver/mongo"

	"github.com/cheriehsieh/orchestration/internal/config"
	"github.com/cheriehsieh/orchestration/internal/engine"
	"github.com/cheriehsieh/orchestration/internal/eventstore"
)

// WorkflowAPIRepo extends WorkflowRepository with list capability.
type WorkflowAPIRepo interface {
	engine.WorkflowRepository
	ListAll(ctx context.Context) ([]*engine.Workflow, error)
	FindByEventTrigger(ctx context.Context, eventName, domain string) ([]*engine.Workflow, error)
}

// InMemoryWorkflowRepo is a simple in-memory workflow repository for API.
type InMemoryWorkflowRepo struct {
	workflows map[string]*engine.Workflow
}

func (r *InMemoryWorkflowRepo) GetByID(_ context.Context, id string) (*engine.Workflow, error) {
	if w, ok := r.workflows[id]; ok {
		return w, nil
	}
	return nil, fmt.Errorf("workflow %s not found", id)
}

func (r *InMemoryWorkflowRepo) ListAll(_ context.Context) ([]*engine.Workflow, error) {
	result := make([]*engine.Workflow, 0, len(r.workflows))
	for _, w := range r.workflows {
		result = append(result, w)
	}
	return result, nil
}

func (r *InMemoryWorkflowRepo) FindByEventTrigger(_ context.Context, eventName, domain string) ([]*engine.Workflow, error) {
	var matches []*engine.Workflow
	for _, wf := range r.workflows {
		start := wf.GetStartNode()
		if start == nil || start.Trigger == nil {
			continue
		}
		if start.Trigger.Type != engine.TriggerEvent {
			continue
		}
		triggerEvent, _ := start.Trigger.Criteria["event_name"].(string)
		triggerDomain, _ := start.Trigger.Criteria["domain"].(string)

		if triggerEvent == eventName && triggerDomain == domain {
			matches = append(matches, wf)
		}
	}
	return matches, nil
}

// Handler holds dependencies for API handlers.
type Handler struct {
	workflows      WorkflowAPIRepo
	eventStore     eventstore.EventStore
	executionStore eventstore.ExecutionStore
	logger         *slog.Logger
}

// API Response types matching frontend TypeScript interfaces
type WorkflowResponse struct {
	ID          string               `json:"id"`
	Nodes       []NodeResponse       `json:"nodes"`
	Connections []ConnectionResponse `json:"connections"`
}

type NodeResponse struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Name string `json:"name"`
}

type ConnectionResponse struct {
	FromNode string `json:"from_node"`
	FromPort string `json:"from_port,omitempty"`
	ToNode   string `json:"to_node"`
	ToPort   string `json:"to_port,omitempty"`
}

type ExecutionResponse struct {
	ID         string `json:"id"`
	WorkflowID string `json:"workflow_id"`
	Status     string `json:"status"`
	StartedAt  string `json:"started_at"`
}

type NodeEventResponse struct {
	ID        string `json:"id"`
	Type      string `json:"type"`
	NodeID    string `json:"node_id"`
	Timestamp string `json:"timestamp"`
}

func toWorkflowResponse(w *engine.Workflow) WorkflowResponse {
	nodes := make([]NodeResponse, len(w.Nodes))
	for i, n := range w.Nodes {
		nodes[i] = NodeResponse{
			ID:   n.ID,
			Type: string(n.Type),
			Name: n.Name,
		}
	}

	connections := make([]ConnectionResponse, len(w.Connections))
	for i, c := range w.Connections {
		connections[i] = ConnectionResponse{
			FromNode: c.FromNode,
			FromPort: c.FromPort,
			ToNode:   c.ToNode,
			ToPort:   c.ToPort,
		}
	}

	return WorkflowResponse{
		ID:          w.ID,
		Nodes:       nodes,
		Connections: connections,
	}
}

// GET /api/workflows
func (h *Handler) listWorkflows(c echo.Context) error {
	workflows, err := h.workflows.ListAll(c.Request().Context())
	if err != nil {
		h.logger.Error("failed to list workflows", slog.Any("error", err))
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	response := make([]WorkflowResponse, len(workflows))
	for i, w := range workflows {
		response[i] = toWorkflowResponse(w)
	}

	return c.JSON(http.StatusOK, response)
}

// GET /api/workflows/:id
func (h *Handler) getWorkflow(c echo.Context) error {
	id := c.Param("id")
	workflow, err := h.workflows.GetByID(c.Request().Context(), id)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, toWorkflowResponse(workflow))
}

// GET /api/workflows/:id/executions
func (h *Handler) listExecutions(c echo.Context) error {
	workflowID := c.Param("id")

	executions, err := h.executionStore.GetByWorkflowID(c.Request().Context(), workflowID)
	if err != nil {
		h.logger.Error("failed to list executions", slog.Any("error", err))
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	response := make([]ExecutionResponse, len(executions))
	for i, exec := range executions {
		response[i] = ExecutionResponse{
			ID:         exec.ID,
			WorkflowID: exec.WorkflowID,
			Status:     exec.Status,
			StartedAt:  exec.StartedAt.Format(time.RFC3339),
		}
	}

	return c.JSON(http.StatusOK, response)
}

// GET /api/executions/:id/events
func (h *Handler) listExecutionEvents(c echo.Context) error {
	executionID := c.Param("id")

	events, err := h.eventStore.GetBySubject(c.Request().Context(), executionID)
	if err != nil {
		h.logger.Error("failed to list execution events", slog.Any("error", err))
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	response := make([]NodeEventResponse, 0, len(events))
	for _, e := range events {
		// Only include node-related events
		if e.Type() == engine.NodeExecutionScheduled ||
			e.Type() == engine.NodeExecutionStarted ||
			e.Type() == engine.NodeExecutionCompleted ||
			e.Type() == engine.NodeExecutionFailed {
			var data map[string]any
			_ = e.DataAs(&data)

			nodeID, _ := data["node_id"].(string)
			response = append(response, NodeEventResponse{
				ID:        e.ID(),
				Type:      e.Type(),
				NodeID:    nodeID,
				Timestamp: e.Time().Format(time.RFC3339),
			})
		}
	}

	return c.JSON(http.StatusOK, response)
}

func main() {
	// 1. Load configuration
	cfg := config.Load()

	// 2. Setup logger
	logger := config.SetupLogger(cfg.Env)
	logger.Info("starting workflow API server",
		slog.String("env", cfg.Env),
	)

	// 3. Connect to MongoDB
	mongoOpts, err := cfg.MongoClientOptions()
	if err != nil {
		logger.Error("invalid MongoDB configuration", slog.Any("error", err))
		os.Exit(1)
	}

	mongoCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	client, err := mongo.Connect(mongoCtx, mongoOpts)
	if err != nil {
		logger.Error("failed to connect to MongoDB", slog.Any("error", err))
		os.Exit(1)
	}
	defer func() {
		_ = client.Disconnect(context.Background())
	}()
	db := client.Database(cfg.MongoDatabase)

	// 3b. Connect to Cassandra
	cassandraCluster := gocql.NewCluster(strings.Split(cfg.CassandraHosts, ",")...)
	cassandraCluster.Port = cfg.CassandraPort
	cassandraCluster.Keyspace = cfg.CassandraKeyspace
	cassandraCluster.Consistency = gocql.LocalQuorum

	var cassandraSession *gocql.Session
	for attempt := 1; attempt <= 5; attempt++ {
		cassandraSession, err = cassandraCluster.CreateSession()
		if err == nil {
			break
		}
		logger.Warn("failed to connect to Cassandra, retrying...",
			slog.Int("attempt", attempt),
			slog.Any("error", err),
		)
		time.Sleep(time.Duration(attempt) * 2 * time.Second)
	}
	if err != nil {
		logger.Error("failed to connect to Cassandra after retries", slog.Any("error", err))
		os.Exit(1)
	}
	defer cassandraSession.Close()

	// 4. Initialize repositories
	eventStoreImpl := eventstore.NewCassandraEventStore(cassandraSession)
	executionStore := eventstore.NewMongoExecutionStore(db, "executions")

	// Define the same workflows as the engine
	publisherWorkflow := &engine.Workflow{
		ID: "order-service",
		Nodes: []engine.Node{
			{ID: "start", Type: engine.StartNode, Name: "Start"},
			{ID: "create-order", Type: engine.ActionNode, Name: "Create Order"},
			{
				ID:   "publish-event",
				Type: engine.PublishEvent,
				Name: "Publish Order Created",
			},
		},
		Connections: []engine.Connection{
			{FromNode: "start", ToNode: "create-order"},
			{FromNode: "create-order", ToNode: "publish-event"},
		},
	}

	subscriberWorkflow := &engine.Workflow{
		ID: "shipping-service",
		Nodes: []engine.Node{
			{
				ID:   "start",
				Type: engine.StartNode,
				Name: "Event Trigger",
				Trigger: &engine.Trigger{
					Type: engine.TriggerEvent,
					Criteria: map[string]any{
						"event_name": "order_created",
						"domain":     "ecommerce",
					},
				},
			},
			{ID: "prepare-shipment", Type: engine.ActionNode, Name: "Prepare Shipment"},
			{ID: "notify-customer", Type: engine.ActionNode, Name: "Notify Customer"},
		},
		Connections: []engine.Connection{
			{FromNode: "start", ToNode: "prepare-shipment"},
			{FromNode: "prepare-shipment", ToNode: "notify-customer"},
		},
	}

	workflowRepo := &InMemoryWorkflowRepo{
		workflows: map[string]*engine.Workflow{
			publisherWorkflow.ID:  publisherWorkflow,
			subscriberWorkflow.ID: subscriberWorkflow,
		},
	}

	// 5. Setup Echo server
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
		AllowOrigins: []string{"http://localhost:5173", "http://localhost:3000"},
		AllowMethods: []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete},
	}))

	// 6. Register routes
	handler := &Handler{
		workflows:      workflowRepo,
		eventStore:     eventStoreImpl,
		executionStore: executionStore,
		logger:         logger,
	}

	apiGroup := e.Group("/api")
	apiGroup.GET("/workflows", handler.listWorkflows)
	apiGroup.GET("/workflows/:id", handler.getWorkflow)
	apiGroup.GET("/workflows/:id/executions", handler.listExecutions)
	apiGroup.GET("/executions/:id/events", handler.listExecutionEvents)

	// 7. Start server in goroutine
	go func() {
		port := os.Getenv("API_PORT")
		if port == "" {
			port = "8081"
		}
		logger.Info("API server listening", slog.String("port", port))
		if err := e.Start(":" + port); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server error", slog.Any("error", err))
			os.Exit(1)
		}
	}()

	// 8. Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	logger.Info("shutting down API server...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := e.Shutdown(ctx); err != nil {
		logger.Error("shutdown error", slog.Any("error", err))
	}
}
