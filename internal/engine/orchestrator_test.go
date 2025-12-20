package engine_test

import (
	"context"
	"testing"

	"github.com/cheriehsieh/orchestration/internal/engine"
	enginemocks "github.com/cheriehsieh/orchestration/internal/engine/mocks"
	"github.com/cheriehsieh/orchestration/internal/eventbus"
	eventbusmocks "github.com/cheriehsieh/orchestration/internal/eventbus/mocks"
	eventstoremocks "github.com/cheriehsieh/orchestration/internal/eventstore/mocks"
	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/stretchr/testify/mock"
)

func TestOrchestrator_HandleExecutionStarted_SchedulesStartNode(t *testing.T) {
	// Arrange
	ctx := context.Background()

	mockEventStore := eventstoremocks.NewMockEventStore(t)
	mockPublisher := eventbusmocks.NewMockPublisher(t)
	mockSubscriber := eventbusmocks.NewMockSubscriber(t)
	mockWorkflowRepo := enginemocks.NewMockWorkflowRepository(t)

	testWorkflow := &engine.Workflow{
		ID: "test-workflow",
		Nodes: []engine.Node{
			{ID: "start", Type: engine.StartNode, Name: "Start"},
			{ID: "action", Type: engine.ActionNode, Name: "Action"},
		},
		Connections: []engine.Connection{
			{FromNode: "start", ToNode: "action"},
		},
	}

	executionStartedEvent := cloudevents.NewEvent()
	executionStartedEvent.SetID("event-1")
	executionStartedEvent.SetSource(engine.EventSource)
	executionStartedEvent.SetType(engine.ExecutionStarted)
	executionStartedEvent.SetSubject("exec-123")
	executionStartedEvent.SetData(cloudevents.ApplicationJSON, engine.ExecutionStartedData{
		WorkflowID: "test-workflow",
		InputData:  map[string]interface{}{"key": "value"},
	})

	// Mock expectations
	mockWorkflowRepo.EXPECT().
		GetByID(ctx, "test-workflow").
		Return(testWorkflow, nil)

	mockEventStore.EXPECT().
		Append(ctx, mock.MatchedBy(func(e cloudevents.Event) bool {
			return e.Type() == engine.NodeExecutionScheduled
		})).
		Return(nil)

	mockPublisher.EXPECT().
		Publish(ctx, mock.MatchedBy(func(e cloudevents.Event) bool {
			return e.Type() == engine.NodeExecutionScheduled
		})).
		Return(nil)

	// Simulate subscription
	mockSubscriber.EXPECT().
		Subscribe(ctx, mock.AnythingOfType("eventbus.EventHandler")).
		RunAndReturn(func(ctx context.Context, handler eventbus.EventHandler) error {
			return handler(ctx, executionStartedEvent)
		})

	// Act
	orchestrator := engine.NewOrchestrator(mockEventStore, mockPublisher, mockSubscriber, mockWorkflowRepo)
	err := orchestrator.Start(ctx)

	// Assert
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
}

func TestOrchestrator_HandleNodeCompleted_SchedulesNextNode(t *testing.T) {
	// Arrange
	ctx := context.Background()

	mockEventStore := eventstoremocks.NewMockEventStore(t)
	mockPublisher := eventbusmocks.NewMockPublisher(t)
	mockSubscriber := eventbusmocks.NewMockSubscriber(t)
	mockWorkflowRepo := enginemocks.NewMockWorkflowRepository(t)

	testWorkflow := &engine.Workflow{
		ID: "test-workflow",
		Nodes: []engine.Node{
			{ID: "start", Type: engine.StartNode, Name: "Start"},
			{ID: "action", Type: engine.ActionNode, Name: "Action"},
		},
		Connections: []engine.Connection{
			{FromNode: "start", ToNode: "action"},
		},
	}

	nodeCompletedEvent := cloudevents.NewEvent()
	nodeCompletedEvent.SetID("event-2")
	nodeCompletedEvent.SetSource(engine.EventSource)
	nodeCompletedEvent.SetType(engine.NodeExecutionCompleted)
	nodeCompletedEvent.SetSubject("exec-123")
	nodeCompletedEvent.SetExtension("workflowid", "test-workflow")
	nodeCompletedEvent.SetData(cloudevents.ApplicationJSON, engine.NodeExecutionCompletedData{
		NodeID:     "start",
		OutputData: map[string]interface{}{"result": "ok"},
		RunIndex:   0,
	})

	// Mock expectations
	mockWorkflowRepo.EXPECT().
		GetByID(ctx, "test-workflow").
		Return(testWorkflow, nil)

	mockEventStore.EXPECT().
		Append(ctx, mock.MatchedBy(func(e cloudevents.Event) bool {
			return e.Type() == engine.NodeExecutionScheduled
		})).
		Return(nil)

	mockPublisher.EXPECT().
		Publish(ctx, mock.MatchedBy(func(e cloudevents.Event) bool {
			return e.Type() == engine.NodeExecutionScheduled
		})).
		Return(nil)

	mockSubscriber.EXPECT().
		Subscribe(ctx, mock.AnythingOfType("eventbus.EventHandler")).
		RunAndReturn(func(ctx context.Context, handler eventbus.EventHandler) error {
			return handler(ctx, nodeCompletedEvent)
		})

	// Act
	orchestrator := engine.NewOrchestrator(mockEventStore, mockPublisher, mockSubscriber, mockWorkflowRepo)
	err := orchestrator.Start(ctx)

	// Assert
	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}
}
