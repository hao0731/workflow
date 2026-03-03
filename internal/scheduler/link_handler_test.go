package scheduler

import (
	"context"
	"testing"
	"time"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cheriehsieh/orchestration/internal/engine"
	"github.com/cheriehsieh/orchestration/internal/eventstore"
)

type mockExecutionStore struct {
	executions map[string]*eventstore.Execution
	children   map[string][]string
}

func newMockExecutionStore() *mockExecutionStore {
	return &mockExecutionStore{
		executions: make(map[string]*eventstore.Execution),
		children:   make(map[string][]string),
	}
}

func (m *mockExecutionStore) Create(_ context.Context, exec *eventstore.Execution) error {
	m.executions[exec.ID] = exec
	return nil
}

func (m *mockExecutionStore) GetByID(_ context.Context, id string) (*eventstore.Execution, error) {
	return m.executions[id], nil
}

func (m *mockExecutionStore) GetByWorkflowID(_ context.Context, workflowID string) ([]*eventstore.Execution, error) {
	var result []*eventstore.Execution
	for _, exec := range m.executions {
		if exec.WorkflowID == workflowID {
			result = append(result, exec)
		}
	}
	return result, nil
}

func (m *mockExecutionStore) GetChildren(_ context.Context, _ string) ([]*eventstore.Execution, error) {
	return nil, nil
}

func (m *mockExecutionStore) AddChildExecution(_ context.Context, parentID, childID string) error {
	m.children[parentID] = append(m.children[parentID], childID)
	return nil
}

func (m *mockExecutionStore) UpdateStatus(_ context.Context, id, status string) error {
	if exec, ok := m.executions[id]; ok {
		exec.Status = status
	}
	return nil
}

func (m *mockExecutionStore) UpdateStatusWithTime(_ context.Context, id, status string, completedAt time.Time) error {
	if exec, ok := m.executions[id]; ok {
		exec.Status = status
		exec.CompletedAt = &completedAt
	}
	return nil
}

func TestExecutionLinkHandler_HandleWithParent(t *testing.T) {
	store := newMockExecutionStore()
	handler := NewExecutionLinkHandler(store)

	// Create parent execution first
	store.executions["parent-123"] = &eventstore.Execution{
		ID:         "parent-123",
		WorkflowID: "hr-onboarding",
		Status:     eventstore.StatusRunning,
		StartedAt:  time.Now(),
	}

	// Create ExecutionStarted event with parent reference
	event := cloudevents.NewEvent()
	event.SetID("evt-1")
	event.SetType(engine.ExecutionStarted)
	event.SetSubject("child-456")
	event.SetSource(engine.EventSource)
	event.SetExtension("workflowid", "chat-new-member")
	event.SetExtension("parentexecutionid", "parent-123")
	event.SetExtension("triggeredbyevent", "hr.new_employee")
	_ = event.SetData(cloudevents.ApplicationJSON, engine.ExecutionStartedData{WorkflowID: "chat-new-member"})

	ctx := context.Background()
	err := handler.Handle(ctx, event)
	require.NoError(t, err)

	// Verify child was created with parent reference
	child := store.executions["child-456"]
	require.NotNil(t, child)
	assert.Equal(t, "parent-123", child.ParentExecutionID)
	assert.Equal(t, "hr.new_employee", child.TriggeredByEvent)

	// Verify parent was updated with child reference
	assert.Contains(t, store.children["parent-123"], "child-456")
}

func TestExecutionLinkHandler_HandleNoParent(t *testing.T) {
	store := newMockExecutionStore()
	handler := NewExecutionLinkHandler(store)

	// Create ExecutionStarted event without parent (root execution)
	event := cloudevents.NewEvent()
	event.SetID("evt-2")
	event.SetType(engine.ExecutionStarted)
	event.SetSubject("root-789")
	event.SetSource(engine.EventSource)
	event.SetExtension("workflowid", "hr-onboarding")
	_ = event.SetData(cloudevents.ApplicationJSON, engine.ExecutionStartedData{WorkflowID: "hr-onboarding"})

	ctx := context.Background()
	err := handler.Handle(ctx, event)
	require.NoError(t, err)

	// Verify execution was created
	exec := store.executions["root-789"]
	require.NotNil(t, exec)
	assert.Empty(t, exec.ParentExecutionID)

	// Verify no parent update happened
	assert.Empty(t, store.children)
}

func TestExecutionLinkHandler_HandleTerminalEvents(t *testing.T) {
	tests := []struct {
		name           string
		executionID    string
		workflowID     string
		eventType      string
		expectedStatus string
	}{
		{name: "completed", executionID: "exec-100", workflowID: "test-wf", eventType: engine.ExecutionCompleted, expectedStatus: eventstore.StatusCompleted},
		{name: "failed", executionID: "exec-200", workflowID: "failing-wf", eventType: engine.ExecutionFailed, expectedStatus: eventstore.StatusFailed},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			store := newMockExecutionStore()
			handler := NewExecutionLinkHandler(store)

			startTime := time.Now().Add(-5 * time.Minute)
			eventTime := time.Now()

			store.executions[tc.executionID] = &eventstore.Execution{
				ID:         tc.executionID,
				WorkflowID: tc.workflowID,
				Status:     eventstore.StatusRunning,
				StartedAt:  startTime,
			}

			event := cloudevents.NewEvent()
			event.SetID("evt-terminal")
			event.SetType(tc.eventType)
			event.SetSubject(tc.executionID)
			event.SetSource(engine.EventSource)
			event.SetTime(eventTime)

			err := handler.Handle(context.Background(), event)
			require.NoError(t, err)

			exec := store.executions[tc.executionID]
			require.NotNil(t, exec)
			assert.Equal(t, tc.expectedStatus, exec.Status)
			require.NotNil(t, exec.CompletedAt)
			assert.Equal(t, eventTime.Unix(), exec.CompletedAt.Unix())
		})
	}
}

func TestExecutionLinkHandler_IgnoresUnrelatedEvents(t *testing.T) {
	store := newMockExecutionStore()
	handler := NewExecutionLinkHandler(store)

	// Create a node-level event (should be ignored)
	event := cloudevents.NewEvent()
	event.SetID("evt-5")
	event.SetType(engine.NodeExecutionCompleted)
	event.SetSubject("exec-300")
	event.SetSource(engine.EventSource)

	err := handler.Handle(context.Background(), event)
	require.NoError(t, err)

	// Verify nothing was created
	assert.Empty(t, store.executions)
}
