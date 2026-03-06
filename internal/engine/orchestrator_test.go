package engine_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/mock"

	cloudevents "github.com/cloudevents/sdk-go/v2"

	"github.com/cheriehsieh/orchestration/internal/engine"
	enginemocks "github.com/cheriehsieh/orchestration/internal/engine/mocks"
	"github.com/cheriehsieh/orchestration/internal/eventbus"
	eventbusmocks "github.com/cheriehsieh/orchestration/internal/eventbus/mocks"
	eventstoremocks "github.com/cheriehsieh/orchestration/internal/eventstore/mocks"
	"github.com/cheriehsieh/orchestration/internal/messaging"
)

func TestOrchestrator_HandleEvent(t *testing.T) {
	tests := []struct {
		name          string
		eventType     string
		eventData     any
		workflowID    string
		expectedCalls func(*eventstoremocks.MockEventStore, *eventbusmocks.MockPublisher, *enginemocks.MockWorkflowRepository)
		wantErr       bool
	}{
		{
			name:       "ExecutionStarted schedules start node",
			eventType:  engine.ExecutionStarted,
			workflowID: "test-workflow",
			eventData: engine.ExecutionStartedData{
				WorkflowID: "test-workflow",
				InputData:  map[string]any{"key": "value"},
			},
			expectedCalls: func(es *eventstoremocks.MockEventStore, pub *eventbusmocks.MockPublisher, repo *enginemocks.MockWorkflowRepository) {
				repo.EXPECT().GetByID(mock.Anything, "test-workflow").Return(testWorkflow(), nil)
				es.EXPECT().Append(mock.Anything, mock.MatchedBy(func(e cloudevents.Event) bool {
					return e.Type() == messaging.EventTypeRuntimeNodeScheduledV1
				})).Return(nil)
				pub.EXPECT().Publish(mock.Anything, mock.MatchedBy(func(e cloudevents.Event) bool {
					return e.Type() == messaging.EventTypeRuntimeNodeScheduledV1
				})).Return(nil)
			},
			wantErr: false,
		},
		{
			name:       "NodeExecutionExecuted routes based on output port",
			eventType:  engine.NodeExecutionExecuted,
			workflowID: "conditional-workflow",
			eventData: engine.NodeExecutionResultData{
				NodeID:     "check",
				OutputPort: engine.PortTrue,
				OutputData: map[string]any{"result": "ok"},
				RunIndex:   0,
			},
			expectedCalls: func(es *eventstoremocks.MockEventStore, pub *eventbusmocks.MockPublisher, repo *enginemocks.MockWorkflowRepository) {
				repo.EXPECT().GetByID(mock.Anything, "conditional-workflow").Return(conditionalWorkflow(), nil)
				// Should only schedule "on-true" node, not "on-false"
				es.EXPECT().Append(mock.Anything, mock.MatchedBy(func(e cloudevents.Event) bool {
					var data engine.NodeExecutionScheduledData
					_ = e.DataAs(&data)
					return e.Type() == messaging.EventTypeRuntimeNodeScheduledV1 && data.NodeID == "on-true"
				})).Return(nil)
				pub.EXPECT().Publish(mock.Anything, mock.MatchedBy(func(e cloudevents.Event) bool {
					return e.Type() == messaging.EventTypeRuntimeNodeScheduledV1
				})).Return(nil)
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			mockEventStore := eventstoremocks.NewMockEventStore(t)
			mockPublisher := eventbusmocks.NewMockPublisher(t)
			mockSubscriber := eventbusmocks.NewMockSubscriber(t)
			mockWorkflowRepo := enginemocks.NewMockWorkflowRepository(t)

			tt.expectedCalls(mockEventStore, mockPublisher, mockWorkflowRepo)

			event := cloudevents.NewEvent()
			event.SetID("event-1")
			event.SetSource(engine.EventSource)
			event.SetType(tt.eventType)
			event.SetSubject("exec-123")
			event.SetExtension("workflowid", tt.workflowID)
			_ = event.SetData(cloudevents.ApplicationJSON, tt.eventData)

			mockSubscriber.EXPECT().
				Subscribe(ctx, mock.AnythingOfType("eventbus.EventHandler")).
				RunAndReturn(func(ctx context.Context, handler eventbus.EventHandler) error {
					return handler(ctx, event)
				})

			orchestrator := engine.NewOrchestrator(mockEventStore, mockPublisher, mockSubscriber, mockWorkflowRepo)
			err := orchestrator.Start(ctx)

			if (err != nil) != tt.wantErr {
				t.Errorf("Orchestrator.Start() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func testWorkflow() *engine.Workflow {
	return &engine.Workflow{
		ID: "test-workflow",
		Nodes: []engine.Node{
			{ID: "start", Type: engine.StartNode, Name: "Start"},
			{ID: "action", Type: engine.ActionNode, Name: "Action"},
		},
		Connections: []engine.Connection{
			{FromNode: "start", ToNode: "action"},
		},
	}
}

func conditionalWorkflow() *engine.Workflow {
	return &engine.Workflow{
		ID: "conditional-workflow",
		Nodes: []engine.Node{
			{ID: "start", Type: engine.StartNode, Name: "Start"},
			{ID: "check", Type: engine.IfNode, Name: "Check Condition"},
			{ID: "on-true", Type: engine.ActionNode, Name: "On True"},
			{ID: "on-false", Type: engine.ActionNode, Name: "On False"},
		},
		Connections: []engine.Connection{
			{FromNode: "start", ToNode: "check"},
			{FromNode: "check", FromPort: engine.PortTrue, ToNode: "on-true"},
			{FromNode: "check", FromPort: engine.PortFalse, ToNode: "on-false"},
		},
	}
}
