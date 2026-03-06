package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"

	cloudevents "github.com/cloudevents/sdk-go/v2"

	"github.com/cheriehsieh/orchestration/internal/marketplace"
)

// PublishEventExecutor executes the PublishEvent node.
type PublishEventExecutor struct {
	js       nats.JetStreamContext
	registry marketplace.EventRegistry
	logger   *slog.Logger
}

// PublishEventOption is a functional option.
type PublishEventOption func(*PublishEventExecutor)

// WithPublishEventLogger sets a custom logger.
func WithPublishEventLogger(logger *slog.Logger) PublishEventOption {
	return func(e *PublishEventExecutor) {
		e.logger = logger
	}
}

// NewPublishEventExecutor creates a new executor for PublishEvent nodes.
func NewPublishEventExecutor(js nats.JetStreamContext, registry marketplace.EventRegistry, opts ...PublishEventOption) *PublishEventExecutor {
	e := &PublishEventExecutor{
		js:       js,
		registry: registry,
		logger:   slog.Default(),
	}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// NewPublishEventWorkerHandler adapts the publish-event executor to the worker handler contract.
func NewPublishEventWorkerHandler(
	js nats.JetStreamContext,
	registry marketplace.EventRegistry,
	logger *slog.Logger,
) func(ctx context.Context, input, params map[string]any) (NodeResult, error) {
	executor := NewPublishEventExecutor(js, registry, WithPublishEventLogger(logger))

	return func(ctx context.Context, input, params map[string]any) (NodeResult, error) {
		executionID, _ := input["_execution_id"].(string)
		if executionID == "" {
			executionID = uuid.New().String()
		}

		return executor.Execute(ctx, executionID, input, params)
	}
}

// Execute publishes an event to the marketplace.
func (e *PublishEventExecutor) Execute(ctx context.Context, executionID string, input, params map[string]any) (NodeResult, error) {
	eventName, _ := params["event_name"].(string)
	domain, _ := params["domain"].(string)
	payloadTemplate, _ := params["payload"].(map[string]any)

	if eventName == "" || domain == "" {
		return NodeResult{Port: PortFailure}, fmt.Errorf("event_name and domain are required")
	}

	// Validate event exists in registry (optional schema validation)
	def, err := e.registry.Get(ctx, domain, eventName)
	if err != nil {
		e.logger.Warn("event not registered, publishing anyway",
			slog.String("domain", domain),
			slog.String("event_name", eventName),
		)
		// Allow publishing unregistered events for flexibility
		def = &marketplace.EventDefinition{Domain: domain, Name: eventName}
	}

	// Build payload from template and input
	payload := buildPayload(payloadTemplate, input)

	// Create CloudEvent
	event := cloudevents.NewEvent()
	event.SetID(uuid.New().String())
	event.SetSource(EventSource)
	event.SetType("marketplace." + domain + "." + eventName)
	event.SetSubject(def.BuildSubject())
	event.SetExtension("executionid", executionID) // Trace context
	_ = event.SetData(cloudevents.ApplicationJSON, payload)

	// Publish to NATS
	data, err := json.Marshal(event)
	if err != nil {
		return NodeResult{Port: PortFailure}, fmt.Errorf("marshal event: %w", err)
	}
	_, err = e.js.Publish(def.BuildSubject(), data)
	if err != nil {
		e.logger.Error("failed to publish marketplace event",
			slog.Any("error", err),
			slog.String("subject", def.BuildSubject()),
		)
		return NodeResult{Output: map[string]any{"error": err.Error()}, Port: PortFailure}, err
	}

	e.logger.Info("published marketplace event",
		slog.String("subject", def.BuildSubject()),
		slog.String("execution_id", executionID),
	)

	return NodeResult{
		Output: map[string]any{
			"published": true,
			"subject":   def.BuildSubject(),
		},
		Port: PortSuccess,
	}, nil
}

// buildPayload resolves template placeholders from input.
// Simple implementation: if value is string starting with "{{." and ending with "}}", extract from input.
func buildPayload(template map[string]any, input map[string]any) map[string]any {
	if template == nil {
		return input
	}

	result := make(map[string]any)
	for k, v := range template {
		if str, ok := v.(string); ok && len(str) > 5 && str[:3] == "{{." && str[len(str)-2:] == "}}" {
			// Extract field path, e.g., "{{.input.id}}" -> "id"
			path := str[3 : len(str)-2]
			if len(path) > 6 && path[:6] == "input." {
				field := path[6:]
				if val, exists := input[field]; exists {
					result[k] = val
					continue
				}
			}
		}
		result[k] = v
	}
	return result
}
