package eventstore

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/gocql/gocql"
)

// Compile-time interface compliance check.
var _ EventStore = (*CassandraEventStore)(nil)

// CassandraEventStore implements event persistence using Cassandra.
type CassandraEventStore struct {
	session *gocql.Session
}

// NewCassandraEventStore creates a new Cassandra-backed event store.
func NewCassandraEventStore(session *gocql.Session) *CassandraEventStore {
	return &CassandraEventStore{session: session}
}

func (s *CassandraEventStore) Append(ctx context.Context, event cloudevents.Event) error {
	stored := FromCloudEvent(event)

	dataJSON, err := json.Marshal(stored.Data)
	if err != nil {
		return fmt.Errorf("marshal event data: %w", err)
	}

	extensions := make(map[string]string)
	for k, v := range stored.Extensions {
		extensions[k] = fmt.Sprintf("%v", v)
	}

	return s.session.Query(
		`INSERT INTO orchestration.events
		 (subject, time, id, type, source, datacontenttype, data, extensions)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		stored.Subject,
		stored.Time,
		stored.ID,
		stored.Type,
		stored.Source,
		stored.DataContentType,
		string(dataJSON),
		extensions,
	).WithContext(ctx).Exec()
}

func (s *CassandraEventStore) GetBySubject(ctx context.Context, subject string) ([]cloudevents.Event, error) {
	return s.queryEvents(ctx, subject, nil)
}

func (s *CassandraEventStore) GetEventsByExecution(ctx context.Context, executionID string, since *time.Time) ([]cloudevents.Event, error) {
	return s.queryEvents(ctx, executionID, since)
}

func (s *CassandraEventStore) GetExecutionsByWorkflow(_ context.Context, _ string) ([]ExecutionSummary, error) {
	return nil, fmt.Errorf("GetExecutionsByWorkflow not supported by CassandraEventStore: use HybridEventStore")
}

// queryEvents retrieves events by partition key (subject), optionally filtered by time.
func (s *CassandraEventStore) queryEvents(ctx context.Context, subject string, since *time.Time) ([]cloudevents.Event, error) {
	var query *gocql.Query
	if since != nil {
		query = s.session.Query(
			"SELECT subject, time, id, type, source, datacontenttype, data, extensions FROM orchestration.events WHERE subject = ? AND time > ?",
			subject, *since,
		)
	} else {
		query = s.session.Query(
			"SELECT subject, time, id, type, source, datacontenttype, data, extensions FROM orchestration.events WHERE subject = ?",
			subject,
		)
	}

	iter := query.WithContext(ctx).Iter()
	var events []cloudevents.Event

	var (
		sub, id, typ, source, dataContentType, dataJSON string
		ts                                              time.Time
		extensions                                      map[string]string
	)
	for iter.Scan(&sub, &ts, &id, &typ, &source, &dataContentType, &dataJSON, &extensions) {
		event := cloudevents.NewEvent()
		event.SetID(id)
		event.SetSource(source)
		event.SetType(typ)
		event.SetSubject(sub)
		event.SetTime(ts)

		var data map[string]any
		if err := json.Unmarshal([]byte(dataJSON), &data); err == nil {
			_ = event.SetData(cloudevents.ApplicationJSON, data)
		}

		for k, v := range extensions {
			event.SetExtension(k, v)
		}

		events = append(events, event)
	}

	if err := iter.Close(); err != nil {
		return nil, fmt.Errorf("cassandra query: %w", err)
	}

	return events, nil
}
