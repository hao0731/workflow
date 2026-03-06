package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/google/uuid"

	cloudevents "github.com/cloudevents/sdk-go/v2"

	"github.com/cheriehsieh/orchestration/internal/engine"
	"github.com/cheriehsieh/orchestration/internal/eventbus"
	"github.com/cheriehsieh/orchestration/internal/eventstore"
	"github.com/cheriehsieh/orchestration/internal/messaging"
)

const (
	// EventSource for scheduler events.
	EventSource        = "orchestration/scheduler"
	defaultNodeAttempt = 1
	nodeResultDedupTTL = 72 * time.Hour
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
	Attempt     int            `json:"attempt"`
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
	dlqPublisher  eventbus.Publisher
	dispatcher    WorkerDispatcher // Dispatches to worker subjects
	workflowRepo  engine.WorkflowRepository
	nodeValidator NodeValidator   // Optional: validates node types are registered
	resultCheck   ResultValidator // Validates worker-result payloads before handoff
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

// WithResultValidator sets a custom worker-result validator.
func WithResultValidator(v ResultValidator) SchedulerOption {
	return func(s *Scheduler) {
		s.resultCheck = v
	}
}

// WithValidationDLQPublisher configures the DLQ publisher for invalid worker results.
func WithValidationDLQPublisher(pub eventbus.Publisher) SchedulerOption {
	return func(s *Scheduler) {
		s.dlqPublisher = pub
	}
}

// SchedulerSubscriptionSubjects returns the subjects owned by the scheduler service.
func SchedulerSubscriptionSubjects() []string {
	return []string{
		messaging.SubjectRuntimeNodeScheduledV1,
		messaging.SubjectEventNodeExecutedV1,
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
		resultCheck:  RequiredResultValidator{},
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
			return s.publishNodeExecuted(
				ctx,
				event.Subject(),
				workflowID,
				nodeExecutionIdempotencyKey(event.Subject(), payload.NodeID, payload.RunIndex, defaultNodeAttempt),
				defaultNodeAttempt,
				engine.NodeExecutionResultData{
					NodeID:   payload.NodeID,
					RunIndex: payload.RunIndex,
					Error:    ErrNodeTypeNotRegistered.Error(),
				},
			)
		}
	}

	s.logger.InfoContext(ctx, "dispatching to worker",
		slog.String("node_id", payload.NodeID),
		slog.String("node_type", dispatchType),
	)

	// Dispatch to worker via the versioned command subject for this node type.
	dispatchEvent := s.newEvent(messaging.CommandNodeExecuteSubjectFromFullType(dispatchType), event.Subject())
	idempotencyKey := nodeExecutionIdempotencyKey(event.Subject(), payload.NodeID, payload.RunIndex, defaultNodeAttempt)
	dispatchEvent.SetExtension("workflowid", workflowID)
	dispatchEvent.SetExtension("executionid", event.Subject())
	dispatchEvent.SetExtension("nodeid", payload.NodeID)
	dispatchEvent.SetExtension("runindex", payload.RunIndex)
	dispatchEvent.SetExtension("attempt", defaultNodeAttempt)
	dispatchEvent.SetExtension("idempotencykey", idempotencyKey)
	dispatchEvent.SetExtension("producer", "scheduler")
	_ = dispatchEvent.SetData(cloudevents.ApplicationJSON, NodeDispatchData{
		ExecutionID: event.Subject(),
		NodeID:      payload.NodeID,
		NodeType:    dispatchType,
		WorkflowID:  workflowID,
		InputData:   payload.InputData,
		Parameters:  node.Parameters,
		RunIndex:    payload.RunIndex,
		Attempt:     defaultNodeAttempt,
	})

	return s.dispatcher.Dispatch(ctx, dispatchType, dispatchEvent)
}

