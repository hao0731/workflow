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

func (m *mockExecutionStore) Create(ctx context.Context, exec *eventstore.Execution) error {
	m.executions[exec.ID] = exec
	return nil
}

func (m *mockExecutionStore) GetByID(ctx context.Context, id string) (*eventstore.Execution, error) {
	return m.executions[id], nil
}

func (m *mockExecutionStore) GetChildren(ctx context.Context, parentID string) ([]*eventstore.Execution, error) {
	return nil, nil
}

func (m *mockExecutionStore) AddChildExecution(ctx context.Context, parentID, childID string) error {
	m.children[parentID] = append(m.children[parentID], childID)
	return nil
}

func (m *mockExecutionStore) UpdateStatus(ctx context.Context, id, status string) error {
	if exec, ok := m.executions[id]; ok {
		exec.Status = status
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
