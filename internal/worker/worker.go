package worker

import (
	"context"
	"log/slog"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"

	cloudevents "github.com/cloudevents/sdk-go/v2"

	"github.com/cheriehsieh/orchestration/internal/engine"
	"github.com/cheriehsieh/orchestration/internal/eventbus"
	"github.com/cheriehsieh/orchestration/internal/marketplace"
	"github.com/cheriehsieh/orchestration/internal/messaging"
	"github.com/cheriehsieh/orchestration/internal/scheduler"
)

// Handler is the function signature for node execution logic.
// Third-party developers implement this interface.
type Handler func(ctx context.Context, input, parameters map[string]any) (engine.NodeResult, error)

// Worker listens to a node-type-specific subject and executes handlers.
// This is the template for third-party worker implementations.
type Worker struct {
	nodeType   engine.NodeType
	handler    Handler
	subscriber eventbus.Subscriber
	publisher  eventbus.Publisher // Publishes to results subject
	logger     *slog.Logger
}

// ManagedWorker captures the subject binding for a runnable first-party worker.
type ManagedWorker struct {
	NodeType     engine.NodeType
	Subject      string
	ConsumerName string
	Service      *Worker
}

// WorkerOption is a functional option for Worker.
type WorkerOption func(*Worker)

// WithWorkerLogger sets a custom logger.
func WithWorkerLogger(logger *slog.Logger) WorkerOption {
	return func(w *Worker) {
		w.logger = logger
	}
}

// NewWorker creates a worker that handles a specific node type.
func NewWorker(
	nodeType engine.NodeType,
	handler Handler,
	sub eventbus.Subscriber,
	pub eventbus.Publisher,
	opts ...WorkerOption,
) *Worker {
	w := &Worker{
		nodeType:   nodeType,
		handler:    handler,
		subscriber: sub,
		publisher:  pub,
		logger:     slog.Default(),
	}
	for _, opt := range opts {
		opt(w)
	}
	return w
}

// FirstPartyWorkerSubjects returns the command subjects owned by repository-provided workers.
func FirstPartyWorkerSubjects() []string {
	return []string{
		messaging.CommandNodeExecuteSubjectFromFullType(string(engine.StartNode)),
		messaging.CommandNodeExecuteSubjectFromFullType(string(engine.JoinNode)),
		messaging.CommandNodeExecuteSubjectFromFullType(string(engine.PublishEvent)),
	}
}

// NewFirstPartyWorkers constructs the built-in workers that ship with this repository.
func NewFirstPartyWorkers(
	js nats.JetStreamContext,
	publisher eventbus.Publisher,
	eventRegistry marketplace.EventRegistry,
	logger *slog.Logger,
) []*ManagedWorker {
	if logger == nil {
		logger = slog.Default()
	}

	type workerSpec struct {
		nodeType     engine.NodeType
		consumerName string
		handler      Handler
	}

	specs := []workerSpec{
		{
			nodeType:     engine.StartNode,
			consumerName: "worker-firstparty-start",
			handler: func(_ context.Context, input, _ map[string]any) (engine.NodeResult, error) {
				return engine.NodeResult{Output: input, Port: engine.DefaultPort}, nil
			},
		},
		{
			nodeType:     engine.JoinNode,
			consumerName: "worker-firstparty-join",
			handler: func(_ context.Context, input, _ map[string]any) (engine.NodeResult, error) {
				return engine.NodeResult{Output: input, Port: engine.DefaultPort}, nil
			},
		},
		{
			nodeType:     engine.PublishEvent,
			consumerName: "worker-firstparty-publish",
			handler:      engine.NewPublishEventWorkerHandler(js, eventRegistry, logger),
		},
	}

	workers := make([]*ManagedWorker, 0, len(specs))
	for _, spec := range specs {
		subject := messaging.CommandNodeExecuteSubjectFromFullType(string(spec.nodeType))
		workers = append(workers, &ManagedWorker{
			NodeType:     spec.nodeType,
			Subject:      subject,
			ConsumerName: spec.consumerName,
			Service: NewWorker(
				spec.nodeType,
				spec.handler,
				eventbus.NewNATSEventBus(js, subject, spec.consumerName, eventbus.WithLogger(logger)),
				publisher,
				WithWorkerLogger(logger),
			),
		})
	}

	return workers
}

