package dsl

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// WorkflowParser parses raw content into DSL models.
type WorkflowParser interface {
	Parse(data []byte) (*WorkflowDefinition, error)
}

// YAMLParser parses YAML workflow definitions.
type YAMLParser struct{}

// NewYAMLParser creates a new YAMLParser.
func NewYAMLParser() *YAMLParser {
	return &YAMLParser{}
}

// Parse parses YAML data into a WorkflowDefinition.
func (p *YAMLParser) Parse(data []byte) (*WorkflowDefinition, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("empty input")
	}

	var def WorkflowDefinition
	if err := yaml.Unmarshal(data, &def); err != nil {
		return nil, fmt.Errorf("failed to parse YAML: %w", err)
	}

	return &def, nil
}
