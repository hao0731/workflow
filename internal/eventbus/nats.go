package eventbus

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/nats-io/nats.go"

	cloudevents "github.com/cloudevents/sdk-go/v2"

	"github.com/cheriehsieh/orchestration/internal/messaging"
)

// Compile-time interface compliance checks.
var (
	_ Publisher  = (*NATSEventBus)(nil)
	_ Subscriber = (*NATSEventBus)(nil)
	_ EventBus   = (*NATSEventBus)(nil)
)

// NATSEventBus implements EventBus using NATS JetStream with CloudEvents.
type NATSEventBus struct {
	js           jetStreamClient
	subject      string
	consumerName string
	logger       *slog.Logger
}

type jetStreamClient interface {
	PublishMsg(msg *nats.Msg, opts ...nats.PubOpt) (*nats.PubAck, error)
	PullSubscribe(subj, durable string, opts ...nats.SubOpt) (*nats.Subscription, error)
}

type streamManager interface {
	AddStream(cfg *nats.StreamConfig, opts ...nats.JSOpt) (*nats.StreamInfo, error)
	StreamInfo(stream string, opts ...nats.JSOpt) (*nats.StreamInfo, error)
}

// NATSOption is a functional option for NATSEventBus.
type NATSOption func(*NATSEventBus)

type publishConfig struct {
	subject string
	headers nats.Header
}

// PublishOption configures message publishing.
type PublishOption func(*publishConfig)

// WithLogger sets a custom logger.
func WithLogger(logger *slog.Logger) NATSOption {
	return func(b *NATSEventBus) {
		b.logger = logger
	}
}

// WithPublishSubject overrides the default subject for one publish call.
func WithPublishSubject(subject string) PublishOption {
	return func(cfg *publishConfig) {
		cfg.subject = subject
	}
}

// WithPublishHeaders injects JetStream headers for one publish call.
func WithPublishHeaders(headers nats.Header) PublishOption {
	return func(cfg *publishConfig) {
		cfg.headers = cloneHeader(headers)
	}
}

// NewNATSEventBus creates a new NATS JetStream-backed event bus.
func NewNATSEventBus(js jetStreamClient, subject, consumerName string, opts ...NATSOption) *NATSEventBus {
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
	return b.PublishWithOptions(ctx, event)
}

func (b *NATSEventBus) PublishWithOptions(_ context.Context, event cloudevents.Event, opts ...PublishOption) error {
	data, err := event.MarshalJSON()
	if err != nil {
		return err
	}

	cfg := publishConfig{
		subject: b.subject,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.subject == "" {
		cfg.subject = event.Type()
	}

	msg := nats.NewMsg(cfg.subject)
	msg.Data = data
	if cfg.headers != nil {
		msg.Header = cloneHeader(cfg.headers)
	}

	_, err = b.js.PublishMsg(msg)
	return err
}

func (b *NATSEventBus) Subscribe(ctx context.Context, handler EventHandler) error {
	// Use simple PullSubscribe - let NATS use existing consumer config if present
	sub, err := b.js.PullSubscribe(b.subject, b.consumerName)
	if err != nil {
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			// Fetch with short timeout to allow checking for new messages frequently
			msgs, err := sub.Fetch(10, nats.MaxWait(500*time.Millisecond))
			if err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, nats.ErrTimeout) {
					continue // Timeout is expected when no messages, just retry
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

// EnsureStreams creates the given streams when they do not already exist.
func EnsureStreams(js streamManager, streamConfigs ...nats.StreamConfig) error {
	for _, streamCfg := range streamConfigs {
		if _, err := js.StreamInfo(streamCfg.Name); err == nil {
			continue
		}

		if _, err := js.AddStream(&streamCfg); err != nil {
			return fmt.Errorf("add stream %s: %w", streamCfg.Name, err)
		}
	}

	return nil
}

// BootstrapWorkflowStreams ensures the workflow v2 streams exist and optionally keeps
// scoped legacy streams available during the migration window.
func BootstrapWorkflowStreams(js streamManager, includeLegacy bool, extraStreams ...nats.StreamConfig) error {
	streams := append([]nats.StreamConfig{}, messaging.WorkflowStreams()...)
	if includeLegacy {
		streams = append(streams, messaging.LegacyWorkflowStreams()...)
	}
	streams = append(streams, extraStreams...)

	return EnsureStreams(js, streams...)
}

func cloneHeader(header nats.Header) nats.Header {
	if header == nil {
		return nil
	}

	cloned := make(nats.Header, len(header))
	for key, values := range header {
		cloned[key] = append([]string(nil), values...)
	}

	return cloned
}
