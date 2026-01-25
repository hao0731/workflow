package api

import "time"

// ExecutionResponse represents an execution for API responses.
type ExecutionResponse struct {
	ID         string     `json:"id"`
	WorkflowID string     `json:"workflow_id"`
	Status     string     `json:"status"`
	StartedAt  time.Time  `json:"started_at"`
	EndedAt    *time.Time `json:"ended_at,omitempty"`
	DurationMs *float64   `json:"duration_ms,omitempty"`
}

// ExecutionEventResponse represents an event in the execution timeline.
type ExecutionEventResponse struct {
	ID        string         `json:"id"`
	Type      string         `json:"type"`
	NodeID    string         `json:"node_id,omitempty"`
	Data      map[string]any `json:"data,omitempty"`
	Timestamp time.Time      `json:"timestamp"`
}

// WorkflowDetailResponse extends workflow with metadata and stats.
type WorkflowDetailResponse struct {
	ID          string               `json:"id"`
	Name        string               `json:"name"`
	Description string               `json:"description"`
	Version     string               `json:"version"`
	Nodes       []NodeResponse       `json:"nodes"`
	Connections []ConnectionResponse `json:"connections"`
	Stats       WorkflowStats        `json:"stats"`
}

// NodeResponse represents a node in API responses.
type NodeResponse struct {
	ID         string         `json:"id"`
	Type       string         `json:"type"`
	Name       string         `json:"name"`
	Parameters map[string]any `json:"parameters,omitempty"`
}

// ConnectionResponse represents a connection in API responses.
type ConnectionResponse struct {
	FromNode string `json:"from_node"`
	FromPort string `json:"from_port,omitempty"`
	ToNode   string `json:"to_node"`
	ToPort   string `json:"to_port,omitempty"`
}

// WorkflowStats provides workflow statistics.
type WorkflowStats struct {
	NodeCount       int    `json:"node_count"`
	ConnectionCount int    `json:"connection_count"`
	HasEventTrigger bool   `json:"has_event_trigger"`
	TriggerEvent    string `json:"trigger_event,omitempty"`
}

// StreamMessage is sent over WebSocket for real-time updates.
type StreamMessage struct {
	Type      string                  `json:"type"` // "event" | "heartbeat" | "error"
	Event     *ExecutionEventResponse `json:"event,omitempty"`
	Timestamp time.Time               `json:"timestamp"`
	Error     string                  `json:"error,omitempty"`
}
