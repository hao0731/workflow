package engine

import "github.com/cheriehsieh/orchestration/internal/messaging"

// Event types for workflow execution (CloudEvents type field).
const (
	ExecutionStarted       = messaging.EventTypeRuntimeExecutionStartedV1
	NodeExecutionScheduled = messaging.EventTypeRuntimeNodeScheduledV1
	NodeExecutionExecuted  = messaging.EventTypeRuntimeNodeExecutedV1
	NodeExecutionStarted   = "workflow.runtime.node.started.v1"
	ExecutionCompleted     = "workflow.runtime.execution.completed.v1"
	ExecutionFailed        = "workflow.runtime.execution.failed.v1"
)

// Legacy event types kept for the compatibility bridge during the migration window.
const (
	LegacyExecutionStarted       = "orchestration.execution.started"
	LegacyNodeExecutionScheduled = "orchestration.node.scheduled"
	LegacyNodeExecutionStarted   = "orchestration.node.started"
	LegacyNodeExecutionCompleted = "orchestration.node.completed"
	LegacyNodeExecutionFailed    = "orchestration.node.failed"
	LegacyExecutionCompleted     = "orchestration.execution.completed"
	LegacyExecutionFailed        = "orchestration.execution.failed"
)

// Legacy aliases retained for components that still reference the older names internally.
const (
	NodeExecutionCompleted = LegacyNodeExecutionCompleted
	NodeExecutionFailed    = LegacyNodeExecutionFailed
)

// ExecutionStartedData is the data payload for ExecutionStarted events.
type ExecutionStartedData struct {
	WorkflowID string         `json:"workflow_id"`
	InputData  map[string]any `json:"input_data,omitempty"`
}

// NodeExecutionScheduledData is the data payload for NodeExecutionScheduled events.
type NodeExecutionScheduledData struct {
	NodeID    string         `json:"node_id"`
	InputData map[string]any `json:"input_data,omitempty"`
	RunIndex  int            `json:"run_index"`
}

// NodeExecutionStartedData is the data payload for NodeExecutionStarted events.
type NodeExecutionStartedData struct {
	NodeID   string `json:"node_id"`
	RunIndex int    `json:"run_index"`
}

// NodeExecutionCompletedData is the data payload for NodeExecutionCompleted events.
type NodeExecutionCompletedData struct {
	NodeID     string         `json:"node_id"`
	OutputPort string         `json:"output_port"` // Which port the output goes to
	OutputData map[string]any `json:"output_data,omitempty"`
	RunIndex   int            `json:"run_index"`
}

// NodeExecutionFailedData is the data payload for NodeExecutionFailed events.
type NodeExecutionFailedData struct {
	NodeID   string `json:"node_id"`
	RunIndex int    `json:"run_index"`
	Error    string `json:"error"`
}

// NodeExecutionResultData is the v2 runtime handoff payload from scheduler to orchestrator.
type NodeExecutionResultData struct {
	NodeID     string         `json:"node_id"`
	OutputPort string         `json:"output_port,omitempty"`
	OutputData map[string]any `json:"output_data,omitempty"`
	RunIndex   int            `json:"run_index"`
	Error      string         `json:"error,omitempty"`
}

// ExecutionCompletedData is the data payload for ExecutionCompleted events.
type ExecutionCompletedData struct {
	FinalData map[string]any `json:"final_data,omitempty"`
}

// ExecutionFailedData is the data payload for ExecutionFailed events.
type ExecutionFailedData struct {
	Error string `json:"error"`
}
