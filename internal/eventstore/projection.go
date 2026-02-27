package eventstore

import (
	"context"
	"fmt"
	"log/slog"

	cloudevents "github.com/cloudevents/sdk-go/v2"

	"github.com/cheriehsieh/orchestration/internal/eventbus"
)

// ProjectionConsumer projects events from the event log into MongoDB read models.
type ProjectionConsumer struct {
	mongo      EventStore
	subscriber eventbus.Subscriber
	logger     *slog.Logger
}

// NewProjectionConsumer creates a new projection consumer.
func NewProjectionConsumer(mongo EventStore, opts ...ProjectionOption) *ProjectionConsumer {
	p := &ProjectionConsumer{
		mongo:  mongo,
		logger: slog.Default(),
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// ProjectionOption configures the ProjectionConsumer.
type ProjectionOption func(*ProjectionConsumer)

// WithProjectionLogger sets the logger.
func WithProjectionLogger(logger *slog.Logger) ProjectionOption {
	return func(p *ProjectionConsumer) {
		p.logger = logger
	}
}

// WithSubscriber sets the event subscriber.
func WithSubscriber(sub eventbus.Subscriber) ProjectionOption {
	return func(p *ProjectionConsumer) {
		p.subscriber = sub
	}
}

// Start begins listening for events and projecting them.
func (p *ProjectionConsumer) Start(ctx context.Context) error {
	if p.subscriber == nil {
		return fmt.Errorf("projection consumer: subscriber not configured")
	}
	return p.subscriber.Subscribe(ctx, p.HandleEvent)
}

// HandleEvent processes a single event and projects it into MongoDB.
// This is the callback to be used with an eventbus.Subscriber.
// Idempotency: MongoDB uses the event ID as _id, so duplicate deliveries
// from NATS retries are safely handled via upsert semantics.
func (p *ProjectionConsumer) HandleEvent(ctx context.Context, event cloudevents.Event) error {
	p.logger.DebugContext(ctx, "projecting event",
		slog.String("id", event.ID()),
		slog.String("type", event.Type()),
		slog.String("subject", event.Subject()),
	)

	// Append the raw event to MongoDB for read queries
	if err := p.mongo.Append(ctx, event); err != nil {
		p.logger.ErrorContext(ctx, "failed to project event",
			slog.String("id", event.ID()),
			slog.Any("error", err),
		)
		return err
	}

	return nil
}
