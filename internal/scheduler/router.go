package scheduler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"

	cloudevents "github.com/cloudevents/sdk-go/v2"

	"github.com/cheriehsieh/orchestration/internal/engine"
	"github.com/cheriehsieh/orchestration/internal/eventbus"
	"github.com/cheriehsieh/orchestration/internal/eventstore"
)

// WorkflowMatcher finds workflows that match a given event trigger.
type WorkflowMatcher interface {
	FindByEventTrigger(ctx context.Context, eventName, domain string) ([]*engine.Workflow, error)
}

// EventRouter listens to marketplace events and starts matching workflows.
type EventRouter struct {
	js            nats.JetStreamContext
	workflowRepo  WorkflowMatcher
	eventStore    eventstore.EventStore
	publisher     eventbus.Publisher
	subjectPrefix string
	consumerName  string
	logger        *slog.Logger
}

// EventRouterOption is a functional option for EventRouter.
type EventRouterOption func(*EventRouter)

// WithEventRouterLogger sets a custom logger.
func WithEventRouterLogger(logger *slog.Logger) EventRouterOption {
	return func(r *EventRouter) {
		r.logger = logger
	}
}

// NewEventRouter creates a new marketplace event router.
func NewEventRouter(
	js nats.JetStreamContext,
	repo WorkflowMatcher,
	es eventstore.EventStore,
	pub eventbus.Publisher,
	opts ...EventRouterOption,
) *EventRouter {
	r := &EventRouter{
		js:            js,
		workflowRepo:  repo,
		eventStore:    es,
		publisher:     pub,
		subjectPrefix: "marketplace",
		consumerName:  "event-router",
		logger:        slog.Default(),
	}
	for _, opt := range opts {
		opt(r)
	}
	return r
}

// Start begins listening to marketplace events.
func (r *EventRouter) Start(ctx context.Context) error {
	sub, err := r.js.PullSubscribe(r.subjectPrefix+".>", r.consumerName)
	if err != nil {
		return fmt.Errorf("failed to subscribe to marketplace events: %w", err)
	}

	r.logger.Info("event router started", slog.String("subject", r.subjectPrefix+".>"))

	for {
		select {
		case <-ctx.Done():
			return nil
		default:
			msgs, err := sub.Fetch(10, nats.MaxWait(5000000000)) // 5 seconds
			if err != nil {
				continue
			}

			for _, msg := range msgs {
				if err := r.handleMessage(ctx, msg); err != nil {
					r.logger.Error("failed to handle marketplace event",
						slog.Any("error", err),
						slog.String("subject", msg.Subject),
					)
					_ = msg.Nak()
				} else {
					_ = msg.Ack()
				}
			}
		}
	}
}

func (r *EventRouter) handleMessage(ctx context.Context, msg *nats.Msg) error {
	// Parse the CloudEvent
	event := cloudevents.NewEvent()
	if err := json.Unmarshal(msg.Data, &event); err != nil {
		return fmt.Errorf("invalid event format: %w", err)
	}

	// Extract event name and domain from subject: marketplace.<domain>.<name>
	// e.g. marketplace.ecommerce.order_created
	eventName, domain := parseSubject(msg.Subject)
	if eventName == "" {
		return fmt.Errorf("invalid subject format: %s", msg.Subject)
	}

	r.logger.Debug("received marketplace event",
		slog.String("event_name", eventName),
		slog.String("domain", domain),
	)

	// Find workflows that trigger on this event
	workflows, err := r.workflowRepo.FindByEventTrigger(ctx, eventName, domain)
	if err != nil {
		return err
	}

	if len(workflows) == 0 {
		r.logger.Debug("no workflows match this event",
			slog.String("event_name", eventName),
			slog.String("domain", domain),
		)
		return nil
	}

	// Extract payload from event
	var payload map[string]any
	if err := event.DataAs(&payload); err != nil {
		payload = make(map[string]any)
	}

	// Extract parent execution ID for tracing
	parentExecutionID, _ := event.Extensions()["executionid"].(string)

	// Start each matching workflow
	for _, wf := range workflows {
		executionID := uuid.New().String()

		r.logger.Info("starting workflow from marketplace event",
			slog.String("workflow_id", wf.ID),
			slog.String("execution_id", executionID),
			slog.String("parent_execution_id", parentExecutionID),
			slog.String("event_name", eventName),
		)

		startEvent := cloudevents.NewEvent()
		startEvent.SetID(uuid.New().String())
		startEvent.SetSource(engine.EventSource)
		startEvent.SetType(engine.ExecutionStarted)
		startEvent.SetSubject(executionID)
		startEvent.SetExtension("workflowid", wf.ID)
		if parentExecutionID != "" {
			startEvent.SetExtension("parentexecutionid", parentExecutionID)
		}
		_ = startEvent.SetData(cloudevents.ApplicationJSON, engine.ExecutionStartedData{
			WorkflowID: wf.ID,
			InputData:  payload,
		})

		if err := r.eventStore.Append(ctx, startEvent); err != nil {
			r.logger.Error("failed to store start event", slog.Any("error", err))
			continue
		}

		if err := r.publisher.Publish(ctx, startEvent); err != nil {
			r.logger.Error("failed to publish start event", slog.Any("error", err))
			continue
		}
	}

	return nil
}

// parseSubject extracts domain and event name from subject.
// e.g., "marketplace.ecommerce.order_created" -> ("order_created", "ecommerce")
func parseSubject(subject string) (eventName, domain string) {
	// subject format: marketplace.<domain>.<event_name>
	parts := splitSubject(subject)
	if len(parts) < 3 || parts[0] != "marketplace" {
		return "", ""
	}
	domain = parts[1]
	eventName = parts[2]
	return
}

func splitSubject(s string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '.' {
			parts = append(parts, s[start:i])
			start = i + 1
		}
	}
	parts = append(parts, s[start:])
	return parts
}
