package scheduler

import (
	"context"
)

// NodeValidator validates that a node type is registered and enabled.
type NodeValidator interface {
	Exists(ctx context.Context, fullType string) bool
}
