package eventbus

import (
	"context"

	cloudevents "github.com/cloudevents/sdk-go/v2"
)

// EventHandler is the callback function for handling received CloudEvents.
type EventHandler func(ctx context.Context, event cloudevents.Event) error

// Publisher publishes CloudEvents to the bus.
type Publisher interface {
	Publish(ctx context.Context, event cloudevents.Event) error
}

// Subscriber subscribes to CloudEvents from the bus.
type Subscriber interface {
	Subscribe(ctx context.Context, handler EventHandler) error
}

// EventBus combines both publishing and subscribing capabilities.
type EventBus interface {
	Publisher
	Subscriber
}
