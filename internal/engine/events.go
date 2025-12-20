package engine

// Event types for workflow execution (CloudEvents type field).
const (
	ExecutionStarted       = "orchestration.execution.started"
	NodeExecutionScheduled = "orchestration.node.scheduled"
	NodeExecutionStarted   = "orchestration.node.started"
	NodeExecutionCompleted = "orchestration.node.completed"
	NodeExecutionFailed    = "orchestration.node.failed"
	ExecutionCompleted     = "orchestration.execution.completed"
	ExecutionFailed        = "orchestration.execution.failed"
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
	OutputData map[string]any `json:"output_data,omitempty"`
	RunIndex   int            `json:"run_index"`
}

// NodeExecutionFailedData is the data payload for NodeExecutionFailed events.
type NodeExecutionFailedData struct {
	NodeID   string `json:"node_id"`
	RunIndex int    `json:"run_index"`
	Error    string `json:"error"`
}

// ExecutionCompletedData is the data payload for ExecutionCompleted events.
type ExecutionCompletedData struct {
	FinalData map[string]any `json:"final_data,omitempty"`
}

// ExecutionFailedData is the data payload for ExecutionFailed events.
type ExecutionFailedData struct {
	Error string `json:"error"`
}
