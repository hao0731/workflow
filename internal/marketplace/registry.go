package marketplace

import (
	"context"
	"errors"
	"sync"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var (
	ErrEventNotFound      = errors.New("event definition not found")
	ErrEventAlreadyExists = errors.New("event definition already exists")
)

// EventDefinition describes a public event in the marketplace.
type EventDefinition struct {
	ID          string    `json:"id" bson:"_id,omitempty"`
	Name        string    `json:"name" bson:"name"`
	Domain      string    `json:"domain" bson:"domain"`
	FullName    string    `json:"full_name" bson:"full_name"` // domain.name
	Description string    `json:"description" bson:"description"`
	Schema      any       `json:"schema" bson:"schema"` // JSON Schema
	Owner       string    `json:"owner" bson:"owner"`
	CreatedAt   time.Time `json:"created_at" bson:"created_at"`
	UpdatedAt   time.Time `json:"updated_at" bson:"updated_at"`
}

// BuildSubject returns the NATS subject for this event.
func (e *EventDefinition) BuildSubject() string {
	return "marketplace." + e.Domain + "." + e.Name
}

// EventRegistry stores and retrieves event definitions.
type EventRegistry interface {
	Register(ctx context.Context, def *EventDefinition) error
	Get(ctx context.Context, domain, name string) (*EventDefinition, error)
	List(ctx context.Context, domain string) ([]*EventDefinition, error)
	Delete(ctx context.Context, domain, name string) error
}

// InMemoryEventRegistry is a simple in-memory registry for testing.
type InMemoryEventRegistry struct {
	mu     sync.RWMutex
	events map[string]*EventDefinition // key: domain.name
}

func NewInMemoryEventRegistry() *InMemoryEventRegistry {
	return &InMemoryEventRegistry{
		events: make(map[string]*EventDefinition),
	}
}

func (r *InMemoryEventRegistry) key(domain, name string) string {
	return domain + "." + name
}

func (r *InMemoryEventRegistry) Register(ctx context.Context, def *EventDefinition) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := r.key(def.Domain, def.Name)
	if _, exists := r.events[key]; exists {
		return ErrEventAlreadyExists
	}

	def.FullName = key
	def.CreatedAt = time.Now()
	def.UpdatedAt = time.Now()
	r.events[key] = def
	return nil
}

func (r *InMemoryEventRegistry) Get(ctx context.Context, domain, name string) (*EventDefinition, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	def, ok := r.events[r.key(domain, name)]
	if !ok {
		return nil, ErrEventNotFound
	}
	return def, nil
}

func (r *InMemoryEventRegistry) List(ctx context.Context, domain string) ([]*EventDefinition, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []*EventDefinition
	for _, def := range r.events {
		if domain == "" || def.Domain == domain {
			result = append(result, def)
		}
	}
	return result, nil
}

func (r *InMemoryEventRegistry) Delete(ctx context.Context, domain, name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.events, r.key(domain, name))
	return nil
}

// MongoEventRegistry persists event definitions to MongoDB.
type MongoEventRegistry struct {
	collection *mongo.Collection
}

func NewMongoEventRegistry(db *mongo.Database) *MongoEventRegistry {
	coll := db.Collection("event_definitions")
	// Create unique index on full_name
	_, _ = coll.Indexes().CreateOne(context.Background(), mongo.IndexModel{
		Keys:    bson.D{{Key: "full_name", Value: 1}},
		Options: options.Index().SetUnique(true),
	})
	return &MongoEventRegistry{collection: coll}
}

func (r *MongoEventRegistry) Register(ctx context.Context, def *EventDefinition) error {
	def.FullName = def.Domain + "." + def.Name
	def.CreatedAt = time.Now()
	def.UpdatedAt = time.Now()

	_, err := r.collection.InsertOne(ctx, def)
	if mongo.IsDuplicateKeyError(err) {
		return ErrEventAlreadyExists
	}
	return err
}

func (r *MongoEventRegistry) Get(ctx context.Context, domain, name string) (*EventDefinition, error) {
	fullName := domain + "." + name
	var def EventDefinition
	err := r.collection.FindOne(ctx, bson.M{"full_name": fullName}).Decode(&def)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrEventNotFound
	}
	return &def, err
}

func (r *MongoEventRegistry) List(ctx context.Context, domain string) (_ []*EventDefinition, err error) {
	filter := bson.M{}
	if domain != "" {
		filter["domain"] = domain
	}

	cursor, err := r.collection.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := cursor.Close(ctx); closeErr != nil && err == nil {
			err = closeErr
		}
	}()

	var results []*EventDefinition
	if err := cursor.All(ctx, &results); err != nil {
		return nil, err
	}
	return results, nil
}

func (r *MongoEventRegistry) Delete(ctx context.Context, domain, name string) error {
	fullName := domain + "." + name
	_, err := r.collection.DeleteOne(ctx, bson.M{"full_name": fullName})
	return err
}

// Compile-time interface checks.
var (
	_ EventRegistry = (*InMemoryEventRegistry)(nil)
	_ EventRegistry = (*MongoEventRegistry)(nil)
)
