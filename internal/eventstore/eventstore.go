package eventstore

import (
	"context"
	"time"

	cloudevents "github.com/cloudevents/sdk-go/v2"
)

// StoredEvent is the MongoDB representation of a CloudEvent.
type StoredEvent struct {
	ID              string         `bson:"_id"`
	Source          string         `bson:"source"`
	Type            string         `bson:"type"`
	Subject         string         `bson:"subject,omitempty"`
	Time            time.Time      `bson:"time"`
	DataContentType string         `bson:"datacontenttype,omitempty"`
	Data            map[string]any `bson:"data"`
	Extensions      map[string]any `bson:"extensions,omitempty"`
}

// ExecutionSummary is a lightweight representation of an execution for API responses.
type ExecutionSummary struct {
	ID         string    `json:"id"`
	WorkflowID string    `json:"workflow_id"`
	Status     string    `json:"status"`
	StartedAt  time.Time `json:"started_at"`
}

// EventStore handles persistence of CloudEvents.
type EventStore interface {
	Append(ctx context.Context, event cloudevents.Event) error
	GetBySubject(ctx context.Context, subject string) ([]cloudevents.Event, error)
	GetExecutionsByWorkflow(ctx context.Context, workflowID string) ([]ExecutionSummary, error)
	// GetEventsByExecution returns all events for a given execution ID.
	GetEventsByExecution(ctx context.Context, executionID string, since *time.Time) ([]cloudevents.Event, error)
}

// FromCloudEvent converts a CloudEvents event to a StoredEvent.
func FromCloudEvent(event cloudevents.Event) StoredEvent {
	var data map[string]any
	_ = event.DataAs(&data)

	extensions := make(map[string]any)
	for k, v := range event.Extensions() {
		extensions[k] = v
	}

	return StoredEvent{
		ID:              event.ID(),
		Source:          event.Source(),
		Type:            event.Type(),
		Subject:         event.Subject(),
		Time:            event.Time(),
		DataContentType: event.DataContentType(),
		Data:            data,
		Extensions:      extensions,
	}
}

// ToCloudEvent converts a StoredEvent back to a CloudEvents event.
func (s StoredEvent) ToCloudEvent() cloudevents.Event {
	event := cloudevents.NewEvent()
	event.SetID(s.ID)
	event.SetSource(s.Source)
	event.SetType(s.Type)
	event.SetSubject(s.Subject)
	event.SetTime(s.Time)
	_ = event.SetData(cloudevents.ApplicationJSON, s.Data)

	for k, v := range s.Extensions {
		event.SetExtension(k, v)
	}

	return event
}
