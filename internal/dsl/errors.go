package dsl

import "errors"

// Common errors returned by the DSL package.
var (
	ErrWorkflowNotFound = errors.New("workflow not found")
	ErrEmptyInput       = errors.New("empty input")
	ErrEmptyPath        = errors.New("path cannot be empty")
)

// ValidationError represents a validation failure.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return e.Message
}
