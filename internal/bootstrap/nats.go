package bootstrap

import (
	"fmt"

	"github.com/nats-io/nats.go"

	"github.com/cheriehsieh/orchestration/internal/config"
	"github.com/cheriehsieh/orchestration/internal/eventbus"
	"github.com/cheriehsieh/orchestration/internal/messaging"
)

// NATSConnection is the subset of a NATS connection needed by service bootstrap.
type NATSConnection interface {
	Close()
	JetStream(opts ...nats.JSOpt) (nats.JetStreamContext, error)
}

// NATSConnectFunc establishes a NATS connection.
type NATSConnectFunc func(url string, opts ...nats.Option) (NATSConnection, error)

// DefaultNATSConnect opens a production NATS connection.
func DefaultNATSConnect(url string, opts ...nats.Option) (NATSConnection, error) {
	return nats.Connect(url, opts...)
}

// StreamBootstrapFunc provisions required JetStream streams.
type StreamBootstrapFunc func(js nats.JetStreamContext, includeLegacy bool, extraStreams ...nats.StreamConfig) error

// ConnectNATS creates the JetStream connection and ensures required streams exist.
func ConnectNATS(
	cfg *config.Config,
	connect NATSConnectFunc,
	bootstrap StreamBootstrapFunc,
	includeLegacy bool,
	extraStreams ...nats.StreamConfig,
) (NATSConnection, nats.JetStreamContext, error) {
	conn, err := connect(cfg.NATSURL)
	if err != nil {
		return nil, nil, fmt.Errorf("connect nats: %w", err)
	}

	js, err := conn.JetStream()
	if err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("create jetstream context: %w", err)
	}

	if err := bootstrap(js, includeLegacy, extraStreams...); err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("bootstrap workflow streams: %w", err)
	}

	return conn, js, nil
}

// DefaultNATSBootstrap ensures the standard workflow streams exist.
func DefaultNATSBootstrap(js nats.JetStreamContext, includeLegacy bool, extraStreams ...nats.StreamConfig) error {
	return eventbus.BootstrapWorkflowStreams(js, includeLegacy, extraStreams...)
}

// WorkflowStreamNames returns the configured stream names in bootstrap order.
func WorkflowStreamNames(includeLegacy bool, extraStreams ...nats.StreamConfig) []string {
	streams := append([]nats.StreamConfig{}, messaging.WorkflowStreams()...)
	if includeLegacy {
		streams = append(streams, messaging.LegacyWorkflowStreams()...)
	}
	streams = append(streams, extraStreams...)

	names := make([]string, 0, len(streams))
	for _, stream := range streams {
		names = append(names, stream.Name)
	}

	return names
}
