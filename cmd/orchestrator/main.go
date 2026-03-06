package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cheriehsieh/orchestration/internal/bootstrap"
	"github.com/cheriehsieh/orchestration/internal/config"
	"github.com/cheriehsieh/orchestration/internal/dsl"
	"github.com/cheriehsieh/orchestration/internal/engine"
	"github.com/cheriehsieh/orchestration/internal/eventbus"
	"github.com/cheriehsieh/orchestration/internal/eventstore"
	"github.com/cheriehsieh/orchestration/internal/messaging"
)

func main() {
	cfg := config.Load()
	logger := bootstrap.NewLogger(cfg)
	logger.Info("starting workflow orchestrator",
		slog.String("env", cfg.Env),
	)

	mongoCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	mongoClient, db, err := bootstrap.ConnectMongo(mongoCtx, cfg, bootstrap.DefaultMongoConnect)
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

	cassandraSession, err := bootstrap.OpenCassandraSession(cfg, logger)
	if err != nil {
		logger.Error("failed to connect to Cassandra", slog.Any("error", err))
		os.Exit(1)
	}
	defer cassandraSession.Close()

	nc, js, err := bootstrap.ConnectNATS(cfg, bootstrap.DefaultNATSConnect, bootstrap.DefaultNATSBootstrap, true)
	if err != nil {
		logger.Error("failed to connect to NATS", slog.Any("error", err))
		os.Exit(1)
	}
	defer nc.Close()

	workflowRepo := dsl.NewMongoWorkflowStore(db)
	eventStore := eventstore.NewCassandraEventStore(cassandraSession)
	joinStore := engine.NewMongoJoinStateStore(db, "workflow_join_states")

	orchestrator := engine.NewOrchestrator(
		eventStore,
		eventbus.NewNATSEventBus(js, "", "orchestrator-publisher", eventbus.WithLogger(logger)),
		nil,
		workflowRepo,
		engine.WithOrchestratorLogger(logger),
		engine.WithJoinStateManager(engine.NewJoinStateManager(joinStore)),
		engine.WithOrchestratorSubscribers(
			eventbus.NewNATSEventBus(js, messaging.SubjectRuntimeExecutionStartedV1, "orchestrator-started", eventbus.WithLogger(logger)),
			eventbus.NewNATSEventBus(js, messaging.SubjectRuntimeNodeExecutedV1, "orchestrator-node-executed", eventbus.WithLogger(logger)),
		),
	)

	serviceCtx, serviceCancel := context.WithCancel(context.Background())
	defer serviceCancel()

	go func() {
		if err := orchestrator.Start(serviceCtx); err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("orchestrator stopped with error", slog.Any("error", err))
			os.Exit(1)
		}
	}()

	logger.Info("workflow orchestrator running",
		slog.Any("subjects", engine.OrchestratorRuntimeSubjects()),
	)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	logger.Info("shutting down workflow orchestrator")
	serviceCancel()
}
