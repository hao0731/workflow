package eventstore

import "time"

// Execution status constants.
const (
	StatusRunning   = "running"
	StatusCompleted = "completed"
	StatusFailed    = "failed"
)

// Execution represents a workflow execution with linking metadata.
type Execution struct {
	ID                string     `bson:"_id" json:"id"`
	WorkflowID        string     `bson:"workflow_id" json:"workflow_id"`
	Status            string     `bson:"status" json:"status"`
	StartedAt         time.Time  `bson:"started_at" json:"started_at"`
	CompletedAt       *time.Time `bson:"completed_at,omitempty" json:"completed_at,omitempty"`
	ParentExecutionID string     `bson:"parent_execution_id,omitempty" json:"parent_execution_id,omitempty"`
	ChildExecutionIDs []string   `bson:"child_execution_ids,omitempty" json:"child_execution_ids,omitempty"`
	TriggeredByEvent  string     `bson:"triggered_by_event,omitempty" json:"triggered_by_event,omitempty"`
}
