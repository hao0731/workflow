package eventstore

import (
	"context"
	"testing"
	"time"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeEventStore is a minimal in-memory EventStore for unit testing.
type fakeEventStore struct {
	appended []cloudevents.Event
}

func (f *fakeEventStore) Append(_ context.Context, event cloudevents.Event) error {
	f.appended = append(f.appended, event)
	return nil
}

func (f *fakeEventStore) GetBySubject(_ context.Context, subject string) ([]cloudevents.Event, error) {
	var result []cloudevents.Event
	for _, e := range f.appended {
		if e.Subject() == subject {
			result = append(result, e)
		}
	}
	return result, nil
}

func (f *fakeEventStore) GetEventsByExecution(_ context.Context, execID string, since *time.Time) ([]cloudevents.Event, error) {
	var result []cloudevents.Event
	for _, e := range f.appended {
		if e.Subject() == execID {
			if since == nil || e.Time().After(*since) {
				result = append(result, e)
			}
		}
	}
	return result, nil
}

func (f *fakeEventStore) GetExecutionsByWorkflow(_ context.Context, workflowID string) ([]ExecutionSummary, error) {
	return []ExecutionSummary{{ID: "exec-from-mongo", WorkflowID: workflowID, Status: "running", StartedAt: time.Now()}}, nil
}

func TestHybridEventStore_Append_DelegatesToCassandra(t *testing.T) {
	cassandra := &fakeEventStore{}
	mongo := &fakeEventStore{}
	store := NewHybridEventStore(cassandra, mongo)

	event := cloudevents.NewEvent()
	event.SetID("e1")
	event.SetSource("test")
	event.SetType("test.event")
	event.SetSubject("exec-1")

	err := store.Append(context.Background(), event)
	require.NoError(t, err)
	assert.Len(t, cassandra.appended, 1, "should write to cassandra")
	assert.Len(t, mongo.appended, 0, "should NOT write to mongo")
}

func TestHybridEventStore_GetBySubject_ReadsFromCassandra(t *testing.T) {
	cassandra := &fakeEventStore{}
	mongo := &fakeEventStore{}
	store := NewHybridEventStore(cassandra, mongo)

	event := cloudevents.NewEvent()
	event.SetID("e1")
	event.SetSource("test")
	event.SetType("test.event")
	event.SetSubject("exec-1")
	cassandra.appended = append(cassandra.appended, event)

	events, err := store.GetBySubject(context.Background(), "exec-1")
	require.NoError(t, err)
	assert.Len(t, events, 1)
}

func TestHybridEventStore_GetEventsByExecution_ReadsFromCassandra(t *testing.T) {
	cassandra := &fakeEventStore{}
	mongo := &fakeEventStore{}
	store := NewHybridEventStore(cassandra, mongo)

	event := cloudevents.NewEvent()
	event.SetID("e1")
	event.SetSource("test")
	event.SetType("test.event")
	event.SetSubject("exec-1")
	event.SetTime(time.Now())
	cassandra.appended = append(cassandra.appended, event)

	events, err := store.GetEventsByExecution(context.Background(), "exec-1", nil)
	require.NoError(t, err)
	assert.Len(t, events, 1)
}

func TestHybridEventStore_GetExecutionsByWorkflow_ReadsFromMongo(t *testing.T) {
	cassandra := &fakeEventStore{}
	mongo := &fakeEventStore{}
	store := NewHybridEventStore(cassandra, mongo)

	summaries, err := store.GetExecutionsByWorkflow(context.Background(), "wf-1")
	require.NoError(t, err)
	assert.Len(t, summaries, 1)
	assert.Equal(t, "exec-from-mongo", summaries[0].ID)
}
