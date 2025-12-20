package scheduler

import (
	"context"
	"fmt"

	"github.com/nats-io/nats.go"

	cloudevents "github.com/cloudevents/sdk-go/v2"
)

// NATSDispatcher implements WorkerDispatcher using NATS JetStream.
// It publishes to subjects like: workflow.nodes.<node_type>
type NATSDispatcher struct {
	js            nats.JetStreamContext
	subjectPrefix string // e.g., "workflow.nodes"
}

// Compile-time interface compliance check.
var _ WorkerDispatcher = (*NATSDispatcher)(nil)

// NewNATSDispatcher creates a dispatcher that publishes to node-type-specific subjects.
func NewNATSDispatcher(js nats.JetStreamContext, subjectPrefix string) *NATSDispatcher {
	return &NATSDispatcher{
		js:            js,
		subjectPrefix: subjectPrefix,
	}
}

func (d *NATSDispatcher) Dispatch(ctx context.Context, nodeType string, event cloudevents.Event) error {
	subject := fmt.Sprintf("%s.%s", d.subjectPrefix, nodeType)

	data, err := event.MarshalJSON()
	if err != nil {
		return err
	}

	_, err = d.js.Publish(subject, data)
	return err
}
