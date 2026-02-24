package scheduler

import (
	"context"
	"log/slog"
	"time"

	cloudevents "github.com/cloudevents/sdk-go/v2"

	"github.com/cheriehsieh/orchestration/internal/engine"
	"github.com/cheriehsieh/orchestration/internal/eventbus"
	"github.com/cheriehsieh/orchestration/internal/eventstore"
)

// ExecutionLinkHandler handles ExecutionStarted events to maintain parent-child links.
type ExecutionLinkHandler struct {
	executionStore eventstore.ExecutionStore
	subscriber     eventbus.Subscriber
	logger         *slog.Logger
}

// ExecutionLinkHandlerOption is a functional option.
type ExecutionLinkHandlerOption func(*ExecutionLinkHandler)

// WithLinkHandlerLogger sets a custom logger.
func WithLinkHandlerLogger(logger *slog.Logger) ExecutionLinkHandlerOption {
	return func(h *ExecutionLinkHandler) {
		h.logger = logger
	}
}

// WithLinkHandlerSubscriber sets the event subscriber.
func WithLinkHandlerSubscriber(sub eventbus.Subscriber) ExecutionLinkHandlerOption {
	return func(h *ExecutionLinkHandler) {
		h.subscriber = sub
	}
}

// NewExecutionLinkHandler creates a new handler for execution linking.
func NewExecutionLinkHandler(store eventstore.ExecutionStore, opts ...ExecutionLinkHandlerOption) *ExecutionLinkHandler {
	h := &ExecutionLinkHandler{
		executionStore: store,
		logger:         slog.Default(),
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// Start begins listening for ExecutionStarted events.
func (h *ExecutionLinkHandler) Start(ctx context.Context) error {
	if h.subscriber == nil {
		return nil
	}
	h.logger.Info("execution link handler started")
	return h.subscriber.Subscribe(ctx, func(ctx context.Context, event cloudevents.Event) error {
		return h.Handle(ctx, event)
	})
}

// Handle processes an ExecutionStarted event and creates/links executions.
func (h *ExecutionLinkHandler) Handle(ctx context.Context, event cloudevents.Event) error {
	if event.Type() != engine.ExecutionStarted {
		return nil
	}

	executionID := event.Subject()
	workflowID, _ := event.Extensions()["workflowid"].(string)
	parentExecutionID, _ := event.Extensions()["parentexecutionid"].(string)
	triggeredByEvent, _ := event.Extensions()["triggeredbyevent"].(string)

	// Create execution record
	exec := &eventstore.Execution{
		ID:                executionID,
		WorkflowID:        workflowID,
		Status:            eventstore.StatusRunning,
		StartedAt:         event.Time(),
		ParentExecutionID: parentExecutionID,
		TriggeredByEvent:  triggeredByEvent,
	}

	if exec.StartedAt.IsZero() {
		exec.StartedAt = time.Now()
	}

	if err := h.executionStore.Create(ctx, exec); err != nil {
		h.logger.Error("failed to create execution record",
			slog.String("execution_id", executionID),
			slog.Any("error", err),
		)
		return err
	}

	// If has parent, update parent's child list
	if parentExecutionID != "" {
		if err := h.executionStore.AddChildExecution(ctx, parentExecutionID, executionID); err != nil {
			h.logger.Warn("failed to update parent child list",
				slog.String("parent_id", parentExecutionID),
				slog.String("child_id", executionID),
				slog.Any("error", err),
			)
			// Non-fatal: linking is observability, not critical path
		}

		h.logger.Info("linked child execution to parent",
			slog.String("child_id", executionID),
			slog.String("parent_id", parentExecutionID),
			slog.String("triggered_by", triggeredByEvent),
		)
	}

	return nil
}
