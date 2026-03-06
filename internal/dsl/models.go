package dsl

import (
	"time"

	"github.com/cheriehsieh/orchestration/internal/engine"
)

// WorkflowDefinition represents the top-level YAML structure.
type WorkflowDefinition struct {
	ID          string            `yaml:"id" json:"id"`
	Name        string            `yaml:"name" json:"name"`
	Description string            `yaml:"description" json:"description"`
	Version     string            `yaml:"version" json:"version"`
	Nodes       []NodeDefinition  `yaml:"nodes" json:"nodes"`
	Connections []ConnectionDef   `yaml:"connections" json:"connections"`
	Events      []EventDefinition `yaml:"events" json:"events"`
}

// NodeDefinition represents a node in the YAML DSL.
type NodeDefinition struct {
	ID         string         `yaml:"id" json:"id"`
	Type       string         `yaml:"type" json:"type"` // e.g., "StartNode", "http-request@v1"
	Name       string         `yaml:"name" json:"name"`
	Parameters map[string]any `yaml:"parameters" json:"parameters"`
	Trigger    *TriggerDef    `yaml:"trigger" json:"trigger"`
}

// ConnectionDef represents a connection between nodes.
type ConnectionDef struct {
	From     string `yaml:"from" json:"from"`
	FromPort string `yaml:"from_port" json:"from_port"`
	To       string `yaml:"to" json:"to"`
	ToPort   string `yaml:"to_port" json:"to_port"`
}

// TriggerDef represents an event trigger configuration for StartNode.
type TriggerDef struct {
	Type     string         `yaml:"type" json:"type"` // "manual" or "event"
	Criteria map[string]any `yaml:"criteria" json:"criteria"`
	InputMap map[string]any `yaml:"input_map" json:"input_map"`
}

// EventDefinition for marketplace registration.
type EventDefinition struct {
	Name        string `yaml:"name" json:"name"`
	Domain      string `yaml:"domain" json:"domain"`
	Description string `yaml:"description" json:"description"`
	Schema      any    `yaml:"schema" json:"schema"`
}

// WorkflowRecord contains the persisted workflow definition metadata and runtime graph.
type WorkflowRecord struct {
	ID          string            `json:"id" bson:"_id"`
	Name        string            `json:"name,omitempty" bson:"name,omitempty"`
	Description string            `json:"description,omitempty" bson:"description,omitempty"`
	Version     string            `json:"version,omitempty" bson:"version,omitempty"`
	Source      []byte            `json:"source,omitempty" bson:"source"`
	Events      []EventDefinition `json:"events,omitempty" bson:"events,omitempty"`
	Workflow    *engine.Workflow  `json:"-" bson:"-"`
	CreatedAt   time.Time         `json:"created_at,omitempty" bson:"created_at,omitempty"`
	UpdatedAt   time.Time         `json:"updated_at,omitempty" bson:"updated_at,omitempty"`
}

// NewWorkflowRecord builds a persisted workflow record from the parsed DSL and runtime graph.
func NewWorkflowRecord(def *WorkflowDefinition, wf *engine.Workflow, source []byte) *WorkflowRecord {
	record := &WorkflowRecord{
		Source:   source,
		Workflow: wf,
	}

	if wf != nil {
		record.ID = wf.ID
	}

	if def == nil {
		return record
	}

	record.ID = def.ID
	record.Name = def.Name
	record.Description = def.Description
	record.Version = def.Version
	record.Events = append([]EventDefinition(nil), def.Events...)

	return record
}
