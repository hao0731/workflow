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

// Start begins listening for execution lifecycle events.
func (h *ExecutionLinkHandler) Start(ctx context.Context) error {
	if h.subscriber == nil {
		return nil
	}
	h.logger.Info("execution link handler started")
	return h.subscriber.Subscribe(ctx, func(ctx context.Context, event cloudevents.Event) error {
		return h.Handle(ctx, event)
	})
}

// Handle processes execution lifecycle events and updates the executions read model.
func (h *ExecutionLinkHandler) Handle(ctx context.Context, event cloudevents.Event) error {
	switch event.Type() {
	case engine.ExecutionStarted:
		return h.handleStarted(ctx, event)
	case engine.ExecutionCompleted:
		return h.handleCompleted(ctx, event)
	case engine.ExecutionFailed:
		return h.handleFailed(ctx, event)
	default:
		return nil
	}
}

func (h *ExecutionLinkHandler) handleStarted(ctx context.Context, event cloudevents.Event) error {
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

func (h *ExecutionLinkHandler) handleCompleted(ctx context.Context, event cloudevents.Event) error {
	executionID := event.Subject()
	completedAt := event.Time()
	if completedAt.IsZero() {
		completedAt = time.Now()
	}

	if err := h.executionStore.UpdateStatusWithTime(ctx, executionID, eventstore.StatusCompleted, completedAt); err != nil {
		h.logger.Error("failed to update execution status to completed",
			slog.String("execution_id", executionID),
			slog.Any("error", err),
		)
		return err
	}

	h.logger.Info("execution completed",
		slog.String("execution_id", executionID),
	)
	return nil
}

func (h *ExecutionLinkHandler) handleFailed(ctx context.Context, event cloudevents.Event) error {
	executionID := event.Subject()
	failedAt := event.Time()
	if failedAt.IsZero() {
		failedAt = time.Now()
	}

	if err := h.executionStore.UpdateStatusWithTime(ctx, executionID, eventstore.StatusFailed, failedAt); err != nil {
		h.logger.Error("failed to update execution status to failed",
			slog.String("execution_id", executionID),
			slog.Any("error", err),
		)
		return err
	}

	h.logger.Info("execution failed",
		slog.String("execution_id", executionID),
	)
	return nil
}
