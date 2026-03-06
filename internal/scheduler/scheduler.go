package scheduler

import (
	"context"
	"errors"
	"log/slog"

	"github.com/google/uuid"

	cloudevents "github.com/cloudevents/sdk-go/v2"

	"github.com/cheriehsieh/orchestration/internal/engine"
	"github.com/cheriehsieh/orchestration/internal/eventbus"
	"github.com/cheriehsieh/orchestration/internal/eventstore"
)

const (
	// EventSource for scheduler events.
	EventSource = "orchestration/scheduler"

	// Event types for worker dispatch.
	NodeDispatch = "orchestration.node.dispatch"
	NodeResult   = "orchestration.node.result"
)

var (
	ErrNodeTypeNotRegistered = errors.New("node type not registered")
)

// NodeDispatchData is sent to workers via node-type-specific subjects.
type NodeDispatchData struct {
	ExecutionID string         `json:"execution_id"`
	NodeID      string         `json:"node_id"`
	NodeType    string         `json:"node_type"`
	WorkflowID  string         `json:"workflow_id"`
	InputData   map[string]any `json:"input_data,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
	RunIndex    int            `json:"run_index"`
}

// NodeResultData is returned by workers.
type NodeResultData struct {
	ExecutionID string         `json:"execution_id"`
	NodeID      string         `json:"node_id"`
	OutputPort  string         `json:"output_port"`
	OutputData  map[string]any `json:"output_data,omitempty"`
	Error       string         `json:"error,omitempty"`
	RunIndex    int            `json:"run_index"`
}

// WorkerDispatcher publishes events to node-type-specific subjects.
type WorkerDispatcher interface {
	Dispatch(ctx context.Context, nodeType string, event cloudevents.Event) error
}

// Scheduler coordinates between Orchestrator and Workers.
type Scheduler struct {
	eventStore    eventstore.EventStore
	publisher     eventbus.Publisher  // Publishes to orchestrator subject
	subscriber    eventbus.Subscriber // Subscribes to scheduler subject
	resultSub     eventbus.Subscriber // Subscribes to result subject
	dispatcher    WorkerDispatcher    // Dispatches to worker subjects
	workflowRepo  engine.WorkflowRepository
	nodeValidator NodeValidator // Optional: validates node types are registered
	logger        *slog.Logger
}

// SchedulerOption is a functional option for Scheduler.
type SchedulerOption func(*Scheduler)

// WithSchedulerLogger sets a custom logger.
func WithSchedulerLogger(logger *slog.Logger) SchedulerOption {
	return func(s *Scheduler) {
		s.logger = logger
	}
}

// WithNodeValidator sets a node validator for registry checks.
func WithNodeValidator(v NodeValidator) SchedulerOption {
	return func(s *Scheduler) {
		s.nodeValidator = v
	}
}

func NewScheduler(
	es eventstore.EventStore,
	pub eventbus.Publisher,
	sub eventbus.Subscriber,
	resultSub eventbus.Subscriber,
	dispatcher WorkerDispatcher,
	repo engine.WorkflowRepository,
	opts ...SchedulerOption,
) *Scheduler {
	s := &Scheduler{
		eventStore:   es,
		publisher:    pub,
		subscriber:   sub,
		resultSub:    resultSub,
		dispatcher:   dispatcher,
		workflowRepo: repo,
		logger:       slog.Default(),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Start begins listening for events from Orchestrator and Workers.
func (s *Scheduler) Start(ctx context.Context) error {
	// Start result listener in goroutine
	go func() {
		if err := s.resultSub.Subscribe(ctx, s.handleResult); err != nil {
			s.logger.ErrorContext(ctx, "result subscriber error", slog.Any("error", err))
		}
	}()

	// Listen for scheduled events from Orchestrator
	return s.subscriber.Subscribe(ctx, s.handleScheduled)
}

func (s *Scheduler) handleScheduled(ctx context.Context, event cloudevents.Event) error {
	if event.Type() != engine.NodeExecutionScheduled {
		return nil
	}

	var payload engine.NodeExecutionScheduledData
	if err := event.DataAs(&payload); err != nil {
		s.logger.ErrorContext(ctx, "failed to parse scheduled data", slog.Any("error", err))
		return nil
	}

	workflowID, _ := event.Extensions()["workflowid"].(string)
	if workflowID == "" {
		s.logger.WarnContext(ctx, "workflowid not in extensions", slog.String("node_id", payload.NodeID))
		return nil
	}

	workflow, err := s.workflowRepo.GetByID(ctx, workflowID)
	if err != nil {
		return err
	}

	node := workflow.GetNode(payload.NodeID)
	if node == nil {
		s.logger.WarnContext(ctx, "node not found", slog.String("node_id", payload.NodeID))
		return nil
	}

	// Validate node type is registered (if validator is set)
	dispatchType := node.DispatchType()
	if s.nodeValidator != nil {
		if !s.nodeValidator.Exists(ctx, dispatchType) {
			s.logger.ErrorContext(ctx, "node type not registered",
				slog.String("node_type", dispatchType),
			)
			// Emit failed event
			failedEvent := s.newEvent(engine.NodeExecutionFailed, event.Subject())
			failedEvent.SetExtension("workflowid", workflowID)
			_ = failedEvent.SetData(cloudevents.ApplicationJSON, engine.NodeExecutionFailedData{
				NodeID:   payload.NodeID,
				RunIndex: payload.RunIndex,
				Error:    ErrNodeTypeNotRegistered.Error(),
			})
			if err := s.eventStore.Append(ctx, failedEvent); err != nil {
				return err
			}
			return s.publisher.Publish(ctx, failedEvent)
		}
	}

	// 1. Emit NodeExecutionStarted to Orchestrator
	startedEvent := s.newEvent(engine.NodeExecutionStarted, event.Subject())
	startedEvent.SetExtension("workflowid", workflowID)
	_ = startedEvent.SetData(cloudevents.ApplicationJSON, engine.NodeExecutionStartedData{
		NodeID:   payload.NodeID,
		RunIndex: payload.RunIndex,
	})

	if err := s.eventStore.Append(ctx, startedEvent); err != nil {
		return err
	}
	if err := s.publisher.Publish(ctx, startedEvent); err != nil {
		return err
	}

	s.logger.InfoContext(ctx, "dispatching to worker",
		slog.String("node_id", payload.NodeID),
		slog.String("node_type", dispatchType),
	)

	// 2. Dispatch to worker via node-type-specific subject
	dispatchEvent := s.newEvent(NodeDispatch, event.Subject())
	dispatchEvent.SetExtension("workflowid", workflowID)
	_ = dispatchEvent.SetData(cloudevents.ApplicationJSON, NodeDispatchData{
		ExecutionID: event.Subject(),
		NodeID:      payload.NodeID,
		NodeType:    dispatchType,
		WorkflowID:  workflowID,
		InputData:   payload.InputData,
		Parameters:  node.Parameters,
		RunIndex:    payload.RunIndex,
	})

	return s.dispatcher.Dispatch(ctx, dispatchType, dispatchEvent)
}

func (s *Scheduler) handleResult(ctx context.Context, event cloudevents.Event) error {
	if event.Type() != NodeResult {
		return nil
	}

	var result NodeResultData
	if err := event.DataAs(&result); err != nil {
		s.logger.ErrorContext(ctx, "failed to parse result data", slog.Any("error", err))
		return nil
	}

	workflowID, _ := event.Extensions()["workflowid"].(string)

	s.logger.DebugContext(ctx, "received worker result",
		slog.String("node_id", result.NodeID),
		slog.String("output_port", result.OutputPort),
	)

	// Forward completion/failure to Orchestrator
	if result.Error != "" {
		failedEvent := s.newEvent(engine.NodeExecutionFailed, result.ExecutionID)
		failedEvent.SetExtension("workflowid", workflowID)
		_ = failedEvent.SetData(cloudevents.ApplicationJSON, engine.NodeExecutionFailedData{
			NodeID:   result.NodeID,
			RunIndex: result.RunIndex,
			Error:    result.Error,
		})

		if err := s.eventStore.Append(ctx, failedEvent); err != nil {
			return err
		}
		return s.publisher.Publish(ctx, failedEvent)
	}

	port := result.OutputPort
	if port == "" {
		port = engine.DefaultPort
	}

	completedEvent := s.newEvent(engine.NodeExecutionCompleted, result.ExecutionID)
	completedEvent.SetExtension("workflowid", workflowID)
	_ = completedEvent.SetData(cloudevents.ApplicationJSON, engine.NodeExecutionCompletedData{
		NodeID:     result.NodeID,
		OutputPort: port,
		OutputData: result.OutputData,
		RunIndex:   result.RunIndex,
	})

	if err := s.eventStore.Append(ctx, completedEvent); err != nil {
		return err
	}
	return s.publisher.Publish(ctx, completedEvent)
}

func (s *Scheduler) newEvent(eventType, subject string) cloudevents.Event {
	event := cloudevents.NewEvent()
	event.SetID(uuid.New().String())
	event.SetSource(EventSource)
	event.SetType(eventType)
	event.SetSubject(subject)
	return event
}
