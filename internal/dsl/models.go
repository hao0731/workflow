package dsl

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
