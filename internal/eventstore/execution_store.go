package eventstore

import (
	"context"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// ExecutionStore manages execution state with linking.
type ExecutionStore interface {
	Create(ctx context.Context, exec *Execution) error
	GetByID(ctx context.Context, id string) (*Execution, error)
	GetChildren(ctx context.Context, parentID string) ([]*Execution, error)
	AddChildExecution(ctx context.Context, parentID, childID string) error
	UpdateStatus(ctx context.Context, id, status string) error
}

// MongoExecutionStore implements ExecutionStore using MongoDB.
type MongoExecutionStore struct {
	collection *mongo.Collection
}

// NewMongoExecutionStore creates a new MongoDB-backed execution store.
func NewMongoExecutionStore(db *mongo.Database, collectionName string) *MongoExecutionStore {
	return &MongoExecutionStore{
		collection: db.Collection(collectionName),
	}
}

func (s *MongoExecutionStore) Create(ctx context.Context, exec *Execution) error {
	_, err := s.collection.InsertOne(ctx, exec)
	return err
}

func (s *MongoExecutionStore) GetByID(ctx context.Context, id string) (*Execution, error) {
	var exec Execution
	err := s.collection.FindOne(ctx, bson.M{"_id": id}).Decode(&exec)
	if err != nil {
		return nil, err
	}
	return &exec, nil
}

func (s *MongoExecutionStore) GetChildren(ctx context.Context, parentID string) ([]*Execution, error) {
	cursor, err := s.collection.Find(ctx, bson.M{"parent_execution_id": parentID})
	if err != nil {
		return nil, err
	}
	defer func() { _ = cursor.Close(ctx) }()

	var children []*Execution
	if err := cursor.All(ctx, &children); err != nil {
		return nil, err
	}
	return children, nil
}

func (s *MongoExecutionStore) AddChildExecution(ctx context.Context, parentID, childID string) error {
	_, err := s.collection.UpdateOne(ctx,
		bson.M{"_id": parentID},
		bson.M{"$addToSet": bson.M{"child_execution_ids": childID}},
	)
	return err
}

func (s *MongoExecutionStore) UpdateStatus(ctx context.Context, id, status string) error {
	_, err := s.collection.UpdateOne(ctx,
		bson.M{"_id": id},
		bson.M{"$set": bson.M{"status": status}},
	)
	return err
}

// EnsureIndexes creates required indexes for efficient queries.
func (s *MongoExecutionStore) EnsureIndexes(ctx context.Context) error {
	indexes := []mongo.IndexModel{
		{Keys: bson.D{{Key: "parent_execution_id", Value: 1}}},
		{Keys: bson.D{{Key: "workflow_id", Value: 1}, {Key: "started_at", Value: -1}}},
	}
	_, err := s.collection.Indexes().CreateMany(ctx, indexes)
	return err
}
