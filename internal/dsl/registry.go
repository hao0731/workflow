package dsl

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cheriehsieh/orchestration/internal/engine"
)

// WorkflowRegistry manages loaded workflows from DSL files.
// Implements engine.WorkflowRepository for seamless integration.
type WorkflowRegistry struct {
	store     WorkflowStore
	loader    WorkflowLoader
	parser    WorkflowParser
	validator WorkflowValidator
	converter WorkflowConverter
}

// RegistryOption configures WorkflowRegistry.
type RegistryOption func(*WorkflowRegistry)

// NewWorkflowRegistry creates a new WorkflowRegistry with default components.
func NewWorkflowRegistry(opts ...RegistryOption) *WorkflowRegistry {
	r := &WorkflowRegistry{
		store:     NewInMemoryWorkflowStore(),
		loader:    NewFileSystemLoader(""),
		parser:    NewYAMLParser(),
		validator: NewCompositeValidator(NewStructureValidator(), NewDAGValidator()),
		converter: NewDefaultConverter(),
	}

	for _, opt := range opts {
		opt(r)
	}

	return r
}

// WithStore sets a custom store backend.
func WithStore(store WorkflowStore) RegistryOption {
	return func(r *WorkflowRegistry) {
		r.store = store
	}
}

// WithLoader sets a custom loader.
func WithLoader(loader WorkflowLoader) RegistryOption {
	return func(r *WorkflowRegistry) {
		r.loader = loader
	}
}

// WithParser sets a custom parser.
func WithParser(parser WorkflowParser) RegistryOption {
	return func(r *WorkflowRegistry) {
		r.parser = parser
	}
}

// WithValidator sets a custom validator.
func WithValidator(validator WorkflowValidator) RegistryOption {
	return func(r *WorkflowRegistry) {
		r.validator = validator
	}
}

// WithConverter sets a custom converter.
func WithConverter(converter WorkflowConverter) RegistryOption {
	return func(r *WorkflowRegistry) {
		r.converter = converter
	}
}

// LoadFile loads a single workflow file.
func (r *WorkflowRegistry) LoadFile(ctx context.Context, path string) error {
	data, err := r.loader.Load(ctx, path)
	if err != nil {
		return fmt.Errorf("load failed: %w", err)
	}

	def, err := r.parser.Parse(data)
	if err != nil {
		return fmt.Errorf("parse failed: %w", err)
	}

	if err := r.validator.Validate(def); err != nil {
		return fmt.Errorf("validation failed: %w", err)
	}

	wf, err := r.converter.Convert(def)
	if err != nil {
		return fmt.Errorf("conversion failed: %w", err)
	}

	return r.store.Register(ctx, wf, data)
}

// LoadDirectory loads all workflow files from a directory.
func (r *WorkflowRegistry) LoadDirectory(ctx context.Context, dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("failed to read directory %s: %w", dir, err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if !isYAMLFile(name) {
			continue
		}

		path := filepath.Join(dir, name)
		if err := r.LoadFile(ctx, path); err != nil {
			return fmt.Errorf("failed to load %s: %w", name, err)
		}
	}

	return nil
}

// GetByID implements engine.WorkflowRepository.
func (r *WorkflowRegistry) GetByID(ctx context.Context, id string) (*engine.Workflow, error) {
	return r.store.GetByID(ctx, id)
}

// GetSource returns the original YAML source for a workflow.
func (r *WorkflowRegistry) GetSource(ctx context.Context, id string) ([]byte, error) {
	return r.store.GetSource(ctx, id)
}

// ListWorkflows returns all loaded workflow IDs.
func (r *WorkflowRegistry) ListWorkflows() []string {
	ids, _ := r.store.List(context.Background())
	return ids
}

// Register adds or updates a workflow in the registry.
func (r *WorkflowRegistry) Register(wf *engine.Workflow) error {
	if wf == nil {
		return fmt.Errorf("workflow is nil")
	}
	if wf.ID == "" {
		return fmt.Errorf("workflow ID is required")
	}

	return r.store.Register(context.Background(), wf, nil)
}

// Delete removes a workflow from the registry.
func (r *WorkflowRegistry) Delete(id string) error {
	return r.store.Delete(context.Background(), id)
}

// isYAMLFile checks if a filename has a YAML extension.
func isYAMLFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".yaml" || ext == ".yml"
}
