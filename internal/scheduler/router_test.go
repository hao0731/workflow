package scheduler

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"
	"time"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cheriehsieh/orchestration/internal/engine"
	"github.com/cheriehsieh/orchestration/internal/eventstore"
)

// captureEventStore captures appended events for inspection.
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

func (s *captureEventStore) GetExecutionsByWorkflow(_ context.Context, _ string) ([]eventstore.ExecutionSummary, error) {
	return nil, nil
}

func (s *captureEventStore) GetEventsByExecution(_ context.Context, _ string, _ *time.Time) ([]cloudevents.Event, error) {
	return nil, nil
}

// capturePublisher captures published events for inspection.
type capturePublisher struct {
	events []cloudevents.Event
}

func (p *capturePublisher) Publish(_ context.Context, event cloudevents.Event) error {
	p.events = append(p.events, event)
	return nil
}

// staticWorkflowMatcher returns preconfigured workflows for testing.
type staticWorkflowMatcher struct {
	workflows []*engine.Workflow
}

func (m *staticWorkflowMatcher) FindByEventTrigger(_ context.Context, _, _ string) ([]*engine.Workflow, error) {
	return m.workflows, nil
}

func noopLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestEventRouter_HandleMessage_SetsWorkflowIDExtension(t *testing.T) {
	store := &captureEventStore{}
	publisher := &capturePublisher{}

	chatWorkflow := &engine.Workflow{
		ID: "chat-new-member",
		Nodes: []engine.Node{
			{
				ID:   "start",
				Type: engine.StartNode,
				Trigger: &engine.Trigger{
					Type: engine.TriggerEvent,
					Criteria: map[string]any{
						"event_name": "new_employee",
						"domain":     "hr",
					},
				},
			},
		},
	}

	matcher := &staticWorkflowMatcher{workflows: []*engine.Workflow{chatWorkflow}}

	router := &EventRouter{
		workflowRepo:  matcher,
		eventStore:    store,
		publisher:     publisher,
		subjectPrefix: "marketplace",
		logger:        noopLogger(),
	}

	// Build a CloudEvent as NATS message data (simulating what PublishEvent sends)
	ce := cloudevents.NewEvent()
	ce.SetID("test-event-id")
	ce.SetSource("orchestration/engine")
	ce.SetType("marketplace.hr.new_employee")
	ce.SetExtension("executionid", "parent-exec-123")
	_ = ce.SetData(cloudevents.ApplicationJSON, map[string]any{
		"name":        "Alice Smith",
		"employee_id": "EMP-001",
	})

	msgData, err := json.Marshal(ce)
	require.NoError(t, err)

	msg := &nats.Msg{
		Subject: "marketplace.hr.new_employee",
		Data:    msgData,
	}

	err = router.handleMessage(context.Background(), msg)
	require.NoError(t, err)

	// Verify the ExecutionStarted event was stored
	require.Len(t, store.events, 1, "expected one event stored")
	storedEvent := store.events[0]

	assert.Equal(t, engine.ExecutionStarted, storedEvent.Type())

	// ROOT CAUSE FIX: workflowid extension must be set for query compatibility
	workflowID, ok := storedEvent.Extensions()["workflowid"].(string)
	assert.True(t, ok, "workflowid extension must be set")
	assert.Equal(t, "chat-new-member", workflowID)

	// Verify the event was also published
	require.Len(t, publisher.events, 1, "expected one event published")
	publishedEvent := publisher.events[0]
	pubWorkflowID, ok := publishedEvent.Extensions()["workflowid"].(string)
	assert.True(t, ok, "workflowid extension must be set on published event")
	assert.Equal(t, "chat-new-member", pubWorkflowID)

	// Verify workflow ID is in the data payload too
	var data engine.ExecutionStartedData
	require.NoError(t, publishedEvent.DataAs(&data))
	assert.Equal(t, "chat-new-member", data.WorkflowID)

	// Verify triggeredbyevent extension is set
	triggeredBy, ok := storedEvent.Extensions()["triggeredbyevent"].(string)
	assert.True(t, ok, "triggeredbyevent extension must be set")
	assert.Equal(t, "hr.new_employee", triggeredBy)
}
