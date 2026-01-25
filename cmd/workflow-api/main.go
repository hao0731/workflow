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
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	"github.com/cheriehsieh/orchestration/internal/api"
	"github.com/cheriehsieh/orchestration/internal/config"
	"github.com/cheriehsieh/orchestration/internal/dsl"
	dslapi "github.com/cheriehsieh/orchestration/internal/dsl/api"
	"github.com/cheriehsieh/orchestration/internal/eventstore"
)

func main() {
	// 1. Load configuration
	cfg := config.Load()

	// 2. Setup logger
	logger := config.SetupLogger(cfg.Env)
	logger.Info("starting workflow API server",
		slog.String("env", cfg.Env),
	)

	// 3. Connect to MongoDB
	mongoURI := os.Getenv("MONGO_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27017"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	mongoClient, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	cancel()
	if err != nil {
		logger.Error("failed to connect to MongoDB", slog.Any("error", err))
		os.Exit(1)
	}
	defer mongoClient.Disconnect(context.Background())

	db := mongoClient.Database("orchestration")
	eventStore := eventstore.NewMongoEventStore(db, "events")

	// 4. Initialize workflow store based on config
	store, err := dsl.NewWorkflowStoreFromConfig(cfg.WorkflowStore, db)
	if err != nil {
		logger.Error("failed to create workflow store", slog.Any("error", err))
		os.Exit(1)
	}
	registry := dsl.NewWorkflowRegistry(dsl.WithStore(store))
	logger.Info("workflow store initialized", slog.String("type", cfg.WorkflowStore))

	// 5. Optionally load workflows from directory
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

	// 6. Setup Echo server
	e := echo.New()
	e.HideBanner = true

	// Middleware
	e.Use(middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		LogStatus: true,
		LogURI:    true,
		LogMethod: true,
		LogValuesFunc: func(c echo.Context, v middleware.RequestLoggerValues) error {
			logger.Info("request",
				slog.String("method", v.Method),
				slog.String("uri", v.URI),
				slog.Int("status", v.Status),
			)
			return nil
		},
	}))
	e.Use(middleware.Recover())
	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins: []string{"*"},
		AllowMethods: []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete},
		AllowHeaders: []string{echo.HeaderContentType, echo.HeaderAuthorization},
	}))

	// 7. Register routes
	apiGroup := e.Group("/api")

	// Workflow routes
	workflowHandler := dslapi.NewWorkflowHandler(registry, logger)
	workflowHandler.RegisterRoutes(apiGroup)

	// Execution routes
	executionHandler := api.NewExecutionHandler(eventStore)
	executionHandler.RegisterRoutes(apiGroup)

	// Stream routes
	streamHandler := api.NewStreamHandler(eventStore, logger)
	streamHandler.RegisterRoutes(apiGroup)

	// Health check
	e.GET("/health", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})

	// 8. Start server in goroutine
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

	// 9. Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	logger.Info("shutting down workflow API server...")
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := e.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown error", slog.Any("error", err))
	}
}
