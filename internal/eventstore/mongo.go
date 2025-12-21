package eventstore

import (
	"context"

	"go.mongodb.org/mongo-driver/mongo"

	cloudevents "github.com/cloudevents/sdk-go/v2"
)

// Compile-time interface compliance check.
var _ EventStore = (*MongoEventStore)(nil)

// MongoEventStore implements EventStore using MongoDB.
type MongoEventStore struct {
	collection *mongo.Collection
}

// NewMongoEventStore creates a new MongoDB-backed event store.
func NewMongoEventStore(db *mongo.Database, collectionName string) *MongoEventStore {
	return &MongoEventStore{
		collection: db.Collection(collectionName),
	}
}

func (s *MongoEventStore) Append(ctx context.Context, event cloudevents.Event) error {
	stored := FromCloudEvent(event)
	_, err := s.collection.InsertOne(ctx, stored)
	return err
}

func (s *MongoEventStore) GetBySubject(ctx context.Context, subject string) ([]cloudevents.Event, error) {
	cursor, err := s.collection.Find(ctx, map[string]string{"subject": subject})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var stored []StoredEvent
	if err := cursor.All(ctx, &stored); err != nil {
		return nil, err
	}

	events := make([]cloudevents.Event, len(stored))
	for i, se := range stored {
		events[i] = se.ToCloudEvent()
	}
	return events, nil
}

// GetExecutionsByWorkflow returns execution summaries for a given workflow ID.
// It finds all execution.started events and derives status from execution.completed/failed events.
func (s *MongoEventStore) GetExecutionsByWorkflow(ctx context.Context, workflowID string) ([]ExecutionSummary, error) {
	// Find all execution.started events for this workflow
	filter := map[string]any{
		"type":             "orchestration.execution.started",
		"data.workflow_id": workflowID,
	}

	cursor, err := s.collection.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var startEvents []StoredEvent
	if err := cursor.All(ctx, &startEvents); err != nil {
		return nil, err
	}

	summaries := make([]ExecutionSummary, 0, len(startEvents))
	for _, se := range startEvents {
		execID := se.Subject
		status := "running"

		// Check if execution completed or failed
		statusEvents, err := s.collection.Find(ctx, map[string]any{
			"subject": execID,
			"type": map[string]any{
				"$in": []string{
					"orchestration.execution.completed",
					"orchestration.execution.failed",
				},
			},
		})
		if err == nil {
			var statusEvts []StoredEvent
			if statusEvents.All(ctx, &statusEvts) == nil && len(statusEvts) > 0 {
				for _, evt := range statusEvts {
					if evt.Type == "orchestration.execution.completed" {
						status = "completed"
					} else if evt.Type == "orchestration.execution.failed" {
						status = "failed"
					}
				}
			}
			statusEvents.Close(ctx)
		}

		summaries = append(summaries, ExecutionSummary{
			ID:         execID,
			WorkflowID: workflowID,
			Status:     status,
			StartedAt:  se.Time,
		})
	}

	return summaries, nil
}
