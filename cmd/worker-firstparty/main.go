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
	"github.com/cheriehsieh/orchestration/internal/eventbus"
	"github.com/cheriehsieh/orchestration/internal/marketplace"
	"github.com/cheriehsieh/orchestration/internal/messaging"
	"github.com/cheriehsieh/orchestration/internal/worker"
)

func main() {
	cfg := config.Load()
	logger := bootstrap.NewLogger(cfg)
	logger.Info("starting first-party worker service",
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

	nc, js, err := bootstrap.ConnectNATS(cfg, bootstrap.DefaultNATSConnect, bootstrap.DefaultNATSBootstrap, true)
	if err != nil {
		logger.Error("failed to connect to NATS", slog.Any("error", err))
		os.Exit(1)
	}
	defer nc.Close()

	publisher := eventbus.NewNATSEventBus(js, messaging.SubjectEventNodeExecutedV1, "worker-firstparty-results", eventbus.WithLogger(logger))
	managedWorkers := worker.NewFirstPartyWorkers(js, publisher, marketplace.NewMongoEventRegistry(db), logger)

	serviceCtx, serviceCancel := context.WithCancel(context.Background())
	defer serviceCancel()

	for _, managedWorker := range managedWorkers {
		currentWorker := managedWorker
		go func() {
			if err := currentWorker.Service.Start(serviceCtx); err != nil && !errors.Is(err, context.Canceled) {
				logger.Error("first-party worker stopped with error",
					slog.String("node_type", string(currentWorker.NodeType)),
					slog.Any("error", err),
				)
				os.Exit(1)
			}
		}()
	}

	logger.Info("first-party workers running",
		slog.Any("subjects", worker.FirstPartyWorkerSubjects()),
	)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	logger.Info("shutting down first-party worker service")
	serviceCancel()
}