// Start begins listening for dispatch events.
func (w *Worker) Start(ctx context.Context) error {
	w.logger.Info("worker started",
		slog.String("node_type", string(w.nodeType)),
	)

	return w.subscriber.Subscribe(ctx, func(ctx context.Context, event cloudevents.Event) error {
		if event.Type() != messaging.CommandNodeExecuteSubjectFromFullType(string(w.nodeType)) {
			return nil
		}
		return w.handleDispatch(ctx, event)
	})
}

func (w *Worker) handleDispatch(ctx context.Context, event cloudevents.Event) error {
	var dispatch scheduler.NodeDispatchData
	if err := event.DataAs(&dispatch); err != nil {
		w.logger.ErrorContext(ctx, "failed to parse dispatch data", slog.Any("error", err))
		return nil
	}

	// Verify this worker handles this node type
	if dispatch.NodeType != string(w.nodeType) {
		return nil
	}

	workflowID, _ := event.Extensions()["workflowid"].(string)

	w.logger.InfoContext(ctx, "executing node",
		slog.String("node_id", dispatch.NodeID),
		slog.String("execution_id", dispatch.ExecutionID),
	)

	// Inject execution context into input so handlers can access it
	input := dispatch.InputData
	if input == nil {
		input = make(map[string]any)
	}
	input["_execution_id"] = dispatch.ExecutionID

	// Execute the handler
	result, err := w.handler(ctx, input, dispatch.Parameters)

	// Build result event
	resultEvent := w.newEvent(messaging.EventTypeNodeExecutedV1, dispatch.ExecutionID)
	resultEvent.SetExtension("workflowid", workflowID)
	resultEvent.SetExtension("executionid", dispatch.ExecutionID)
	resultEvent.SetExtension("producer", "worker/"+string(w.nodeType))
	resultEvent.SetExtension("nodeid", dispatch.NodeID)
	resultEvent.SetExtension("runindex", dispatch.RunIndex)
	resultEvent.SetExtension("attempt", 1)
	if idempotencyKey, ok := event.Extensions()["idempotencykey"].(string); ok && idempotencyKey != "" {
		resultEvent.SetExtension("idempotencykey", idempotencyKey)
	}

	if err != nil {
		w.logger.ErrorContext(ctx, "node execution failed",
			slog.String("node_id", dispatch.NodeID),
			slog.Any("error", err),
		)

		_ = resultEvent.SetData(cloudevents.ApplicationJSON, scheduler.NodeResultData{
			ExecutionID: dispatch.ExecutionID,
			NodeID:      dispatch.NodeID,
			RunIndex:    dispatch.RunIndex,
			Error:       err.Error(),
		})
	} else {
		port := result.Port
		if port == "" {
			port = engine.DefaultPort
		}

		w.logger.DebugContext(ctx, "node execution completed",
			slog.String("node_id", dispatch.NodeID),
			slog.String("output_port", port),
		)

		_ = resultEvent.SetData(cloudevents.ApplicationJSON, scheduler.NodeResultData{
			ExecutionID: dispatch.ExecutionID,
			NodeID:      dispatch.NodeID,
			OutputPort:  port,
			OutputData:  result.Output,
			RunIndex:    dispatch.RunIndex,
		})
	}

	return w.publisher.Publish(ctx, resultEvent)
}

func (w *Worker) newEvent(eventType, subject string) cloudevents.Event {
	event := cloudevents.NewEvent()
	event.SetID(uuid.New().String())
	event.SetSource("worker/" + string(w.nodeType))
	event.SetType(eventType)
	event.SetSubject(subject)
	return event
}
