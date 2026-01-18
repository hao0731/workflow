package dsl

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/cheriehsieh/orchestration/internal/engine"
)

// WorkflowRegistry manages loaded workflows from DSL files.
// Implements engine.WorkflowRepository for seamless integration.
type WorkflowRegistry struct {
	loader    WorkflowLoader
	parser    WorkflowParser
	validator WorkflowValidator
	converter WorkflowConverter
	workflows map[string]*engine.Workflow
	mu        sync.RWMutex
}

// RegistryOption configures WorkflowRegistry.
type RegistryOption func(*WorkflowRegistry)

// NewWorkflowRegistry creates a new WorkflowRegistry with default components.
func NewWorkflowRegistry(opts ...RegistryOption) *WorkflowRegistry {
	r := &WorkflowRegistry{
		loader:    NewFileSystemLoader(""),
		parser:    NewYAMLParser(),
		validator: NewCompositeValidator(NewStructureValidator(), NewDAGValidator()),
		converter: NewDefaultConverter(),
		workflows: make(map[string]*engine.Workflow),
	}

	for _, opt := range opts {
		opt(r)
	}

	return r
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

	r.mu.Lock()
	r.workflows[wf.ID] = wf
	r.mu.Unlock()

	return nil
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
	r.mu.RLock()
	defer r.mu.RUnlock()

	wf, ok := r.workflows[id]
	if !ok {
		return nil, fmt.Errorf("workflow not found: %s", id)
	}

	return wf, nil
}

// ListWorkflows returns all loaded workflow IDs.
func (r *WorkflowRegistry) ListWorkflows() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	ids := make([]string, 0, len(r.workflows))
	for id := range r.workflows {
		ids = append(ids, id)
	}
	return ids
}

// isYAMLFile checks if a filename has a YAML extension.
func isYAMLFile(name string) bool {
	ext := strings.ToLower(filepath.Ext(name))
	return ext == ".yaml" || ext == ".yml"
}
