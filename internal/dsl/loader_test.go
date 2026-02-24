package dsl

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Test: Load returns content from a valid file (Happy Path)
func TestFileSystemLoader_Load_ReturnsFileContent(t *testing.T) {
	// Setup: Create a temp file with known content
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "workflow.yaml")
	expectedContent := []byte("id: test-workflow\nname: Test Workflow")
	err := os.WriteFile(testFile, expectedContent, 0644)
	require.NoError(t, err)

	// Act
	loader := NewFileSystemLoader("")
	data, err := loader.Load(context.Background(), testFile)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, expectedContent, data)
}

// Test: Load returns error for non-existent file (Zero/Exception case)
func TestFileSystemLoader_Load_ReturnsErrorForMissingFile(t *testing.T) {
	loader := NewFileSystemLoader("")
	_, err := loader.Load(context.Background(), "/nonexistent/path/workflow.yaml")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read file")
}

// Test: Load with basePath prepends path correctly
func TestFileSystemLoader_Load_UsesBasePath(t *testing.T) {
	// Setup: Create a temp directory structure
	tempDir := t.TempDir()
	subDir := filepath.Join(tempDir, "workflows")
	require.NoError(t, os.MkdirAll(subDir, 0755))

	testFile := filepath.Join(subDir, "my-workflow.yaml")
	expectedContent := []byte("id: my-workflow")
	require.NoError(t, os.WriteFile(testFile, expectedContent, 0644))

	// Act: Load with basePath set to tempDir/workflows
	loader := NewFileSystemLoader(subDir)
	data, err := loader.Load(context.Background(), "my-workflow.yaml")

	// Assert
	require.NoError(t, err)
	assert.Equal(t, expectedContent, data)
}

// Test: Load returns error for empty path (Boundary case)
func TestFileSystemLoader_Load_ReturnsErrorForEmptyPath(t *testing.T) {
	loader := NewFileSystemLoader("")
	_, err := loader.Load(context.Background(), "")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "path cannot be empty")
}
