package engine

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	cloudevents "github.com/cloudevents/sdk-go/v2"

	"github.com/cheriehsieh/orchestration/internal/eventbus"
	"github.com/cheriehsieh/orchestration/internal/eventstore"
)

// NodeResult is the result of a node execution.
type NodeResult struct {
	Output map[string]any
	Port   string // Output port name; empty = "default"
}

// NodeHandler is the function signature for node execution logic.
// Returns output data, output port name, and error.
type NodeHandler func(ctx context.Context, input, parameters map[string]any) (NodeResult, error)

type Worker struct {
	eventStore   eventstore.EventStore
	publisher    eventbus.Publisher
	subscriber   eventbus.Subscriber
	workflowRepo WorkflowRepository
	registry     map[NodeType]NodeHandler
	logger       *slog.Logger
}

func NewWorker(
	es eventstore.EventStore,
	pub eventbus.Publisher,
	sub eventbus.Subscriber,
	repo WorkflowRepository,
	opts ...WorkerOption,
) *Worker {
	w := &Worker{
		eventStore:   es,
		publisher:    pub,
		subscriber:   sub,
		workflowRepo: repo,
		registry:     make(map[NodeType]NodeHandler),
		logger:       slog.Default(),
	}
	for _, opt := range opts {
		opt(w)
	}
	return w
}

// WorkerOption is a functional option for Worker.
type WorkerOption func(*Worker)

// WithWorkerLogger sets a custom logger.
func WithWorkerLogger(logger *slog.Logger) WorkerOption {
	return func(w *Worker) {
		w.logger = logger
	}
}

func (w *Worker) Register(nodeType NodeType, handler NodeHandler) {
	w.registry[nodeType] = handler
}

func (w *Worker) Start(ctx context.Context) error {
	return w.subscriber.Subscribe(ctx, func(ctx context.Context, event cloudevents.Event) error {
		if event.Type() == NodeExecutionScheduled {
			return w.handleNodeExecutionScheduled(ctx, event)
		}
		return nil
	})
}

func (w *Worker) handleNodeExecutionScheduled(ctx context.Context, event cloudevents.Event) error {
	var payload NodeExecutionScheduledData
	if err := event.DataAs(&payload); err != nil {
		w.logger.ErrorContext(ctx, "failed to parse NodeExecutionScheduled data", slog.Any("error", err))
		return nil
	}

	workflowID, _ := event.Extensions()["workflowid"].(string)
	if workflowID == "" {
		w.logger.WarnContext(ctx, "workflowid not in extensions", slog.String("node_id", payload.NodeID))
		return nil
	}

	workflow, err := w.workflowRepo.GetByID(ctx, workflowID)
	if err != nil {
		return err
	}

	node := workflow.GetNode(payload.NodeID)
	if node == nil {
		w.logger.WarnContext(ctx, "node not found in workflow", slog.String("node_id", payload.NodeID))
		return nil
	}

	handler, ok := w.registry[node.Type]
	if !ok {
		w.logger.WarnContext(ctx, "no handler registered", slog.String("node_type", string(node.Type)))
		return nil
	}

	// 1. Emit NodeExecutionStarted
	startedEvent := w.newEvent(NodeExecutionStarted, event.Subject())
	startedEvent.SetExtension("workflowid", workflowID)
	_ = startedEvent.SetData(cloudevents.ApplicationJSON, NodeExecutionStartedData{
		NodeID:   payload.NodeID,
		RunIndex: payload.RunIndex,
	})

	if appendErr := w.eventStore.Append(ctx, startedEvent); appendErr != nil {
		return appendErr
	}
	if publishErr := w.publisher.Publish(ctx, startedEvent); publishErr != nil {
		return publishErr
	}

	w.logger.InfoContext(ctx, "executing node",
		slog.String("node_id", payload.NodeID),
		slog.String("node_type", string(node.Type)),
	)

	// 2. Execute Node Logic
	result, err := handler(ctx, payload.InputData, node.Parameters)

	// 3. Emit Completion/Failure
	if err != nil {
		w.logger.ErrorContext(ctx, "node execution failed",
			slog.String("node_id", payload.NodeID),
			slog.Any("error", err),
		)

		failedEvent := w.newEvent(NodeExecutionFailed, event.Subject())
		failedEvent.SetExtension("workflowid", workflowID)
		_ = failedEvent.SetData(cloudevents.ApplicationJSON, NodeExecutionFailedData{
			NodeID:   payload.NodeID,
			RunIndex: payload.RunIndex,
			Error:    err.Error(),
		})

		if appendErr := w.eventStore.Append(ctx, failedEvent); appendErr != nil {
			return appendErr
		}
		return w.publisher.Publish(ctx, failedEvent)
	}

	// Normalize port name
	port := result.Port
	if port == "" {
		port = DefaultPort
	}

	w.logger.DebugContext(ctx, "node completed",
		slog.String("node_id", payload.NodeID),
		slog.String("output_port", port),
	)

	completedEvent := w.newEvent(NodeExecutionCompleted, event.Subject())
	completedEvent.SetExtension("workflowid", workflowID)
	_ = completedEvent.SetData(cloudevents.ApplicationJSON, NodeExecutionCompletedData{
		NodeID:     payload.NodeID,
		OutputPort: port,
		OutputData: result.Output,
		RunIndex:   payload.RunIndex,
	})

	if err := w.eventStore.Append(ctx, completedEvent); err != nil {
		return err
	}
	return w.publisher.Publish(ctx, completedEvent)
}

func (w *Worker) newEvent(eventType, subject string) cloudevents.Event {
	event := cloudevents.NewEvent()
	event.SetID(uuid.New().String())
	event.SetSource(EventSource)
	event.SetType(eventType)
	event.SetSubject(subject)
	return event
}
