package registry

import (
	"fmt"
	"time"
)

// NodeRegistration represents a registered node type.
type NodeRegistration struct {
	ID            string         `bson:"_id" json:"id"`
	NodeType      string         `bson:"node_type" json:"node_type"`
	Version       string         `bson:"version" json:"version"`
	FullType      string         `bson:"full_type" json:"full_type"` // e.g., "http-request@v1"
	DisplayName   string         `bson:"display_name" json:"display_name"`
	Description   string         `bson:"description" json:"description,omitempty"`
	InputSchema   map[string]any `bson:"input_schema" json:"input_schema,omitempty"`
	OutputPorts   []string       `bson:"output_ports" json:"output_ports"`
	Owner         string         `bson:"owner" json:"owner"`
	Enabled       bool           `bson:"enabled" json:"enabled"`
	TokenHash     string         `bson:"token_hash" json:"-"` // Hidden from JSON
	Subject       string         `bson:"subject" json:"subject"`
	ConsumerName  string         `bson:"consumer_name" json:"consumer_name"`
	LastHeartbeat *time.Time     `bson:"last_heartbeat" json:"last_heartbeat,omitempty"`
	WorkerCount   int            `bson:"worker_count" json:"worker_count"`
	CreatedAt     time.Time      `bson:"created_at" json:"created_at"`
	UpdatedAt     time.Time      `bson:"updated_at" json:"updated_at"`
}

// BuildFullType creates the versioned node type string.
func BuildFullType(nodeType, version string) string {
	if version == "" {
		version = "v1"
	}
	return fmt.Sprintf("%s@%s", nodeType, version)
}

// BuildSubject creates the NATS subject for this node type.
func BuildSubject(nodeType, version string) string {
	return fmt.Sprintf("workflow.nodes.%s.%s", nodeType, version)
}

// BuildConsumerName creates the NATS consumer name.
func BuildConsumerName(nodeType, version string) string {
	return fmt.Sprintf("worker-%s-%s", nodeType, version)
}

// RegisterRequest is the API request to register a new node type.
type RegisterRequest struct {
	NodeType    string         `json:"node_type" validate:"required"`
	Version     string         `json:"version"` // Optional, defaults to "v1"
	DisplayName string         `json:"display_name" validate:"required"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"input_schema"`
	OutputPorts []string       `json:"output_ports"`
	Owner       string         `json:"owner" validate:"required,email"`
}

// RegisterResponse is the API response after registration.
type RegisterResponse struct {
	ID           string `json:"id"`
	FullType     string `json:"full_type"`
	Subject      string `json:"subject"`
	ConsumerName string `json:"consumer_name"`
	APIToken     string `json:"api_token"`
	CreatedAt    string `json:"created_at"`
}

// ConnectRequest is the API request to get NATS proxy access.
type ConnectRequest struct {
	// Token is passed via Authorization header
}

// ConnectResponse provides NATS connection details.
type ConnectResponse struct {
	ProxyURL      string `json:"proxy_url"`
	Subject       string `json:"subject"`
	ConsumerName  string `json:"consumer_name"`
	ResultSubject string `json:"result_subject"`
}

// HealthRequest is the worker heartbeat request.
type HealthRequest struct {
	WorkerID string `json:"worker_id" validate:"required"`
	Status   string `json:"status"` // "healthy", "busy", "draining"
}

// HealthResponse acknowledges the heartbeat.
type HealthResponse struct {
	Acknowledged bool   `json:"acknowledged"`
	ServerTime   string `json:"server_time"`
}

// NodeInfo is the public info about a registered node.
type NodeInfo struct {
	FullType      string   `json:"full_type"`
	DisplayName   string   `json:"display_name"`
	Description   string   `json:"description"`
	OutputPorts   []string `json:"output_ports"`
	Enabled       bool     `json:"enabled"`
	WorkerCount   int      `json:"worker_count"`
	LastHeartbeat *string  `json:"last_heartbeat,omitempty"`
}
