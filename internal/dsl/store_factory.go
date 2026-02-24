package dsl

import (
	"errors"
	"fmt"

	"go.mongodb.org/mongo-driver/mongo"
)

// NewWorkflowStoreFromConfig creates a WorkflowStore based on the store type.
func NewWorkflowStoreFromConfig(storeType string, db *mongo.Database) (WorkflowStore, error) {
	switch storeType {
	case "mongo":
		if db == nil {
			return nil, errors.New("MongoDB database required for mongo store")
		}
		return NewMongoWorkflowStore(db), nil
	case "memory", "":
		return NewInMemoryWorkflowStore(), nil
	default:
		return nil, fmt.Errorf("unknown workflow store type: %s", storeType)
	}
}
