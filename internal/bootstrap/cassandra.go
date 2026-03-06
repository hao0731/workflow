package bootstrap

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/gocql/gocql"

	"github.com/cheriehsieh/orchestration/internal/config"
)

const defaultCassandraConnectAttempts = 5

// CassandraConfig captures the deterministic session dial inputs.
type CassandraConfig struct {
	Hosts       []string
	Port        int
	Keyspace    string
	Consistency gocql.Consistency
}

// CassandraSession is the subset of session behavior shared by tests and production code.
type CassandraSession interface {
	Close()
}

// CassandraDialFunc opens a Cassandra session using the supplied config.
type CassandraDialFunc func(cfg CassandraConfig) (CassandraSession, error)

// BuildCassandraConfig translates application config into Cassandra dial settings.
func BuildCassandraConfig(cfg *config.Config) CassandraConfig {
	return CassandraConfig{
		Hosts:       strings.Split(cfg.CassandraHosts, ","),
		Port:        cfg.CassandraPort,
		Keyspace:    cfg.CassandraKeyspace,
		Consistency: gocql.LocalQuorum,
	}
}

// ConnectCassandra retries the supplied dialer until it succeeds or attempts are exhausted.
func ConnectCassandra(
	cfg CassandraConfig,
	logger *slog.Logger,
	dial CassandraDialFunc,
	sleep func(time.Duration),
	maxAttempts int,
) (CassandraSession, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if sleep == nil {
		sleep = time.Sleep
	}
	if maxAttempts <= 0 {
		maxAttempts = defaultCassandraConnectAttempts
	}

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		session, err := dial(cfg)
		if err == nil {
			return session, nil
		}

		lastErr = err
		if attempt == maxAttempts {
			break
		}

		logger.Warn("failed to connect to Cassandra, retrying...",
			slog.Int("attempt", attempt),
			slog.Any("error", err),
		)
		sleep(time.Duration(attempt) * 2 * time.Second)
	}

	if lastErr == nil {
		lastErr = errors.New("unknown Cassandra connection error")
	}

	return nil, fmt.Errorf("connect cassandra after %d attempts: %w", maxAttempts, lastErr)
}

// OpenCassandraSession creates a production Cassandra session using application config.
func OpenCassandraSession(cfg *config.Config, logger *slog.Logger) (*gocql.Session, error) {
	session, err := ConnectCassandra(
		BuildCassandraConfig(cfg),
		logger,
		func(cassandraCfg CassandraConfig) (CassandraSession, error) {
			cluster := gocql.NewCluster(cassandraCfg.Hosts...)
			cluster.Port = cassandraCfg.Port
			cluster.Keyspace = cassandraCfg.Keyspace
			cluster.Consistency = cassandraCfg.Consistency

			return cluster.CreateSession()
		},
		time.Sleep,
		defaultCassandraConnectAttempts,
	)
	if err != nil {
		return nil, err
	}

	typedSession, ok := session.(*gocql.Session)
	if !ok {
		return nil, fmt.Errorf("unexpected Cassandra session type %T", session)
	}

	return typedSession, nil
}
