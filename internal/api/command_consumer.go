package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	cloudevents "github.com/cloudevents/sdk-go/v2"

	"github.com/cheriehsieh/orchestration/internal/eventbus"
	"github.com/cheriehsieh/orchestration/internal/messaging"
)

// ExecutionStartCommand is the shared request shape used by REST and NATS execute paths.
type ExecutionStartCommand struct {
	WorkflowID     string
	ExecutionID    string
	IdempotencyKey string
	Input          map[string]any
	Producer       string
}

// ExecutionStartResult is the shared response from the execution-start path.
type ExecutionStartResult struct {
	ExecutionID string
	WorkflowID  string
	Status      string
	StartedAt   time.Time
}

// ExecutionStarter starts a workflow execution from a normalized request.
type ExecutionStarter interface {
	StartExecution(ctx context.Context, command ExecutionStartCommand) (*ExecutionStartResult, error)
}

// CommandConsumer bridges workflow.command.execute.v1 messages into the shared starter.
type CommandConsumer struct {
	subscriber eventbus.Subscriber
	starter    ExecutionStarter
	logger     *slog.Logger
}

// CommandConsumerOption configures a CommandConsumer.
type CommandConsumerOption func(*CommandConsumer)

type executeCommandPayload struct {
	WorkflowID string         `json:"workflowId"`
	Input      map[string]any `json:"input"`
}

// WithCommandConsumerLogger sets a custom logger.
func WithCommandConsumerLogger(logger *slog.Logger) CommandConsumerOption {
	return func(c *CommandConsumer) {
		c.logger = logger
	}
}

// NewCommandConsumer creates a consumer for external workflow execute commands.
func NewCommandConsumer(subscriber eventbus.Subscriber, starter ExecutionStarter, opts ...CommandConsumerOption) *CommandConsumer {
	consumer := &CommandConsumer{
		subscriber: subscriber,
		starter:    starter,
		logger:     slog.Default(),
	}
	for _, opt := range opts {
		opt(consumer)
	}

	return consumer
}

// Start begins consuming workflow.command.execute.v1 events from the configured subscriber.
func (c *CommandConsumer) Start(ctx context.Context) error {
	if c.subscriber == nil {
		return errors.New("command subscriber not configured")
	}

	return c.subscriber.Subscribe(ctx, c.Handle)
}

// Handle translates a command event into a shared execution-start request.
func (c *CommandConsumer) Handle(ctx context.Context, event cloudevents.Event) error {
	if c.starter == nil {
		return errors.New("execution starter not configured")
	}
	if event.Type() != messaging.EventTypeCommandExecuteV1 {
		return fmt.Errorf("unexpected event type %s", event.Type())
	}

	var payload executeCommandPayload
	if err := event.DataAs(&payload); err != nil {
		return fmt.Errorf("decode execute command: %w", err)
	}

	workflowID := eventExtensionString(event, "workflowid")
	if workflowID == "" {
		workflowID = payload.WorkflowID
	}
	if workflowID == "" {
		return errors.New("workflowid is required")
	}

	producer := eventExtensionString(event, "producer")
	if producer == "" {
		producer = event.Source()
	}

	command := ExecutionStartCommand{
		WorkflowID:     workflowID,
		ExecutionID:    eventExtensionString(event, "executionid"),
		IdempotencyKey: eventExtensionString(event, "idempotencykey"),
		Input:          payload.Input,
		Producer:       producer,
	}

	result, err := c.starter.StartExecution(ctx, command)
	if err != nil {
		return fmt.Errorf("start execution: %w", err)
	}

	c.logger.InfoContext(ctx, "workflow command bridged to execution start",
		slog.String("workflow_id", result.WorkflowID),
		slog.String("execution_id", result.ExecutionID),
		slog.String("producer", producer),
	)

	return nil
}

func eventExtensionString(event cloudevents.Event, key string) string {
	value, ok := event.Extensions()[key]
	if !ok {
		return ""
	}

	strValue, ok := value.(string)
	if ok {
		return strValue
	}

	return fmt.Sprint(value)
}
