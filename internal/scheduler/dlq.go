package scheduler

import (
	"encoding/json"
	"errors"

	"github.com/google/uuid"

	cloudevents "github.com/cloudevents/sdk-go/v2"

	"github.com/cheriehsieh/orchestration/internal/messaging"
)

// ValidationFailureData is the DLQ payload for invalid worker-result events.
type ValidationFailureData struct {
	EventID           string   `json:"event_id"`
	EventType         string   `json:"event_type"`
	Subject           string   `json:"subject"`
	Error             string   `json:"error"`
	MissingExtensions []string `json:"missing_extensions,omitempty"`
	WorkflowID        string   `json:"workflow_id,omitempty"`
	ExecutionID       string   `json:"execution_id,omitempty"`
	NodeID            string   `json:"node_id,omitempty"`
	OriginalEventJSON []byte   `json:"original_event_json,omitempty"`
}

func newValidationFailureEvent(original cloudevents.Event, cause error) (cloudevents.Event, error) {
	rawEvent, err := json.Marshal(original)
	if err != nil {
		return cloudevents.Event{}, err
	}

	payload := ValidationFailureData{
		EventID:           original.ID(),
		EventType:         original.Type(),
		Subject:           original.Subject(),
		Error:             cause.Error(),
		WorkflowID:        eventExtensionString(original, "workflowid"),
		ExecutionID:       eventExtensionString(original, "executionid"),
		NodeID:            eventExtensionString(original, "nodeid"),
		OriginalEventJSON: rawEvent,
	}

	var validationErr ValidationError
	if errors.As(cause, &validationErr) {
		payload.MissingExtensions = append([]string(nil), validationErr.MissingExtensions...)
	}

	dlqEvent := cloudevents.NewEvent()
	dlqEvent.SetID(uuid.New().String())
	dlqEvent.SetSource(EventSource)
	dlqEvent.SetType(messaging.EventTypeDLQSchedulerValidationV1)
	dlqEvent.SetSubject(original.Subject())
	if payload.WorkflowID != "" {
		dlqEvent.SetExtension("workflowid", payload.WorkflowID)
	}
	if payload.ExecutionID != "" {
		dlqEvent.SetExtension("executionid", payload.ExecutionID)
	}
	if payload.NodeID != "" {
		dlqEvent.SetExtension("nodeid", payload.NodeID)
	}
	if err := dlqEvent.SetData(cloudevents.ApplicationJSON, payload); err != nil {
		return cloudevents.Event{}, err
	}

	return dlqEvent, nil
}
