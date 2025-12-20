package eventbus

import (
	"context"
	"log/slog"

	"github.com/nats-io/nats.go"

	cloudevents "github.com/cloudevents/sdk-go/v2"
)

// Compile-time interface compliance checks.
var (
	_ Publisher  = (*NATSEventBus)(nil)
	_ Subscriber = (*NATSEventBus)(nil)
	_ EventBus   = (*NATSEventBus)(nil)
)

// NATSEventBus implements EventBus using NATS JetStream with CloudEvents.
type NATSEventBus struct {
	js           nats.JetStreamContext
	subject      string
	consumerName string
	logger       *slog.Logger
}

// NATSOption is a functional option for NATSEventBus.
type NATSOption func(*NATSEventBus)

// WithLogger sets a custom logger.
func WithLogger(logger *slog.Logger) NATSOption {
	return func(b *NATSEventBus) {
		b.logger = logger
	}
}

// NewNATSEventBus creates a new NATS JetStream-backed event bus.
func NewNATSEventBus(js nats.JetStreamContext, subject, consumerName string, opts ...NATSOption) *NATSEventBus {
	b := &NATSEventBus{
		js:           js,
		subject:      subject,
		consumerName: consumerName,
		logger:       slog.Default(),
	}
	for _, opt := range opts {
		opt(b)
	}
	return b
}

func (b *NATSEventBus) Publish(ctx context.Context, event cloudevents.Event) error {
	data, err := event.MarshalJSON()
	if err != nil {
		return err
	}
	_, err = b.js.Publish(b.subject, data)
	return err
}

func (b *NATSEventBus) Subscribe(ctx context.Context, handler EventHandler) error {
	sub, err := b.js.PullSubscribe(b.subject, b.consumerName)
	if err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			msgs, err := sub.Fetch(1, nats.Context(ctx))
			if err != nil {
				if err == context.Canceled || err == context.DeadlineExceeded {
					return nil
				}
				b.logger.WarnContext(ctx, "fetch error", slog.Any("error", err))
				continue
			}

			for _, msg := range msgs {
				event := cloudevents.NewEvent()
				if err := event.UnmarshalJSON(msg.Data); err != nil {
					b.logger.ErrorContext(ctx, "unmarshal error", slog.Any("error", err))
					_ = msg.Ack()
					continue
				}

				if err := handler(ctx, event); err != nil {
					b.logger.ErrorContext(ctx, "handler error",
						slog.String("event_type", event.Type()),
						slog.Any("error", err),
					)
					_ = msg.Nak()
				} else {
					_ = msg.Ack()
				}
			}
		}
	}
}
