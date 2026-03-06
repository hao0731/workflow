package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"

	"github.com/cheriehsieh/orchestration/internal/api"
	"github.com/cheriehsieh/orchestration/internal/bootstrap"
	"github.com/cheriehsieh/orchestration/internal/config"
	"github.com/cheriehsieh/orchestration/internal/dsl"
	dslapi "github.com/cheriehsieh/orchestration/internal/dsl/api"
	"github.com/cheriehsieh/orchestration/internal/eventbus"
	"github.com/cheriehsieh/orchestration/internal/eventstore"
	"github.com/cheriehsieh/orchestration/internal/marketplace"
	"github.com/cheriehsieh/orchestration/internal/messaging"
	"github.com/cheriehsieh/orchestration/internal/schema"
)

func main() {
	// 1. Load configuration
	cfg := config.Load()

	// 2. Setup logger
	logger := bootstrap.NewLogger(cfg)
	logger.Info("starting workflow API server",
		slog.String("env", cfg.Env),
	)

	// 3. Connect to MongoDB
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	mongoClient, db, err := bootstrap.ConnectMongo(ctx, cfg, bootstrap.DefaultMongoConnect)
	cancel()
	if err != nil {
		logger.Error("failed to connect to MongoDB", slog.Any("error", err))
		os.Exit(1)
	}
	defer func() {
		if disconnectErr := mongoClient.Disconnect(context.Background()); disconnectErr != nil {
			logger.Error("failed to disconnect MongoDB", slog.Any("error", disconnectErr))
		}
	}()

	// 3b. Connect to Cassandra
	cassandraSession, err := bootstrap.OpenCassandraSession(cfg, logger)
	if err != nil {
		logger.Error("failed to connect to Cassandra", slog.Any("error", err))
		os.Exit(1)
	}
	defer cassandraSession.Close()

	eventStore := eventstore.NewCassandraEventStore(cassandraSession)

	var schemaCache *schema.Cache
	if cfg.SchemaRegistryURL != "" {
		schemaCache, err = schema.NewCachedClient(cfg.SchemaRegistryURL, cfg.SchemaCacheTTL)
		if err != nil {
			logger.Error("failed to configure schema registry client", slog.Any("error", err))
			os.Exit(1)
		}
		logger.Info("schema registry client configured",
			slog.String("url", cfg.SchemaRegistryURL),
			slog.Duration("cache_ttl", cfg.SchemaCacheTTL),
		)
	} else {
		logger.Info("schema registry client disabled")
	}
	_ = schemaCache

	// 4. Initialize workflow store based on config
	store, err := dsl.NewWorkflowStoreFromConfig(cfg.WorkflowStore, db)
	if err != nil {
		logger.Error("failed to create workflow store", slog.Any("error", err))
		os.Exit(1)
	}
	registry := dsl.NewWorkflowRegistry(dsl.WithStore(store))
	logger.Info("workflow store initialized", slog.String("type", cfg.WorkflowStore))

	// 5. Connect to NATS
	nc, js, err := bootstrap.ConnectNATS(cfg, bootstrap.DefaultNATSConnect, bootstrap.DefaultNATSBootstrap, true)
	if err != nil {
		logger.Error("failed to connect to NATS", slog.Any("error", err))
		os.Exit(1)
	}
	defer nc.Close()

	// Create EventBus instances for the v2 execute flow.
	executionEventBus := eventbus.NewNATSEventBus(js, messaging.SubjectRuntimeExecutionStartedV1, "workflow-api-runtime", eventbus.WithLogger(logger))
	commandEventBus := eventbus.NewNATSEventBus(js, messaging.SubjectCommandExecuteV1, "workflow-api-command", eventbus.WithLogger(logger))
	logger.Info("connected to NATS", slog.String("url", cfg.NATSURL))

	// 6. Optionally load workflows from directory
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

	// Event Marketplace registry (shared between workflow and marketplace handlers)
	eventRegistry := marketplace.NewMongoEventRegistry(db)

	// Workflow routes use the registry's metadata-aware persistence when supported.
	workflowHandler := dslapi.NewWorkflowHandler(registry, logger,
		dslapi.WithEventBus(executionEventBus),
		dslapi.WithEventStore(eventStore),
		dslapi.WithEventRegistry(eventRegistry),
	)
	workflowHandler.RegisterRoutes(apiGroup)
	commandConsumer := api.NewCommandConsumer(
		commandEventBus,
		workflowHandler,
		api.WithCommandConsumerLogger(logger),
	)

	// Execution routes
	execStore := eventstore.NewMongoExecutionStore(db, "executions")
	executionHandler := api.NewExecutionHandler(eventStore, api.WithExecutionStore(execStore))
	executionHandler.RegisterRoutes(apiGroup)

	// Stream routes
	streamHandler := api.NewStreamHandler(eventStore, logger)
	streamHandler.RegisterRoutes(apiGroup)

	// Marketplace routes
	marketplaceHandler := api.NewMarketplaceHandler(eventRegistry, logger)
	marketplaceHandler.RegisterRoutes(apiGroup)

	// Health check
	e.GET("/health", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})

	serviceCtx, serviceCancel := context.WithCancel(context.Background())
	defer serviceCancel()

	go func() {
		if err := commandConsumer.Start(serviceCtx); err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("command consumer error", slog.Any("error", err))
			os.Exit(1)
		}
	}()

	// 8. Start server in goroutine
	go func() {
		port := os.Getenv("WORKFLOW_API_PORT")
		if port == "" {
			port = "8083"
		}
		logger.Info("workflow API server listening", slog.String("port", port))
		if err := e.Start(":" + port); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server error", slog.Any("error", err))
			os.Exit(1)
		}
	}()

	// 9. Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	logger.Info("shutting down workflow API server...")
	serviceCancel()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := e.Shutdown(shutdownCtx); err != nil {
		logger.Error("shutdown error", slog.Any("error", err))
	}
}
