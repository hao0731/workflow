package registry

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var (
	ErrNodeNotFound      = errors.New("node type not found")
	ErrNodeAlreadyExists = errors.New("node type already exists")
)

// Repository handles persistence of node registrations.
type Repository interface {
	Create(ctx context.Context, reg *NodeRegistration) error
	GetByFullType(ctx context.Context, fullType string) (*NodeRegistration, error)
	GetByID(ctx context.Context, id string) (*NodeRegistration, error)
	List(ctx context.Context) ([]NodeRegistration, error)
	UpdateHeartbeat(ctx context.Context, fullType string, workerID string) error
	Delete(ctx context.Context, id string) error
}

// MongoRepository implements Repository using MongoDB.
type MongoRepository struct {
	collection *mongo.Collection
}

// Compile-time interface compliance check.
var _ Repository = (*MongoRepository)(nil)

// NewMongoRepository creates a new MongoDB-backed repository.
func NewMongoRepository(db *mongo.Database) *MongoRepository {
	coll := db.Collection("node_registrations")

	// Create unique index on full_type
	_, _ = coll.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys:    bson.D{{Key: "full_type", Value: 1}},
		Options: options.Index().SetUnique(true),
	})

	return &MongoRepository{collection: coll}
}

func (r *MongoRepository) Create(ctx context.Context, reg *NodeRegistration) error {
	_, err := r.collection.InsertOne(ctx, reg)
	if mongo.IsDuplicateKeyError(err) {
		return ErrNodeAlreadyExists
	}
	return err
}

func (r *MongoRepository) GetByFullType(ctx context.Context, fullType string) (*NodeRegistration, error) {
	var reg NodeRegistration
	err := r.collection.FindOne(ctx, bson.M{"full_type": fullType}).Decode(&reg)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNodeNotFound
	}
	return &reg, err
}

func (r *MongoRepository) GetByID(ctx context.Context, id string) (*NodeRegistration, error) {
	var reg NodeRegistration
	err := r.collection.FindOne(ctx, bson.M{"_id": id}).Decode(&reg)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrNodeNotFound
	}
	return &reg, err
}

func (r *MongoRepository) List(ctx context.Context) (_ []NodeRegistration, err error) {
	cursor, err := r.collection.Find(ctx, bson.M{"enabled": true})
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := cursor.Close(ctx); closeErr != nil && err == nil {
			err = closeErr
		}
	}()

	var registrations []NodeRegistration
	if err := cursor.All(ctx, &registrations); err != nil {
		return nil, err
	}
	return registrations, nil
}

func (r *MongoRepository) UpdateHeartbeat(ctx context.Context, fullType, workerID string) error {
	now := time.Now()
	_, err := r.collection.UpdateOne(ctx,
		bson.M{"full_type": fullType},
		bson.M{
			"$set": bson.M{
				"last_heartbeat": now,
				"updated_at":     now,
			},
			"$inc": bson.M{"worker_count": 1},
		},
	)
	return err
}

func (r *MongoRepository) Delete(ctx context.Context, id string) error {
	_, err := r.collection.DeleteOne(ctx, bson.M{"_id": id})
	return err
}
