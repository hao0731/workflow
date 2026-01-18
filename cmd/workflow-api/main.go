package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"

	"github.com/cheriehsieh/orchestration/internal/config"
	"github.com/cheriehsieh/orchestration/internal/dsl"
	"github.com/cheriehsieh/orchestration/internal/dsl/api"
)

func main() {
	// 1. Load configuration
	cfg := config.Load()

	// 2. Setup logger
	logger := config.SetupLogger(cfg.Env)
	logger.Info("starting workflow API server",
		slog.String("env", cfg.Env),
	)

	// 3. Initialize DSL registry
	registry := dsl.NewWorkflowRegistry()

	// 4. Optionally load workflows from directory
	workflowDir := os.Getenv("WORKFLOW_DIR")
	if workflowDir != "" {
		if err := registry.LoadDirectory(context.Background(), workflowDir); err != nil {
			logger.Warn("failed to load workflows from directory",
				slog.String("dir", workflowDir),
				slog.Any("error", err),
			)
		} else {
			logger.Info("loaded workflows from directory",
				slog.String("dir", workflowDir),
				slog.Int("count", len(registry.ListWorkflows())),
			)
		}
	}

	// 5. Setup Echo server
	e := echo.New()
	e.HideBanner = true

	// Middleware
	e.Use(middleware.RequestLogger())
	e.Use(middleware.Recover())
	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete},
		AllowHeaders: []string{echo.HeaderContentType, echo.HeaderAuthorization},
	}))

	// 6. Register routes
	handler := api.NewWorkflowHandler(registry, logger)
	handler.RegisterRoutes(e.Group("/api"))

	// Health check
	e.GET("/health", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})

	// 7. Start server in goroutine
	go func() {
		port := os.Getenv("WORKFLOW_API_PORT")
		if port == "" {
			port = "8083"
		}
		logger.Info("workflow API server listening", slog.String("port", port))
		if err := e.Start(":" + port); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", slog.Any("error", err))
			os.Exit(1)
		}
	}()

	// 8. Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	logger.Info("shutting down workflow API server...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := e.Shutdown(ctx); err != nil {
		logger.Error("shutdown error", slog.Any("error", err))
	}
}
