package bootstrap

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/gocql/gocql"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/cheriehsieh/orchestration/internal/config"
	"github.com/cheriehsieh/orchestration/internal/messaging"
)

type stubMongoConnector struct {
	called bool
	opts   *options.ClientOptions
	client *mongo.Client
	err    error
}

func (c *stubMongoConnector) Connect(_ context.Context, opts *options.ClientOptions) (*mongo.Client, error) {
	c.called = true
	c.opts = opts

	return c.client, c.err
}

type stubJetStream struct {
	nats.JetStreamContext
}

type stubNATSConn struct {
	js  nats.JetStreamContext
	err error
}

func (c *stubNATSConn) JetStream(_ ...nats.JSOpt) (nats.JetStreamContext, error) {
	return c.js, c.err
}

func (c *stubNATSConn) Close() {}

type stubNATSConnector struct {
	called bool
	url    string
	conn   NATSConnection
	err    error
}

func (c *stubNATSConnector) Connect(url string, _ ...nats.Option) (NATSConnection, error) {
	c.called = true
	c.url = url

	return c.conn, c.err
}

type stubCassandraSession struct {
	closed bool
}

func (s *stubCassandraSession) Close() {
	s.closed = true
}

func TestLoggerUsesConfigEnvironment(t *testing.T) {
	t.Parallel()

	devLogger := NewLogger(&config.Config{Env: "development"})
	prodLogger := NewLogger(&config.Config{Env: "production"})

	assert.True(t, devLogger.Enabled(context.Background(), slog.LevelDebug))
	assert.False(t, prodLogger.Enabled(context.Background(), slog.LevelDebug))
}

func TestConnectMongoUsesConfigOptions(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		MongoURI:      "mongodb://example:27017",
		MongoDatabase: "workflow",
		MongoUsername: "demo",
		MongoPassword: "secret",
	}
	connector := &stubMongoConnector{client: &mongo.Client{}}

	client, db, err := ConnectMongo(context.Background(), cfg, connector.Connect)
	require.NoError(t, err)

	require.True(t, connector.called)
	require.Same(t, connector.client, client)
	require.NotNil(t, connector.opts)
	assert.Equal(t, cfg.MongoURI, connector.opts.GetURI())
	require.NotNil(t, connector.opts.Auth)
	assert.Equal(t, cfg.MongoUsername, connector.opts.Auth.Username)
	assert.Equal(t, cfg.MongoPassword, connector.opts.Auth.Password)
	assert.Equal(t, cfg.MongoDatabase, db.Name())
}

func TestConnectCassandraRetriesBeforeReturningSession(t *testing.T) {
	t.Parallel()

	cfg := CassandraConfig{
		Hosts:       []string{"cassandra-a", "cassandra-b"},
		Port:        9042,
		Keyspace:    "workflow",
		Consistency: gocql.LocalQuorum,
	}

	var attempts int
	var waits []time.Duration

	session, err := ConnectCassandra(cfg, nil, func(CassandraConfig) (CassandraSession, error) {
		attempts++
		if attempts < 3 {
			return nil, errors.New("not yet")
		}

		return &stubCassandraSession{}, nil
	}, func(wait time.Duration) {
		waits = append(waits, wait)
	}, 5)
	require.NoError(t, err)

	require.NotNil(t, session)
	assert.Equal(t, 3, attempts)
	assert.Equal(t, []time.Duration{2 * time.Second, 4 * time.Second}, waits)
}

func TestConnectNATSBootstrapsWorkflowStreams(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{
		NATSURL: "nats://workflow-nats:4222",
	}
	extra := nats.StreamConfig{Name: "MARKETPLACE", Subjects: []string{"marketplace.>"}}

	connector := &stubNATSConnector{
		conn: &stubNATSConn{js: &stubJetStream{}},
	}

	var (
		gotLegacy bool
		gotExtra  []nats.StreamConfig
	)

	conn, js, err := ConnectNATS(cfg, connector.Connect, func(gotJS nats.JetStreamContext, includeLegacy bool, extraStreams ...nats.StreamConfig) error {
		require.IsType(t, &stubJetStream{}, gotJS)
		gotLegacy = includeLegacy
		gotExtra = append(gotExtra, extraStreams...)

		return nil
	}, true, extra)
	require.NoError(t, err)

	require.True(t, connector.called)
	assert.Equal(t, cfg.NATSURL, connector.url)
	require.Same(t, connector.conn, conn)
	require.IsType(t, &stubJetStream{}, js)
	assert.True(t, gotLegacy)
	require.Len(t, gotExtra, 1)
	assert.Equal(t, messaging.StreamNameCommands, WorkflowStreamNames(true, extra)[0])
	assert.Equal(t, extra.Name, gotExtra[0].Name)
}
