package engine_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	cloudevents "github.com/cloudevents/sdk-go/v2"

	"github.com/cheriehsieh/orchestration/internal/engine"
	enginemocks "github.com/cheriehsieh/orchestration/internal/engine/mocks"
	"github.com/cheriehsieh/orchestration/internal/eventbus"
	eventbusmocks "github.com/cheriehsieh/orchestration/internal/eventbus/mocks"
	"github.com/cheriehsieh/orchestration/internal/messaging"
)

type captureEventStore struct {
	events []cloudevents.Event
}

func (s *captureEventStore) Append(_ context.Context, event cloudevents.Event) error {
	s.events = append(s.events, event)
	return nil
}

func (s *captureEventStore) GetBySubject(_ context.Context, _ string) ([]cloudevents.Event, error) {
	return nil, nil
}

func (s *captureEventStore) GetEventsByExecution(_ context.Context, _ string, _ *time.Time) ([]cloudevents.Event, error) {
	return nil, nil
}

func (s *captureEventStore) ExistsByDedupKey(_ context.Context, _ string) (bool, error) {
	return false, nil
}

func (s *captureEventStore) SaveDedupRecord(_ context.Context, _ string, _ time.Duration) error {
	return nil
}

func TestOrchestratorRuntimeSubjects(t *testing.T) {
	t.Parallel()

	require.Equal(t, []string{
		messaging.SubjectRuntimeExecutionStartedV1,
		messaging.SubjectRuntimeNodeExecutedV1,
	}, engine.OrchestratorRuntimeSubjects())
}

func TestOrchestratorStart_UsesOwnedRuntimeSubscribers(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	mockEventStore := &captureEventStore{}
	mockPublisher := eventbusmocks.NewMockPublisher(t)
	mockWorkflowRepo := enginemocks.NewMockWorkflowRepository(t)
	startedSubscriber := eventbusmocks.NewMockSubscriber(t)
	executedSubscriber := eventbusmocks.NewMockSubscriber(t)

	mockWorkflowRepo.EXPECT().GetByID(mock.Anything, "test-workflow").Return(testWorkflow(), nil).Twice()
	mockPublisher.EXPECT().Publish(mock.Anything, mock.MatchedBy(func(e cloudevents.Event) bool {
		return e.Type() == messaging.EventTypeRuntimeNodeScheduledV1
	})).Return(nil)
	mockPublisher.EXPECT().Publish(mock.Anything, mock.MatchedBy(func(e cloudevents.Event) bool {
		return e.Type() == engine.ExecutionCompleted
	})).Return(nil)

	startEvent := cloudevents.NewEvent()
	startEvent.SetID("event-start")
	startEvent.SetSource(engine.EventSource)
	startEvent.SetType(engine.ExecutionStarted)
	startEvent.SetSubject("exec-standalone")
	startEvent.SetExtension("workflowid", "test-workflow")
	_ = startEvent.SetData(cloudevents.ApplicationJSON, engine.ExecutionStartedData{
		WorkflowID: "test-workflow",
		InputData:  map[string]any{"source": "api"},
	})

	nodeExecutedEvent := cloudevents.NewEvent()
	nodeExecutedEvent.SetID("event-executed")
	nodeExecutedEvent.SetSource(engine.EventSource)
	nodeExecutedEvent.SetType(engine.NodeExecutionExecuted)
	nodeExecutedEvent.SetSubject("exec-standalone")
	nodeExecutedEvent.SetExtension("workflowid", "test-workflow")
	_ = nodeExecutedEvent.SetData(cloudevents.ApplicationJSON, engine.NodeExecutionResultData{
		NodeID:     "action",
		OutputPort: engine.DefaultPort,
		OutputData: map[string]any{"status": "ok"},
		RunIndex:   0,
	})

	startedSubscriber.EXPECT().
		Subscribe(mock.Anything, mock.AnythingOfType("eventbus.EventHandler")).
		RunAndReturn(func(ctx context.Context, handler eventbus.EventHandler) error {
			return handler(ctx, startEvent)
		})
	executedSubscriber.EXPECT().
		Subscribe(mock.Anything, mock.AnythingOfType("eventbus.EventHandler")).
		RunAndReturn(func(ctx context.Context, handler eventbus.EventHandler) error {
			return handler(ctx, nodeExecutedEvent)
		})

	orchestrator := engine.NewOrchestrator(
		mockEventStore,
		mockPublisher,
		nil,
		mockWorkflowRepo,
		engine.WithOrchestratorSubscribers(startedSubscriber, executedSubscriber),
	)

	require.NoError(t, orchestrator.Start(ctx))
	require.Len(t, mockEventStore.events, 2)
	assert.Contains(t, []string{mockEventStore.events[0].Type(), mockEventStore.events[1].Type()}, messaging.EventTypeRuntimeNodeScheduledV1)
	assert.Contains(t, []string{mockEventStore.events[0].Type(), mockEventStore.events[1].Type()}, engine.ExecutionCompleted)
}

func TestOrchestrator_HandleEvent(t *testing.T) {
	tests := []struct {
		name          string
		eventType     string
		eventData     any
		workflowID    string
		expectedCalls func(*eventbusmocks.MockPublisher, *enginemocks.MockWorkflowRepository)
		assertEvents  func(*testing.T, *captureEventStore)
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
			expectedCalls: func(pub *eventbusmocks.MockPublisher, repo *enginemocks.MockWorkflowRepository) {
				repo.EXPECT().GetByID(mock.Anything, "test-workflow").Return(testWorkflow(), nil)
				pub.EXPECT().Publish(mock.Anything, mock.MatchedBy(func(e cloudevents.Event) bool {
					return e.Type() == messaging.EventTypeRuntimeNodeScheduledV1
				})).Return(nil)
			},
			assertEvents: func(t *testing.T, store *captureEventStore) {
				t.Helper()

				require.Len(t, store.events, 1)
				assert.Equal(t, messaging.EventTypeRuntimeNodeScheduledV1, store.events[0].Type())
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
			expectedCalls: func(pub *eventbusmocks.MockPublisher, repo *enginemocks.MockWorkflowRepository) {
				repo.EXPECT().GetByID(mock.Anything, "conditional-workflow").Return(conditionalWorkflow(), nil)
				pub.EXPECT().Publish(mock.Anything, mock.MatchedBy(func(e cloudevents.Event) bool {
					return e.Type() == messaging.EventTypeRuntimeNodeScheduledV1
				})).Return(nil)
			},
			assertEvents: func(t *testing.T, store *captureEventStore) {
				t.Helper()

				require.Len(t, store.events, 1)
				assert.Equal(t, messaging.EventTypeRuntimeNodeScheduledV1, store.events[0].Type())

				var data engine.NodeExecutionScheduledData
				require.NoError(t, store.events[0].DataAs(&data))
				assert.Equal(t, "on-true", data.NodeID)
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()

			mockEventStore := &captureEventStore{}
			mockPublisher := eventbusmocks.NewMockPublisher(t)
			mockSubscriber := eventbusmocks.NewMockSubscriber(t)
			mockWorkflowRepo := enginemocks.NewMockWorkflowRepository(t)

			tt.expectedCalls(mockPublisher, mockWorkflowRepo)

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

			if tt.assertEvents != nil {
				tt.assertEvents(t, mockEventStore)
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
