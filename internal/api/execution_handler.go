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
