package bootstrap

import (
	"log/slog"

	"github.com/cheriehsieh/orchestration/internal/config"
)

// NewLogger returns the standard environment-aware service logger.
func NewLogger(cfg *config.Config) *slog.Logger {
	return config.SetupLogger(cfg.Env)
}
