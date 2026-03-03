package api

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"

	"github.com/cheriehsieh/orchestration/internal/dsl"
	"github.com/cheriehsieh/orchestration/internal/engine"
	"github.com/cheriehsieh/orchestration/internal/eventbus"
	"github.com/cheriehsieh/orchestration/internal/marketplace"
)

// WorkflowHandler handles HTTP requests for workflow management.
type WorkflowHandler struct {
	registry      *dsl.WorkflowRegistry
	parser        dsl.WorkflowParser
	validator     dsl.WorkflowValidator
	converter     dsl.WorkflowConverter
	logger        *slog.Logger
	eventBus      eventbus.Publisher
	eventRegistry marketplace.EventRegistry
}

// HandlerOption is a functional option for WorkflowHandler.
type HandlerOption func(*WorkflowHandler)

// WithEventBus sets the event bus for publishing execution events.
func WithEventBus(eb eventbus.Publisher) HandlerOption {
	return func(h *WorkflowHandler) {
		h.eventBus = eb
	}
}

// WithEventRegistry sets the event registry for registering marketplace events.
func WithEventRegistry(er marketplace.EventRegistry) HandlerOption {
	return func(h *WorkflowHandler) {
		h.eventRegistry = er
	}
}

// NewWorkflowHandler creates a new WorkflowHandler.
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

// RegisterRoutes registers workflow API routes.
func (h *WorkflowHandler) RegisterRoutes(g *echo.Group) {
	g.POST("/workflows", h.Create)
	g.GET("/workflows", h.List)
	g.GET("/workflows/:id", h.Get)
	g.GET("/workflows/:id/source", h.GetSource)
	g.PUT("/workflows/:id", h.Update)
	g.DELETE("/workflows/:id", h.Delete)
	g.POST("/workflows/:id/execute", h.ExecuteWorkflow)
}

// WorkflowResponse is the API response for a workflow.
type WorkflowResponse struct {
	ID          string               `json:"id"`
	Nodes       []NodeResponse       `json:"nodes"`
	Connections []ConnectionResponse `json:"connections"`
}

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

// NodeResponse is the API response for a node.
type NodeResponse struct {
	ID   string `json:"id"`
	Type string `json:"type"`
	Name string `json:"name"`
}

// ConnectionResponse is the API response for a connection.
type ConnectionResponse struct {
	FromNode string `json:"from_node"`
	FromPort string `json:"from_port,omitempty"`
	ToNode   string `json:"to_node"`
	ToPort   string `json:"to_port,omitempty"`
}

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

// Create handles POST /workflows
// Query param: ?dryrun=true to validate without saving
func (h *WorkflowHandler) Create(c echo.Context) error {
	body, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "failed to read body"})
	}

	// Parse YAML
	def, err := h.parser.Parse(body)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}

	// Validate
	if validateErr := h.validator.Validate(def); validateErr != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": validateErr.Error()})
	}

	// Convert to engine model
	wf, err := h.converter.Convert(def)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}

	// Check dryrun mode
	dryrun := c.QueryParam("dryrun") == "true"
	if dryrun {
		h.logger.Info("workflow validated (dryrun)", slog.String("id", wf.ID))
		return c.JSON(http.StatusOK, map[string]any{
			"valid":   true,
			"id":      wf.ID,
			"message": "validation successful (dryrun mode)",
		})
	}

	// Register workflow
	if err := h.registry.RegisterWithSource(wf, body); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

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

	h.logger.Info("workflow created", slog.String("id", wf.ID))
	return c.JSON(http.StatusCreated, map[string]any{
		"id":      wf.ID,
		"message": "workflow created",
	})
}

// List handles GET /workflows
func (h *WorkflowHandler) List(c echo.Context) error {
	ids := h.registry.ListWorkflows()

	workflows := make([]WorkflowResponse, 0, len(ids))
	for _, id := range ids {
		wf, err := h.registry.GetByID(c.Request().Context(), id)
		if err != nil {
			continue
		}
		workflows = append(workflows, toWorkflowResponse(wf))
	}

	return c.JSON(http.StatusOK, workflows)
}

// Get handles GET /workflows/:id
func (h *WorkflowHandler) Get(c echo.Context) error {
	id := c.Param("id")

	wf, err := h.registry.GetByID(c.Request().Context(), id)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, toWorkflowResponse(wf))
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

// Update handles PUT /workflows/:id
func (h *WorkflowHandler) Update(c echo.Context) error {
	id := c.Param("id")

	// Check if workflow exists
	if _, err := h.registry.GetByID(c.Request().Context(), id); err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": err.Error()})
	}

	body, err := io.ReadAll(c.Request().Body)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "failed to read body"})
	}

	// Parse YAML
	def, err := h.parser.Parse(body)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}

	// Ensure ID matches
	if def.ID != id {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "workflow ID in body does not match URL"})
	}

	// Validate
	if validateErr := h.validator.Validate(def); validateErr != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": validateErr.Error()})
	}

	// Convert to engine model
	wf, err := h.converter.Convert(def)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}

	// Register (upsert)
	if err := h.registry.RegisterWithSource(wf, body); err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

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

	h.logger.Info("workflow updated", slog.String("id", wf.ID))
	return c.JSON(http.StatusOK, map[string]any{
		"id":      wf.ID,
		"message": "workflow updated",
	})
}

// Delete handles DELETE /workflows/:id
func (h *WorkflowHandler) Delete(c echo.Context) error {
	id := c.Param("id")

	if err := h.registry.Delete(id); err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": err.Error()})
	}

	h.logger.Info("workflow deleted", slog.String("id", id))
	return c.JSON(http.StatusOK, map[string]any{
		"id":      id,
		"message": "workflow deleted",
	})
}

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
	event.SetExtension("workflowid", workflowID) // For consistent querying
	_ = event.SetData(cloudevents.ApplicationJSON, engine.ExecutionStartedData{
		WorkflowID: workflowID,
		InputData:  req.Input,
	})

	// Publish to NATS (events reach Cassandra through engine processing)
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