func (s *Scheduler) handleResult(ctx context.Context, event cloudevents.Event) error {
	if event.Type() != messaging.EventTypeNodeExecutedV1 {
		return nil
	}

	var result NodeResultData
	if err := event.DataAs(&result); err != nil {
		return s.handleValidationFailure(ctx, event, fmt.Errorf("parse result data: %w", err))
	}
	if s.resultCheck != nil {
		if err := s.resultCheck.Validate(ctx, event, result); err != nil {
			return s.handleValidationFailure(ctx, event, err)
		}
	}

	workflowID, _ := event.Extensions()["workflowid"].(string)
	executionID, _ := event.Extensions()["executionid"].(string)
	if executionID == "" {
		executionID = result.ExecutionID
	}

	s.logger.DebugContext(ctx, "received worker result",
		slog.String("node_id", result.NodeID),
		slog.String("output_port", result.OutputPort),
	)

	attempt := eventExtensionInt(event, "attempt", defaultNodeAttempt)
	idempotencyKey := eventExtensionString(event, "idempotencykey")
	if idempotencyKey == "" {
		idempotencyKey = nodeExecutionIdempotencyKey(executionID, result.NodeID, result.RunIndex, attempt)
	}
	dedupRecordKey := nodeResultDedupRecordKey(executionID, result.NodeID, result.RunIndex, attempt, idempotencyKey)
	exists, err := s.eventStore.ExistsByDedupKey(ctx, dedupRecordKey)
	if err != nil {
		return fmt.Errorf("check node-result dedup key: %w", err)
	}
	if exists {
		s.logger.InfoContext(ctx, "duplicate worker result ignored",
			slog.String("execution_id", executionID),
			slog.String("node_id", result.NodeID),
			slog.String("dedup_key", dedupRecordKey),
		)
		return nil
	}
	if err := s.eventStore.SaveDedupRecord(ctx, dedupRecordKey, nodeResultDedupTTL); err != nil {
		return fmt.Errorf("save node-result dedup key: %w", err)
	}

	port := result.OutputPort
	if port == "" {
		port = engine.DefaultPort
	}

	return s.publishNodeExecuted(ctx, executionID, workflowID, idempotencyKey, attempt, engine.NodeExecutionResultData{
		NodeID:     result.NodeID,
		OutputPort: port,
		OutputData: result.OutputData,
		RunIndex:   result.RunIndex,
		Error:      result.Error,
	})
}

func (s *Scheduler) handleValidationFailure(ctx context.Context, event cloudevents.Event, validationErr error) error {
	s.logger.WarnContext(ctx, "worker result validation failed",
		slog.String("event_id", event.ID()),
		slog.String("subject", event.Subject()),
		slog.Any("error", validationErr),
	)

	if s.dlqPublisher == nil {
		return validationErr
	}

	dlqEvent, err := newValidationFailureEvent(event, validationErr)
	if err != nil {
		return fmt.Errorf("build validation failure dlq event: %w", err)
	}
	if err := s.dlqPublisher.Publish(ctx, dlqEvent); err != nil {
		return fmt.Errorf("publish validation failure dlq event: %w", err)
	}

	return nil
}

func (s *Scheduler) newEvent(eventType, subject string) cloudevents.Event {
	event := cloudevents.NewEvent()
	event.SetID(uuid.New().String())
	event.SetSource(EventSource)
	event.SetType(eventType)
	event.SetSubject(subject)
	return event
}

func (s *Scheduler) publishNodeExecuted(ctx context.Context, executionID, workflowID, idempotencyKey string, attempt int, result engine.NodeExecutionResultData) error {
	executedEvent := s.newEvent(engine.NodeExecutionExecuted, executionID)
	executedEvent.SetExtension("workflowid", workflowID)
	executedEvent.SetExtension("executionid", executionID)
	executedEvent.SetExtension("producer", "scheduler")
	executedEvent.SetExtension("nodeid", result.NodeID)
	executedEvent.SetExtension("runindex", result.RunIndex)
	executedEvent.SetExtension("attempt", attempt)
	if idempotencyKey != "" {
		executedEvent.SetExtension("idempotencykey", idempotencyKey)
	}
	_ = executedEvent.SetData(cloudevents.ApplicationJSON, result)

	if err := s.eventStore.Append(ctx, executedEvent); err != nil {
		return err
	}

	return s.publisher.Publish(ctx, executedEvent)
}

func nodeExecutionIdempotencyKey(executionID, nodeID string, runIndex, attempt int) string {
	return fmt.Sprintf("node:%s:%s:%d:%d:v1", executionID, nodeID, runIndex, attempt)
}

func nodeResultDedupRecordKey(executionID, nodeID string, runIndex, attempt int, idempotencyKey string) string {
	return fmt.Sprintf("%s|%s|%d|%d|%s", executionID, nodeID, runIndex, attempt, idempotencyKey)
}

func eventExtensionString(event cloudevents.Event, key string) string {
	value, ok := event.Extensions()[key]
	if !ok {
		return ""
	}

	if strValue, ok := value.(string); ok {
		return strValue
	}

	return fmt.Sprint(value)
}

func eventExtensionInt(event cloudevents.Event, key string, fallback int) int {
	value, ok := event.Extensions()[key]
	if !ok {
		return fallback
	}

	switch typed := value.(type) {
	case int:
		return typed
	case int32:
		return int(typed)
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case string:
		parsed, err := strconv.Atoi(typed)
		if err == nil {
			return parsed
		}
	}

	return fallback
}
