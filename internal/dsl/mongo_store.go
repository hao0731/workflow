package dsl

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/cheriehsieh/orchestration/internal/engine"
)

// WorkflowDocument represents the MongoDB document structure.
type WorkflowDocument struct {
	ID          string              `bson:"_id"`
	Source      []byte              `bson:"source"`
	Nodes       []engine.Node       `bson:"nodes"`
	Connections []engine.Connection `bson:"connections"`
	CreatedAt   time.Time           `bson:"created_at"`
	UpdatedAt   time.Time           `bson:"updated_at"`
	DeletedAt   *time.Time          `bson:"deleted_at,omitempty"`
}

// MongoWorkflowStore provides MongoDB storage for workflows.
type MongoWorkflowStore struct {
	collection *mongo.Collection
}

// NewMongoWorkflowStore creates a new MongoDB workflow store.
func NewMongoWorkflowStore(db *mongo.Database) *MongoWorkflowStore {
	coll := db.Collection("workflows")

	// Create unique index on _id (basic indexing strategy)
	_, _ = coll.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys:    bson.D{{Key: "_id", Value: 1}},
		Options: options.Index().SetUnique(true),
	})

	return &MongoWorkflowStore{collection: coll}
}

func (s *MongoWorkflowStore) Register(ctx context.Context, wf *engine.Workflow, source []byte) error {
	if wf == nil {
		return errors.New("workflow is nil")
	}
	if wf.ID == "" {
		return errors.New("workflow ID is required")
	}

	now := time.Now()

	opts := options.Update().SetUpsert(true)
	_, err := s.collection.UpdateOne(ctx,
		bson.M{"_id": wf.ID},
		bson.M{
			"$set": bson.M{
				"source":      source,
				"nodes":       wf.Nodes,
				"connections": wf.Connections,
				"updated_at":  now,
			},
			"$setOnInsert": bson.M{"created_at": now},
			"$unset":       bson.M{"deleted_at": ""},
		},
		opts,
	)
	return err
}

func (s *MongoWorkflowStore) GetByID(ctx context.Context, id string) (*engine.Workflow, error) {
	var doc WorkflowDocument

	err := s.collection.FindOne(ctx, bson.M{"_id": id, "deleted_at": bson.M{"$exists": false}}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrWorkflowNotFound
	}
	if err != nil {
		return nil, err
	}

	return &engine.Workflow{
		ID:          doc.ID,
		Nodes:       doc.Nodes,
		Connections: doc.Connections,
	}, nil
}

func (s *MongoWorkflowStore) GetSource(ctx context.Context, id string) ([]byte, error) {
	var doc struct {
		Source []byte `bson:"source"`
	}

	opts := options.FindOne().SetProjection(bson.M{"source": 1})
	err := s.collection.FindOne(ctx, bson.M{"_id": id, "deleted_at": bson.M{"$exists": false}}, opts).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrWorkflowNotFound
	}
	if err != nil {
		return nil, err
	}

	return doc.Source, nil
}

func (s *MongoWorkflowStore) List(ctx context.Context) ([]string, error) {
	opts := options.Find().SetProjection(bson.M{"_id": 1})
	cursor, err := s.collection.Find(ctx, bson.M{"deleted_at": bson.M{"$exists": false}}, opts)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var results []struct {
		ID string `bson:"_id"`
	}
	if err := cursor.All(ctx, &results); err != nil {
		return nil, err
	}

	ids := make([]string, len(results))
	for i, r := range results {
		ids[i] = r.ID
	}
	return ids, nil
}

func (s *MongoWorkflowStore) Delete(ctx context.Context, id string) error {
	now := time.Now()
	result, err := s.collection.UpdateOne(ctx,
		bson.M{"_id": id, "deleted_at": bson.M{"$exists": false}},
		bson.M{"$set": bson.M{"deleted_at": now}},
	)
	if err != nil {
		return err
	}
	if result.MatchedCount == 0 {
		return ErrWorkflowNotFound
	}
	return nil
}
