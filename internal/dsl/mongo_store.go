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
	Name        string              `bson:"name,omitempty"`
	Description string              `bson:"description,omitempty"`
	Version     string              `bson:"version,omitempty"`
	Source      []byte              `bson:"source"`
	Events      []EventDefinition   `bson:"events,omitempty"`
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

	_, _ = coll.Indexes().CreateMany(context.Background(), []mongo.IndexModel{
		{
			Keys:    bson.D{{Key: "_id", Value: 1}},
			Options: options.Index().SetUnique(true),
		},
		{
			Keys: bson.D{{Key: "updated_at", Value: -1}},
		},
		{
			Keys: bson.D{{Key: "events.name", Value: 1}, {Key: "events.domain", Value: 1}},
		},
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

	return s.RegisterRecord(ctx, &WorkflowRecord{
		ID:       wf.ID,
		Source:   source,
		Workflow: wf,
	})
}

func (s *MongoWorkflowStore) RegisterRecord(ctx context.Context, record *WorkflowRecord) error {
	if record == nil {
		return errors.New("workflow record is nil")
	}
	if record.Workflow == nil {
		return errors.New("workflow is nil")
	}
	if record.ID == "" {
		record.ID = record.Workflow.ID
	}
	if record.ID == "" {
		return errors.New("workflow ID is required")
	}

	now := time.Now()

	opts := options.Update().SetUpsert(true)
	_, err := s.collection.UpdateOne(ctx,
		bson.M{"_id": record.ID},
		bson.M{
			"$set": bson.M{
				"name":        record.Name,
				"description": record.Description,
				"version":     record.Version,
				"source":      record.Source,
				"events":      record.Events,
				"nodes":       record.Workflow.Nodes,
				"connections": record.Workflow.Connections,
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
	record, err := s.GetRecord(ctx, id)
	if err != nil {
		return nil, err
	}

	return record.Workflow, nil
}

func (s *MongoWorkflowStore) GetRecord(ctx context.Context, id string) (*WorkflowRecord, error) {
	var doc WorkflowDocument

	err := s.collection.FindOne(ctx, bson.M{"_id": id, "deleted_at": bson.M{"$exists": false}}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrWorkflowNotFound
	}
	if err != nil {
		return nil, err
	}

	workflow := &engine.Workflow{
		ID:          doc.ID,
		Nodes:       doc.Nodes,
		Connections: doc.Connections,
	}
	for i := range workflow.Nodes {
		if workflow.Nodes[i].FullType == "" {
			workflow.Nodes[i].FullType = workflow.Nodes[i].DispatchType()
		}
	}

	return &WorkflowRecord{
		ID:          doc.ID,
		Name:        doc.Name,
		Description: doc.Description,
		Version:     doc.Version,
		Source:      doc.Source,
		Events:      doc.Events,
		CreatedAt:   doc.CreatedAt,
		UpdatedAt:   doc.UpdatedAt,
		Workflow:    workflow,
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

func (s *MongoWorkflowStore) List(ctx context.Context) (_ []string, err error) {
	opts := options.Find().SetProjection(bson.M{"_id": 1})
	cursor, err := s.collection.Find(ctx, bson.M{"deleted_at": bson.M{"$exists": false}}, opts)
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := cursor.Close(ctx); closeErr != nil && err == nil {
			err = closeErr
		}
	}()

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

// FindByEventTrigger finds workflows that are triggered by the given event.
func (s *MongoWorkflowStore) FindByEventTrigger(ctx context.Context, eventName, domain string) (_ []*engine.Workflow, err error) {
	// Query all non-deleted workflows and filter in-memory
	// For production, consider adding indexes on nodes.trigger.criteria
	cursor, err := s.collection.Find(ctx, bson.M{"deleted_at": bson.M{"$exists": false}})
	if err != nil {
		return nil, err
	}
	defer func() {
		if closeErr := cursor.Close(ctx); closeErr != nil && err == nil {
			err = closeErr
		}
	}()

	var matching []*engine.Workflow
	for cursor.Next(ctx) {
		var doc WorkflowDocument
		if err := cursor.Decode(&doc); err != nil {
			continue
		}

		// Check if any node is a StartNode with matching event trigger
		for _, node := range doc.Nodes {
			if node.Type != engine.StartNode || node.Trigger == nil {
				continue
			}
			if node.Trigger.Type != engine.TriggerEvent {
				continue
			}

			// Check criteria match
			criteria := node.Trigger.Criteria
			if criteria == nil {
				continue
			}

			evtName, _ := criteria["event_name"].(string)
			evtDomain, _ := criteria["domain"].(string)

			if evtName == eventName && (evtDomain == "" || evtDomain == domain) {
				matching = append(matching, &engine.Workflow{
					ID:          doc.ID,
					Nodes:       doc.Nodes,
					Connections: doc.Connections,
				})
				break
			}
		}
	}

	return matching, nil
}

var _ WorkflowStore = (*MongoWorkflowStore)(nil)
var _ WorkflowRecordStore = (*MongoWorkflowStore)(nil)
