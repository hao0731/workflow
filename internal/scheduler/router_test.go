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
	"github.com/cheriehsieh/orchestration/internal/messaging"
)

// captureEventStore captures appended events for inspection.
type captureEventStore struct {
	events        []cloudevents.Event
	dedupKeys     map[string]time.Duration
	savedDedupKey []string
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

func (s *captureEventStore) ExistsByDedupKey(_ context.Context, dedupKey string) (bool, error) {
	if s.dedupKeys == nil {
		return false, nil
	}

	_, ok := s.dedupKeys[dedupKey]
	return ok, nil
}

func (s *captureEventStore) SaveDedupRecord(_ context.Context, dedupKey string, ttl time.Duration) error {
	if s.dedupKeys == nil {
		s.dedupKeys = make(map[string]time.Duration)
	}

	s.dedupKeys[dedupKey] = ttl
	s.savedDedupKey = append(s.savedDedupKey, dedupKey)
	return nil
}

// capturePublisher captures published events for inspection.
type capturePublisher struct {
	events []cloudevents.Event
}

func (p *capturePublisher) Publish(_ context.Context, event cloudevents.Event) error {
	p.events = append(p.events, event)
	return nil
}

type captureDispatcher struct {
	nodeType string
	event    cloudevents.Event
}

func (d *captureDispatcher) Dispatch(_ context.Context, nodeType string, event cloudevents.Event) error {
	d.nodeType = nodeType
	d.event = event
	return nil
}

type captureNodeValidator struct {
	fullTypes []string
}

func (v *captureNodeValidator) Exists(_ context.Context, fullType string) bool {
	v.fullTypes = append(v.fullTypes, fullType)
	return fullType == "http-request@v1"
}

type staticWorkflowRepo struct {
	workflow *engine.Workflow
}

func (r *staticWorkflowRepo) GetByID(_ context.Context, _ string) (*engine.Workflow, error) {
	return r.workflow, nil
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

func TestSchedulerSubscriptionSubjects(t *testing.T) {
	t.Parallel()

	require.Equal(t, []string{
		messaging.SubjectRuntimeNodeScheduledV1,
		messaging.SubjectEventNodeExecutedV1,
	}, SchedulerSubscriptionSubjects())
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

func TestEventRouter_SchedulerUsesFullTypeForDispatchAndValidation(t *testing.T) {
	store := &captureEventStore{}
	publisher := &capturePublisher{}
	dispatcher := &captureDispatcher{}
	validator := &captureNodeValidator{}

	workflow := &engine.Workflow{
		ID: "custom-worker-workflow",
		Nodes: []engine.Node{
			{ID: "start", Type: engine.StartNode, Name: "Start", FullType: string(engine.StartNode)},
			{ID: "custom", Type: engine.ActionNode, Name: "Custom Worker", FullType: "http-request@v1"},
		},
		Connections: []engine.Connection{
			{FromNode: "start", ToNode: "custom"},
		},
	}

	s := NewScheduler(
		store,
		publisher,
		nil,
		nil,
		dispatcher,
		&staticWorkflowRepo{workflow: workflow},
		WithSchedulerLogger(noopLogger()),
		WithNodeValidator(validator),
	)

	event := cloudevents.NewEvent()
	event.SetID("scheduled-1")
	event.SetSource("test")
	event.SetType(engine.NodeExecutionScheduled)
	event.SetSubject("exec-1")
	event.SetExtension("workflowid", workflow.ID)
	_ = event.SetData(cloudevents.ApplicationJSON, engine.NodeExecutionScheduledData{
		NodeID:    "custom",
		InputData: map[string]any{"hello": "world"},
		RunIndex:  0,
	})

	err := s.handleScheduled(context.Background(), event)
	require.NoError(t, err)

	require.Equal(t, []string{"http-request@v1"}, validator.fullTypes)
	assert.Equal(t, "http-request@v1", dispatcher.nodeType)
	assert.Equal(t, messaging.CommandNodeExecuteSubjectFromFullType("http-request@v1"), dispatcher.event.Type())

	var dispatchData NodeDispatchData
	require.NoError(t, dispatcher.event.DataAs(&dispatchData))
	assert.Equal(t, "http-request@v1", dispatchData.NodeType)
}

func TestScheduler_HandleResult_PublishesRuntimeNodeExecutedV1(t *testing.T) {
	store := &captureEventStore{}
	publisher := &capturePublisher{}

	s := NewScheduler(
		store,
		publisher,
		nil,
		nil,
		&captureDispatcher{},
		&staticWorkflowRepo{},
		WithSchedulerLogger(noopLogger()),
	)

	event := cloudevents.NewEvent()
	event.SetID("node-result-1")
	event.SetSource("worker/http-request-v1")
	event.SetType(messaging.EventTypeNodeExecutedV1)
	event.SetSubject("exec-2")
	event.SetExtension("workflowid", "workflow-2")
	event.SetExtension("executionid", "exec-2")
	event.SetExtension("nodeid", "custom")
	event.SetExtension("runindex", 1)
	event.SetExtension("attempt", 1)
	event.SetExtension("idempotencykey", "node:exec-2:custom:1:1:v1")
	event.SetExtension("producer", "worker/http-request@v1")
	_ = event.SetData(cloudevents.ApplicationJSON, NodeResultData{
		NodeID:     "custom",
		OutputPort: engine.PortSuccess,
		OutputData: map[string]any{"status": "ok"},
		RunIndex:   1,
	})

	err := s.handleResult(context.Background(), event)
	require.NoError(t, err)

	require.Len(t, store.events, 1)
	require.Len(t, publisher.events, 1)
	assert.Equal(t, messaging.EventTypeRuntimeNodeExecutedV1, store.events[0].Type())
	assert.Equal(t, messaging.EventTypeRuntimeNodeExecutedV1, publisher.events[0].Type())

	var data engine.NodeExecutionResultData
	require.NoError(t, publisher.events[0].DataAs(&data))
	assert.Equal(t, "custom", data.NodeID)
	assert.Equal(t, engine.PortSuccess, data.OutputPort)
	assert.Equal(t, map[string]any{"status": "ok"}, data.OutputData)
	assert.Equal(t, 1, data.RunIndex)
}

func TestScheduler_HandleResult_DedupIgnoresDuplicateNodeResults(t *testing.T) {
	store := &captureEventStore{}
	publisher := &capturePublisher{}

	s := NewScheduler(
		store,
		publisher,
		nil,
		nil,
		&captureDispatcher{},
		&staticWorkflowRepo{},
		WithSchedulerLogger(noopLogger()),
	)

	newResultEvent := func(id string) cloudevents.Event {
		event := cloudevents.NewEvent()
		event.SetID(id)
		event.SetSource("worker/http-request-v1")
		event.SetType(messaging.EventTypeNodeExecutedV1)
		event.SetSubject("exec-2")
		event.SetExtension("workflowid", "workflow-2")
		event.SetExtension("executionid", "exec-2")
		event.SetExtension("nodeid", "custom")
		event.SetExtension("runindex", 1)
		event.SetExtension("attempt", 1)
		event.SetExtension("idempotencykey", "node:exec-2:custom:1:1:v1")
		event.SetExtension("producer", "worker/http-request@v1")
		_ = event.SetData(cloudevents.ApplicationJSON, NodeResultData{
			ExecutionID: "exec-2",
			NodeID:      "custom",
			OutputPort:  engine.PortSuccess,
			OutputData:  map[string]any{"status": "ok"},
			RunIndex:    1,
		})
		return event
	}

	err := s.handleResult(context.Background(), newResultEvent("node-result-1"))
	require.NoError(t, err)

	err = s.handleResult(context.Background(), newResultEvent("node-result-2"))
	require.NoError(t, err)

	require.Len(t, store.events, 1)
	require.Len(t, publisher.events, 1)
	assert.Equal(t, []string{"exec-2|custom|1|1|node:exec-2:custom:1:1:v1"}, store.savedDedupKey)
}
