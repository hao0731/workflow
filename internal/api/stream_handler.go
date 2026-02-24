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
