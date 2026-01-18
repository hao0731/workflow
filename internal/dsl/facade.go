package dsl

import (
	"context"

	"github.com/cheriehsieh/orchestration/internal/engine"
)

// LoadWorkflowFromFile is a convenience function for loading a single workflow file.
func LoadWorkflowFromFile(path string) (*engine.Workflow, error) {
	loader := NewFileSystemLoader("")
	parser := NewYAMLParser()
	validator := NewCompositeValidator(
		NewStructureValidator(),
		NewDAGValidator(),
	)
	converter := NewDefaultConverter()

	data, err := loader.Load(context.Background(), path)
	if err != nil {
		return nil, err
	}

	def, err := parser.Parse(data)
	if err != nil {
		return nil, err
	}

	if err := validator.Validate(def); err != nil {
		return nil, err
	}

	return converter.Convert(def)
}

// LoadWorkflowFromBytes parses workflow YAML from bytes.
func LoadWorkflowFromBytes(data []byte) (*engine.Workflow, error) {
	parser := NewYAMLParser()
	validator := NewCompositeValidator(
		NewStructureValidator(),
		NewDAGValidator(),
	)
	converter := NewDefaultConverter()

	def, err := parser.Parse(data)
	if err != nil {
		return nil, err
	}

	if err := validator.Validate(def); err != nil {
		return nil, err
	}

	return converter.Convert(def)
}
