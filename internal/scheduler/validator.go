package scheduler

import (
	"context"
	"fmt"
	"strings"

	cloudevents "github.com/cloudevents/sdk-go/v2"
)

// NodeValidator validates that a node type is registered and enabled.
type NodeValidator interface {
	Exists(ctx context.Context, fullType string) bool
}

// ResultValidator validates worker result events before runtime handoff.
type ResultValidator interface {
	Validate(ctx context.Context, event cloudevents.Event, result NodeResultData) error
}

// ValidationError describes why a worker result failed validation.
type ValidationError struct {
	MissingExtensions []string `json:"missing_extensions,omitempty"`
	Reason            string   `json:"reason,omitempty"`
}

func (e ValidationError) Error() string {
	parts := make([]string, 0, 2)
	if len(e.MissingExtensions) > 0 {
		parts = append(parts, fmt.Sprintf("missing required extensions: %s", strings.Join(e.MissingExtensions, ", ")))
	}
	if e.Reason != "" {
		parts = append(parts, e.Reason)
	}
	if len(parts) == 0 {
		return "worker result validation failed"
	}

	return strings.Join(parts, "; ")
}

// RequiredResultValidator enforces the minimum CloudEvents contract for node results.
type RequiredResultValidator struct{}

// Validate checks required node-result extensions and payload fields.
func (RequiredResultValidator) Validate(_ context.Context, event cloudevents.Event, result NodeResultData) error {
	var missing []string

	requiredStringExtensions := []string{"workflowid", "executionid", "nodeid", "idempotencykey", "producer"}
	for _, key := range requiredStringExtensions {
		if eventExtensionString(event, key) == "" {
			missing = append(missing, key)
		}
	}
	if _, ok := event.Extensions()["runindex"]; !ok {
		missing = append(missing, "runindex")
	}
	if _, ok := event.Extensions()["attempt"]; !ok {
		missing = append(missing, "attempt")
	}
	if len(missing) > 0 {
		return ValidationError{MissingExtensions: missing}
	}
	if result.NodeID == "" {
		return ValidationError{Reason: "node result payload missing node_id"}
	}

	return nil
}
