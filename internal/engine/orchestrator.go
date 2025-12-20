package engine

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/cheriehsieh/orchestration/internal/eventbus"
	"github.com/cheriehsieh/orchestration/internal/eventstore"
	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/google/uuid"
)

type Orchestrator struct {
	eventStore   eventstore.EventStore
	publisher    eventbus.Publisher
	subscriber   eventbus.Subscriber
	workflowRepo WorkflowRepository
	logger       *slog.Logger
}

func NewOrchestrator(
	es eventstore.EventStore,
	pub eventbus.Publisher,
	sub eventbus.Subscriber,
	repo WorkflowRepository,
	opts ...OrchestratorOption,
) *Orchestrator {
	o := &Orchestrator{
		eventStore:   es,
		publisher:    pub,
		subscriber:   sub,
		workflowRepo: repo,
		logger:       slog.Default(),
	}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// OrchestratorOption is a functional option for Orchestrator.
type OrchestratorOption func(*Orchestrator)

// WithOrchestratorLogger sets a custom logger.
func WithOrchestratorLogger(logger *slog.Logger) OrchestratorOption {
	return func(o *Orchestrator) {
		o.logger = logger
	}
}

func (o *Orchestrator) Start(ctx context.Context) error {
	return o.subscriber.Subscribe(ctx, func(ctx context.Context, event cloudevents.Event) error {
		return o.handleEvent(ctx, event)
	})
}

func (o *Orchestrator) handleEvent(ctx context.Context, event cloudevents.Event) error {
	switch event.Type() {
	case ExecutionStarted:
		return o.handleExecutionStarted(ctx, event)
	case NodeExecutionCompleted:
		return o.handleNodeExecutionCompleted(ctx, event)
	}
	return nil
}

func (o *Orchestrator) handleExecutionStarted(ctx context.Context, event cloudevents.Event) error {
	var payload ExecutionStartedData
	if err := event.DataAs(&payload); err != nil {
		return fmt.Errorf("failed to parse ExecutionStarted data: %w", err)
	}

	workflow, err := o.workflowRepo.GetByID(ctx, payload.WorkflowID)
	if err != nil {
		return fmt.Errorf("failed to get workflow: %w", err)
	}

	startNode := workflow.GetStartNode()
	if startNode == nil {
		return fmt.Errorf("no start node found for workflow")
	}

	o.logger.InfoContext(ctx, "execution started",
		slog.String("execution_id", event.Subject()),
		slog.String("workflow_id", payload.WorkflowID),
		slog.String("start_node", startNode.ID),
	)

	return o.scheduleNode(ctx, event.Subject(), startNode.ID, payload.InputData, 0, payload.WorkflowID)
}

func (o *Orchestrator) handleNodeExecutionCompleted(ctx context.Context, event cloudevents.Event) error {
	var payload NodeExecutionCompletedData
	if err := event.DataAs(&payload); err != nil {
		return fmt.Errorf("failed to parse NodeExecutionCompleted data: %w", err)
	}

	workflowID, _ := event.Extensions()["workflowid"].(string)
	if workflowID == "" {
		o.logger.WarnContext(ctx, "workflow_id not in extensions, skipping",
			slog.String("node_id", payload.NodeID),
		)
		return nil
	}

	workflow, err := o.workflowRepo.GetByID(ctx, workflowID)
	if err != nil {
		return fmt.Errorf("failed to get workflow: %w", err)
	}

	nextNodes := workflow.GetNextNodes(payload.NodeID)

	o.logger.DebugContext(ctx, "node completed, scheduling next",
		slog.String("completed_node", payload.NodeID),
		slog.Any("next_nodes", nextNodes),
	)

	for _, nodeID := range nextNodes {
		if err := o.scheduleNode(ctx, event.Subject(), nodeID, payload.OutputData, 0, workflowID); err != nil {
			return err
		}
	}

	if len(nextNodes) == 0 {
		o.logger.InfoContext(ctx, "execution completed",
			slog.String("execution_id", event.Subject()),
		)

		finishedEvent := o.newEvent(ExecutionCompleted, event.Subject())
		finishedEvent.SetExtension("workflowid", workflowID)
		_ = finishedEvent.SetData(cloudevents.ApplicationJSON, ExecutionCompletedData{
			FinalData: payload.OutputData,
		})

		if err := o.eventStore.Append(ctx, finishedEvent); err != nil {
			return err
		}
		return o.publisher.Publish(ctx, finishedEvent)
	}

	return nil
}

func (o *Orchestrator) scheduleNode(ctx context.Context, executionID, nodeID string, inputData map[string]any, runIndex int, workflowID string) error {
	event := o.newEvent(NodeExecutionScheduled, executionID)
	event.SetExtension("workflowid", workflowID)
	_ = event.SetData(cloudevents.ApplicationJSON, NodeExecutionScheduledData{
		NodeID:    nodeID,
		InputData: inputData,
		RunIndex:  runIndex,
	})

	if err := o.eventStore.Append(ctx, event); err != nil {
		return err
	}
	return o.publisher.Publish(ctx, event)
}

func (o *Orchestrator) newEvent(eventType, subject string) cloudevents.Event {
	event := cloudevents.NewEvent()
	event.SetID(uuid.New().String())
	event.SetSource(EventSource)
	event.SetType(eventType)
	event.SetSubject(subject)
	return event
}
