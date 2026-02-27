package eventstore

import (
	"context"
	"fmt"
	"testing"
	"time"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/gocql/gocql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestCassandra(t *testing.T) (*gocql.Session, func()) {
	t.Helper()
	cluster := gocql.NewCluster("localhost")
	cluster.Port = 9042
	cluster.Keyspace = "orchestration"
	cluster.Consistency = gocql.LocalQuorum

	session, err := cluster.CreateSession()
	if err != nil {
		t.Skipf("Cassandra not available: %v", err)
	}

	// Clean up before test
	_ = session.Query("TRUNCATE events").Exec()

	return session, func() {
		session.Close()
	}
}

func TestCassandraEventStore_Append(t *testing.T) {
	session, cleanup := setupTestCassandra(t)
	defer cleanup()

	store := NewCassandraEventStore(session)
	ctx := context.Background()

	event := cloudevents.NewEvent()
	event.SetID("evt-001")
	event.SetSource("test")
	event.SetType("orchestration.execution.started")
	event.SetSubject("exec-123")
	event.SetTime(time.Now().Truncate(time.Millisecond))
	_ = event.SetData(cloudevents.ApplicationJSON, map[string]any{"workflow_id": "wf-1"})
	event.SetExtension("workflowid", "wf-1")

	err := store.Append(ctx, event)
	require.NoError(t, err)

	// Verify data was written by querying directly
	var id, typ, source, subject string
	err = session.Query(
		"SELECT id, type, source, subject FROM events WHERE subject = ? LIMIT 1",
		"exec-123",
	).Scan(&id, &typ, &source, &subject)
	require.NoError(t, err)
	assert.Equal(t, "evt-001", id)
	assert.Equal(t, "orchestration.execution.started", typ)
	assert.Equal(t, "test", source)
}

func TestCassandraEventStore_GetBySubject(t *testing.T) {
	session, cleanup := setupTestCassandra(t)
	defer cleanup()

	store := NewCassandraEventStore(session)
	ctx := context.Background()

	// Insert two events for same subject
	for i, typ := range []string{"execution.started", "execution.completed"} {
		e := cloudevents.NewEvent()
		e.SetID(fmt.Sprintf("evt-%d", i))
		e.SetSource("test")
		e.SetType(typ)
		e.SetSubject("exec-get-subject")
		e.SetTime(time.Now().Add(time.Duration(i) * time.Second).Truncate(time.Millisecond))
		_ = e.SetData(cloudevents.ApplicationJSON, map[string]any{"i": i})
		require.NoError(t, store.Append(ctx, e))
	}

	events, err := store.GetBySubject(ctx, "exec-get-subject")
	require.NoError(t, err)
	assert.Len(t, events, 2)
	assert.Equal(t, "execution.started", events[0].Type())
	assert.Equal(t, "execution.completed", events[1].Type())
}

func TestCassandraEventStore_GetEventsByExecution(t *testing.T) {
	session, cleanup := setupTestCassandra(t)
	defer cleanup()

	store := NewCassandraEventStore(session)
	ctx := context.Background()

	base := time.Now().Truncate(time.Millisecond)
	for i := 0; i < 3; i++ {
		e := cloudevents.NewEvent()
		e.SetID(fmt.Sprintf("evt-exec-%d", i))
		e.SetSource("test")
		e.SetType("node.completed")
		e.SetSubject("exec-since-test")
		e.SetTime(base.Add(time.Duration(i) * time.Minute))
		_ = e.SetData(cloudevents.ApplicationJSON, map[string]any{"step": i})
		require.NoError(t, store.Append(ctx, e))
	}

	// Get events since 30 seconds after base — should return last 2
	since := base.Add(30 * time.Second)
	events, err := store.GetEventsByExecution(ctx, "exec-since-test", &since)
	require.NoError(t, err)
	assert.Len(t, events, 2)
}
