package eventstore

import (
	"context"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"go.mongodb.org/mongo-driver/mongo"
)

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
	for i, s := range stored {
		events[i] = s.ToCloudEvent()
	}
	return events, nil
}
