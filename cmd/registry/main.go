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
	"github.com/nats-io/nats.go"
	"go.mongodb.org/mongo-driver/mongo"

	"github.com/cheriehsieh/orchestration/internal/config"
	"github.com/cheriehsieh/orchestration/internal/registry"
)

func main() {
	// 1. Load configuration
	cfg := config.Load()

	// 2. Setup logger
	logger := config.SetupLogger(cfg.Env)
	logger.Info("starting node registry service",
		slog.String("env", cfg.Env),
		slog.String("port", cfg.Port),
	)

	// 3. Connect to MongoDB
	mongoOpts, err := cfg.MongoClientOptions()
	if err != nil {
		logger.Error("invalid MongoDB configuration", slog.Any("error", err))
		os.Exit(1)
	}

	mongoClient, err := mongo.Connect(context.Background(), mongoOpts)
	if err != nil {
		logger.Error("failed to connect to MongoDB", slog.Any("error", err))
		os.Exit(1)
	}
	defer func() {
		if err := mongoClient.Disconnect(context.Background()); err != nil {
			logger.Error("failed to disconnect MongoDB", slog.Any("error", err))
		}
	}()
	db := mongoClient.Database(cfg.MongoDatabase)

	// 4. Connect to NATS
	nc, err := nats.Connect(cfg.NATSURL)
	if err != nil {
		logger.Error("failed to connect to NATS", slog.Any("error", err))
		os.Exit(1)
	}
	defer nc.Close()

	js, err := nc.JetStream()
	if err != nil {
		logger.Error("failed to create JetStream context", slog.Any("error", err))
		os.Exit(1)
	}

	// Create streams if needed
	streams := []nats.StreamConfig{
		{Name: "WORKFLOW_NODES", Subjects: []string{"workflow.nodes.>"}},
		{Name: "WORKFLOW_RESULTS", Subjects: []string{"workflow.events.results"}},
	}
	for _, streamCfg := range streams {
		if _, err := js.AddStream(&streamCfg); err != nil {
			logger.Debug("stream may already exist", slog.String("stream", streamCfg.Name))
		}
	}

	// 5. Initialize Registry components
	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		jwtSecret = "default-secret-change-in-production"
	}

	repo := registry.NewMongoRepository(db)
	jwtConfig := registry.DefaultJWTConfig(jwtSecret)
	service := registry.NewService(repo, jwtConfig, registry.WithServiceLogger(logger))
	handler := registry.NewHandler(service)

	// 6. Initialize WebSocket Proxy
	proxy := registry.NewProxy(js, service, "workflow.events.results", registry.WithProxyLogger(logger))
	defer proxy.Close()

	// 7. Setup Echo server
	e := echo.New()
	e.HideBanner = true

	// Middleware
	e.Use(middleware.Recover())
	e.Use(middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{
		LogStatus:   true,
		LogURI:      true,
		LogError:    true,
		HandleError: true,
		LogValuesFunc: func(c echo.Context, v middleware.RequestLoggerValues) error {
			if v.Error != nil {
				logger.ErrorContext(c.Request().Context(), "request error",
					slog.String("uri", v.URI),
					slog.Int("status", v.Status),
					slog.Any("error", v.Error),
				)
			} else {
				logger.InfoContext(c.Request().Context(), "request",
					slog.String("uri", v.URI),
					slog.Int("status", v.Status),
				)
			}
			return nil
		},
	}))

	// Health check
	e.GET("/health", func(c echo.Context) error {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok"})
	})

	// Register API routes
	handler.RegisterRoutes(e)

	// Register WebSocket proxy endpoint
	e.GET("/ws/connect", proxy.HandleWebSocket)

	// 8. Start server with graceful shutdown
	go func() {
		if err := e.Start(":" + cfg.Port); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", slog.Any("error", err))
		}
	}()

	// 9. Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	logger.Info("shutting down...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := e.Shutdown(ctx); err != nil {
		logger.Error("shutdown error", slog.Any("error", err))
	}
}
