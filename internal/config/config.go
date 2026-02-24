package config

import (
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

// Config holds all application configuration.
type Config struct {
	// Application
	Env   string
	Port  string
	Debug bool

	// MongoDB
	MongoURI      string
	MongoDatabase string
	MongoUsername string
	MongoPassword string

	// NATS
	NATSURL     string
	NATSStream  string
	NATSSubject string

	// Workflow
	WorkflowStore string

	// Cassandra
	CassandraHosts    string
	CassandraKeyspace string
	CassandraPort     int

	// Migration
	MigrationPhase string // "shadow", "read_switch", "cleanup"
}

// Load reads configuration from environment variables.
// It loads .env file if present (useful for local development).
func Load() *Config {
	_ = godotenv.Load() // Ignore error, env vars might be set by OS

	return &Config{
		// Application
		Env:   getEnv("APP_ENV", "development"),
		Port:  getEnv("PORT", "8081"),
		Debug: getBoolEnv("DEBUG", false),

		// MongoDB
		MongoURI:      getEnv("MONGO_URI", "mongodb://localhost:27017"),
		MongoDatabase: getEnv("MONGO_DATABASE", "orchestration"),
		MongoUsername: getEnv("MONGO_USERNAME", ""),
		MongoPassword: getEnv("MONGO_PASSWORD", ""),

		// NATS
		NATSURL:     getEnv("NATS_URL", "nats://localhost:4222"),
		NATSStream:  getEnv("NATS_STREAM", "WORKFLOW_EVENTS"),
		NATSSubject: getEnv("NATS_SUBJECT", "workflow.events.>"),

		// Workflow
		WorkflowStore: getEnv("WORKFLOW_STORE", "mongo"),

		// Cassandra
		CassandraHosts:    getEnv("CASSANDRA_HOSTS", "localhost"),
		CassandraKeyspace: getEnv("CASSANDRA_KEYSPACE", "orchestration"),
		CassandraPort:     getIntEnv("CASSANDRA_PORT", 9042),

		// Migration
		MigrationPhase: getEnv("MIGRATION_PHASE", "shadow"),
	}
}

// IsDevelopment returns true if running in development mode.
func (c *Config) IsDevelopment() bool {
	return c.Env == "development"
}

// Helper functions

func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}

func getIntEnv(key string, fallback int) int {
	if value, exists := os.LookupEnv(key); exists {
		if i, err := strconv.Atoi(value); err == nil {
			return i
		}
	}
	return fallback
}

func getBoolEnv(key string, fallback bool) bool {
	if value, exists := os.LookupEnv(key); exists {
		if b, err := strconv.ParseBool(value); err == nil {
			return b
		}
	}
	return fallback
}

func getDurationEnv(key string, fallback time.Duration) time.Duration {
	if value, exists := os.LookupEnv(key); exists {
		if d, err := time.ParseDuration(value); err == nil {
			return d
		}
	}
	return fallback
}
