package engine

import (
	"context"

	"github.com/cheriehsieh/orchestration/internal/eventbus"
	"github.com/cheriehsieh/orchestration/internal/eventstore"
	cloudevents "github.com/cloudevents/sdk-go/v2"
)

// WorkflowRepository provides access to workflow definitions.
type WorkflowRepository interface {
	GetByID(ctx context.Context, workflowID string) (*Workflow, error)
}

// Re-export commonly used types for convenience.
type (
	Event        = cloudevents.Event
	EventStore   = eventstore.EventStore
	Publisher    = eventbus.Publisher
	Subscriber   = eventbus.Subscriber
	EventHandler = eventbus.EventHandler
)

// CloudEvent source for this engine.
const EventSource = "orchestration/engine"
