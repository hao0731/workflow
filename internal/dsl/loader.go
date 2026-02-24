package dsl

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// WorkflowLoader loads raw workflow content from various sources.
type WorkflowLoader interface {
	Load(ctx context.Context, source string) ([]byte, error)
}

// FileSystemLoader loads workflows from the filesystem.
type FileSystemLoader struct {
	basePath string
}

// NewFileSystemLoader creates a new FileSystemLoader.
// If basePath is provided, it will be prepended to all load paths.
func NewFileSystemLoader(basePath string) *FileSystemLoader {
	return &FileSystemLoader{basePath: basePath}
}

// Load reads a workflow file from the filesystem.
func (l *FileSystemLoader) Load(ctx context.Context, path string) ([]byte, error) {
	if path == "" {
		return nil, fmt.Errorf("path cannot be empty")
	}

	fullPath := path
	if l.basePath != "" {
		fullPath = filepath.Join(l.basePath, path)
	}

	data, err := os.ReadFile(fullPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file %s: %w", fullPath, err)
	}

	return data, nil
}
