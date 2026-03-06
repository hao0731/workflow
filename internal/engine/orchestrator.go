package engine

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	cloudevents "github.com/cloudevents/sdk-go/v2"

	"github.com/cheriehsieh/orchestration/internal/eventbus"
	"github.com/cheriehsieh/orchestration/internal/eventstore"
)

type Orchestrator struct {
	eventStore   eventstore.EventStore
	publisher    eventbus.Publisher
	subscriber   eventbus.Subscriber
	workflowRepo WorkflowRepository
	joinManager  *JoinStateManager
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
		joinManager:  NewJoinStateManager(NewInMemoryJoinStateStore()), // Default
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

// WithJoinStateManager sets a custom join state manager.
func WithJoinStateManager(manager *JoinStateManager) OrchestratorOption {
	return func(o *Orchestrator) {
		o.joinManager = manager
	}
}

func (o *Orchestrator) Start(ctx context.Context) error {
	return o.subscriber.Subscribe(ctx, func(ctx context.Context, event cloudevents.Event) error {
		slog.InfoContext(ctx, "event received",
			slog.String("event_type", event.Type()),
			slog.String("subject", event.Subject()),
		)
		return o.handleEvent(ctx, event)
	})
}

func (o *Orchestrator) handleEvent(ctx context.Context, event cloudevents.Event) error {
	switch event.Type() {
	case ExecutionStarted:
		return o.handleExecutionStarted(ctx, event)
	case NodeExecutionExecuted:
		return o.handleNodeExecutionExecuted(ctx, event)
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

func (o *Orchestrator) handleNodeExecutionExecuted(ctx context.Context, event cloudevents.Event) error {
	var payload NodeExecutionResultData
	if err := event.DataAs(&payload); err != nil {
		return fmt.Errorf("failed to parse NodeExecutionExecuted data: %w", err)
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

	executionID := event.Subject()
	if payload.Error != "" {
		return o.emitExecutionFailed(ctx, executionID, workflowID, payload.Error)
	}

	// Route based on output port
	nextNodes := workflow.GetNextNodes(payload.NodeID, payload.OutputPort)

	o.logger.DebugContext(ctx, "node completed, routing to next",
		slog.String("completed_node", payload.NodeID),
		slog.String("output_port", payload.OutputPort),
		slog.Any("next_nodes", nextNodes),
	)

	for _, nodeID := range nextNodes {
		node := workflow.GetNode(nodeID)
		if node == nil {
			continue
		}

		// Handle join nodes specially
		if IsJoinNode(node) {
			if err := o.handleJoinNode(ctx, executionID, workflowID, workflow, node, payload.NodeID, payload.OutputData); err != nil {
				return err
			}
		} else {
			// Regular node - schedule immediately
			if err := o.scheduleNode(ctx, executionID, nodeID, payload.OutputData, 0, workflowID); err != nil {
				return err
			}
		}
	}

	if len(nextNodes) == 0 {
		return o.emitExecutionCompleted(ctx, executionID, workflowID, payload.OutputData)
	}

	return nil
}

func (o *Orchestrator) handleJoinNode(ctx context.Context, executionID, workflowID string, workflow *Workflow, joinNode *Node, fromNodeID string, outputData map[string]any) error {
	predecessors := workflow.GetPredecessors(joinNode.ID)
	toPort := FindToPort(workflow, fromNodeID, joinNode.ID)

	o.logger.DebugContext(ctx, "processing join node",
		slog.String("join_node", joinNode.ID),
		slog.String("from_node", fromNodeID),
		slog.String("to_port", toPort),
		slog.Any("predecessors", predecessors),
	)

	// Process join logic via manager
	complete, combinedInputs, err := o.joinManager.ProcessJoin(ctx, executionID, joinNode.ID, workflowID, predecessors, fromNodeID, toPort, outputData)
	if err != nil {
		return err
	}

	if !complete {
		o.logger.DebugContext(ctx, "join node waiting for more inputs",
			slog.String("join_node", joinNode.ID),
		)
		return nil
	}

	o.logger.InfoContext(ctx, "join node ready, all predecessors completed",
		slog.String("join_node", joinNode.ID),
		slog.Any("combined_inputs", combinedInputs),
	)

	// All predecessors done - schedule the join node
	return o.scheduleNode(ctx, executionID, joinNode.ID, combinedInputs, 0, workflowID)
}

func (o *Orchestrator) emitExecutionCompleted(ctx context.Context, executionID, workflowID string, finalData map[string]any) error {
	o.logger.InfoContext(ctx, "execution completed",
		slog.String("execution_id", executionID),
	)

	finishedEvent := o.newEvent(ExecutionCompleted, executionID)
	finishedEvent.SetExtension("workflowid", workflowID)
	_ = finishedEvent.SetData(cloudevents.ApplicationJSON, ExecutionCompletedData{
		FinalData: finalData,
	})

	if err := o.eventStore.Append(ctx, finishedEvent); err != nil {
		return err
	}
	return o.publisher.Publish(ctx, finishedEvent)
}

func (o *Orchestrator) emitExecutionFailed(ctx context.Context, executionID, workflowID, message string) error {
	o.logger.InfoContext(ctx, "execution failed",
		slog.String("execution_id", executionID),
		slog.String("error", message),
	)

	failedEvent := o.newEvent(ExecutionFailed, executionID)
	failedEvent.SetExtension("workflowid", workflowID)
	_ = failedEvent.SetData(cloudevents.ApplicationJSON, ExecutionFailedData{
		Error: message,
	})

	if err := o.eventStore.Append(ctx, failedEvent); err != nil {
		return err
	}

	return o.publisher.Publish(ctx, failedEvent)
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
