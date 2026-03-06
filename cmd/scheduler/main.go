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
	"github.com/cheriehsieh/orchestration/internal/eventbus"
	"github.com/cheriehsieh/orchestration/internal/eventstore"
	"github.com/cheriehsieh/orchestration/internal/messaging"
	"github.com/cheriehsieh/orchestration/internal/registry"
	"github.com/cheriehsieh/orchestration/internal/scheduler"
)

func main() {
	cfg := config.Load()
	logger := bootstrap.NewLogger(cfg)
	logger.Info("starting workflow scheduler",
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

	jwtSecret := os.Getenv("JWT_SECRET")
	if jwtSecret == "" {
		jwtSecret = "default-secret-change-in-production"
	}

	workflowRepo := dsl.NewMongoWorkflowStore(db)
	eventStore := eventstore.NewCassandraEventStore(cassandraSession)
	nodeRegistry := registry.NewService(
		registry.NewMongoRepository(db),
		registry.DefaultJWTConfig(jwtSecret),
		registry.WithServiceLogger(logger),
	)

	sched := scheduler.NewScheduler(
		eventStore,
		eventbus.NewNATSEventBus(js, "", "scheduler-publisher", eventbus.WithLogger(logger)),
		eventbus.NewNATSEventBus(js, messaging.SubjectRuntimeNodeScheduledV1, "scheduler-scheduled", eventbus.WithLogger(logger)),
		eventbus.NewNATSEventBus(js, messaging.SubjectEventNodeExecutedV1, "scheduler-results", eventbus.WithLogger(logger)),
		scheduler.NewNATSDispatcher(js, "workflow.command.node.execute"),
		workflowRepo,
		scheduler.WithSchedulerLogger(logger),
		scheduler.WithNodeValidator(nodeRegistry),
		scheduler.WithValidationDLQPublisher(
			eventbus.NewNATSEventBus(js, messaging.SubjectDLQSchedulerValidationV1, "scheduler-dlq", eventbus.WithLogger(logger)),
		),
	)

	serviceCtx, serviceCancel := context.WithCancel(context.Background())
	defer serviceCancel()

	go func() {
		if err := sched.Start(serviceCtx); err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("scheduler stopped with error", slog.Any("error", err))
			os.Exit(1)
		}
	}()

	logger.Info("workflow scheduler running",
		slog.Any("subjects", scheduler.SchedulerSubscriptionSubjects()),
	)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	logger.Info("shutting down workflow scheduler")
	serviceCancel()
}
