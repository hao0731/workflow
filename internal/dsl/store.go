package dsl

import (
	"context"

	"github.com/cheriehsieh/orchestration/internal/engine"
)

// WorkflowStore provides persistence for workflow definitions.
type WorkflowStore interface {
	// Register adds or updates a workflow with its source.
	Register(ctx context.Context, wf *engine.Workflow, source []byte) error

	// GetByID retrieves a workflow by its ID.
	GetByID(ctx context.Context, id string) (*engine.Workflow, error)

	// GetSource retrieves the original YAML source for a workflow.
	GetSource(ctx context.Context, id string) ([]byte, error)

	// List returns all workflow IDs.
	List(ctx context.Context) ([]string, error)

	// Delete removes a workflow by its ID.
	Delete(ctx context.Context, id string) error
}
