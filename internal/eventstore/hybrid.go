package eventstore

import (
	"context"
	"time"

	cloudevents "github.com/cloudevents/sdk-go/v2"
)

// Compile-time interface compliance check.
var _ EventStore = (*HybridEventStore)(nil)

// HybridEventStore routes writes to Cassandra and complex reads to MongoDB.
type HybridEventStore struct {
	cassandra EventStore // append + partition-key reads
	mongo     EventStore // complex aggregated reads
}

// NewHybridEventStore creates a composite event store.
func NewHybridEventStore(cassandra, mongo EventStore) *HybridEventStore {
	return &HybridEventStore{cassandra: cassandra, mongo: mongo}
}

func (h *HybridEventStore) Append(ctx context.Context, event cloudevents.Event) error {
	return h.cassandra.Append(ctx, event)
}

func (h *HybridEventStore) GetBySubject(ctx context.Context, subject string) ([]cloudevents.Event, error) {
	return h.cassandra.GetBySubject(ctx, subject)
}

func (h *HybridEventStore) GetEventsByExecution(ctx context.Context, executionID string, since *time.Time) ([]cloudevents.Event, error) {
	return h.cassandra.GetEventsByExecution(ctx, executionID, since)
}

func (h *HybridEventStore) GetExecutionsByWorkflow(ctx context.Context, workflowID string) ([]ExecutionSummary, error) {
	return h.mongo.GetExecutionsByWorkflow(ctx, workflowID)
}
