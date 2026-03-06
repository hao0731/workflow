package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gocql/gocql"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"go.mongodb.org/mongo-driver/mongo"

	cloudevents "github.com/cloudevents/sdk-go/v2"

	"github.com/cheriehsieh/orchestration/internal/config"
	"github.com/cheriehsieh/orchestration/internal/dsl"
	"github.com/cheriehsieh/orchestration/internal/engine"
	"github.com/cheriehsieh/orchestration/internal/eventbus"
	"github.com/cheriehsieh/orchestration/internal/eventstore"
	"github.com/cheriehsieh/orchestration/internal/marketplace"
	"github.com/cheriehsieh/orchestration/internal/messaging"
	"github.com/cheriehsieh/orchestration/internal/scheduler"
	"github.com/cheriehsieh/orchestration/internal/worker"
)

func main() {
	// 1. Load configuration
	cfg := config.Load()

	// 2. Setup logger
	logger := config.SetupLogger(cfg.Env)
	logger.Info("starting workflow engine with Event Marketplace",
		slog.String("env", cfg.Env),
	)

	// 3. Connect to MongoDB
	mongoOpts, err := cfg.MongoClientOptions()
	if err != nil {
		logger.Error("invalid MongoDB configuration", slog.Any("error", err))
		os.Exit(1)
	}

	mongoCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	client, err := mongo.Connect(mongoCtx, mongoOpts)
	if err != nil {
		logger.Error("failed to connect to MongoDB", slog.Any("error", err))
		os.Exit(1)
	}
	defer func() {
		_ = client.Disconnect(context.Background())
	}()
	db := client.Database(cfg.MongoDatabase)

	// 3b. Connect to Cassandra
	cassandraCluster := gocql.NewCluster(strings.Split(cfg.CassandraHosts, ",")...)
	cassandraCluster.Port = cfg.CassandraPort
	cassandraCluster.Keyspace = cfg.CassandraKeyspace
	cassandraCluster.Consistency = gocql.LocalQuorum

	var cassandraSession *gocql.Session
	for attempt := 1; attempt <= 5; attempt++ {
		cassandraSession, err = cassandraCluster.CreateSession()
		if err == nil {
			break
		}
		logger.Warn("failed to connect to Cassandra, retrying...",
			slog.Int("attempt", attempt),
			slog.Any("error", err),
		)
		time.Sleep(time.Duration(attempt) * 2 * time.Second)
	}
	if err != nil {
		logger.Error("failed to connect to Cassandra after retries", slog.Any("error", err))
		os.Exit(1)
	}
	defer cassandraSession.Close()

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

	if err := eventbus.BootstrapWorkflowStreams(
		js,
		true,
		nats.StreamConfig{Name: "MARKETPLACE", Subjects: []string{"marketplace.>"}},
	); err != nil {
		logger.Error("failed to bootstrap workflow streams", slog.Any("error", err))
		os.Exit(1)
	}

	// 5. Initialize Event Marketplace Registry
	eventRegistry := marketplace.NewMongoEventRegistry(db)

	// Register a public event
	_ = eventRegistry.Register(context.Background(), &marketplace.EventDefinition{
		Name:        "order_created",
		Domain:      "ecommerce",
		Description: "Fired when a new order is placed",
		Schema:      map[string]any{"order_id": "string", "total": "number"},
	})

	// 6. Define Workflows
	// Publisher Workflow: Creates an order and publishes to marketplace
	// publisherWorkflow := &engine.Workflow{
	// 	ID: "order-service",
	// 	Nodes: []engine.Node{
	// 		{ID: "start", Type: engine.StartNode, Name: "Start"},
	// 		{ID: "create-order", Type: engine.ActionNode, Name: "Create Order"},
	// 		{
	// 			ID:   "publish-event",
	// 			Type: engine.PublishEvent,
	// 			Name: "Publish Order Created",
	// 			Parameters: map[string]any{
	// 				"event_name": "order_created",
	// 				"domain":     "ecommerce",
	// 				"payload": map[string]any{
	// 					"order_id": "{{.input.order_id}}",
	// 					"total":    "{{.input.total}}",
	// 				},
	// 			},
	// 		},
	// 	},
	// 	Connections: []engine.Connection{
	// 		{FromNode: "start", ToNode: "create-order"},
	// 		{FromNode: "create-order", ToNode: "publish-event"},
	// 	},
	// }

	// Subscriber Workflow: Triggered by order_created event
	// subscriberWorkflow := &engine.Workflow{
	// 	ID: "shipping-service",
	// 	Nodes: []engine.Node{
	// 		{
	// 			ID:   "start",
	// 			Type: engine.StartNode,
	// 			Name: "Event Trigger",
	// 			Trigger: &engine.Trigger{
	// 				Type: engine.TriggerEvent,
	// 				Criteria: map[string]any{
	// 					"event_name": "order_created",
	// 					"domain":     "ecommerce",
	// 				},
	// 			},
	// 		},
	// 		{ID: "prepare-shipment", Type: engine.ActionNode, Name: "Prepare Shipment"},
	// 		{ID: "notify-customer", Type: engine.ActionNode, Name: "Notify Customer"},
	// 	},
	// 	Connections: []engine.Connection{
	// 		{FromNode: "start", ToNode: "prepare-shipment"},
	// 		{FromNode: "prepare-shipment", ToNode: "notify-customer"},
	// 	},
	// }

	// workflowRepo := &InMemoryWorkflowRepo{
	// 	workflows: map[string]*engine.Workflow{
	// 		publisherWorkflow.ID:  publisherWorkflow,
	// 		subscriberWorkflow.ID: subscriberWorkflow,
	// 	},
	// }

	workflowRepo := dsl.NewMongoWorkflowStore(db)

	// Cassandra Event Store: sole source for event persistence
	eventStoreImpl := eventstore.NewCassandraEventStore(cassandraSession)

	// 6b. Initialize Execution Store for linking
	executionStore := eventstore.NewMongoExecutionStore(db, "executions")
	if err := executionStore.EnsureIndexes(context.Background()); err != nil {
		logger.Warn("failed to ensure execution indexes", slog.Any("error", err))
	}

	// 7. Initialize Orchestrator
	joinStore := engine.NewInMemoryJoinStateStore()
	joinManager := engine.NewJoinStateManager(joinStore)

	workflowPub := eventbus.NewNATSEventBus(js, "", "workflow-pub", eventbus.WithLogger(logger))
	orchestratorSub := eventbus.NewNATSEventBus(js, messaging.PlaneWildcardSubject(messaging.PlaneRuntime), "orchestrator", eventbus.WithLogger(logger))

	orchestrator := engine.NewOrchestrator(
		eventStoreImpl, workflowPub, orchestratorSub, workflowRepo,
		engine.WithOrchestratorLogger(logger),
		engine.WithJoinStateManager(joinManager),
	)

	// 8. Initialize Scheduler
	schedulerSub := eventbus.NewNATSEventBus(js, messaging.SubjectRuntimeNodeScheduledV1, "scheduler", eventbus.WithLogger(logger))
	resultSub := eventbus.NewNATSEventBus(js, messaging.SubjectEventNodeExecutedV1, "scheduler-results", eventbus.WithLogger(logger))
	dispatcher := scheduler.NewNATSDispatcher(js, "workflow.nodes")

	sched := scheduler.NewScheduler(
		eventStoreImpl, workflowPub, schedulerSub, resultSub, dispatcher, workflowRepo,
		scheduler.WithSchedulerLogger(logger),
	)

	// 9. Initialize Event Router (Marketplace)
	eventRouter := scheduler.NewEventRouter(
		js, workflowRepo, eventStoreImpl, workflowPub,
		scheduler.WithEventRouterLogger(logger),
	)

	// 9b. Initialize Execution Link Handler
	linkHandlerSub := eventbus.NewNATSEventBus(js, messaging.PlaneWildcardSubject(messaging.PlaneRuntime), "link-handler", eventbus.WithLogger(logger))
	linkHandler := scheduler.NewExecutionLinkHandler(
		executionStore,
		scheduler.WithLinkHandlerLogger(logger),
		scheduler.WithLinkHandlerSubscriber(linkHandlerSub),
	)

	// 10. Initialize Workers
	// PublishEvent executor
	publishExecutor := engine.NewPublishEventExecutor(js, eventRegistry, engine.WithPublishEventLogger(logger))

	startWorker := worker.NewWorker(
		engine.StartNode,
		func(ctx context.Context, input, params map[string]any) (engine.NodeResult, error) {
			logger.InfoContext(ctx, "▶ START node executed")
			return engine.NodeResult{Output: input, Port: engine.DefaultPort}, nil
		},
		eventbus.NewNATSEventBus(js, messaging.CommandNodeExecuteSubject(string(engine.StartNode), "v1"), "worker-start", eventbus.WithLogger(logger)),
		workflowPub,
		worker.WithWorkerLogger(logger),
	)

	actionWorker := worker.NewWorker(
		engine.ActionNode,
		func(ctx context.Context, input, params map[string]any) (engine.NodeResult, error) {
			logger.InfoContext(ctx, "⚙ ACTION node executed", slog.Any("input", input))
			return engine.NodeResult{Output: input, Port: engine.DefaultPort}, nil
		},
		eventbus.NewNATSEventBus(js, messaging.CommandNodeExecuteSubject(string(engine.ActionNode), "v1"), "worker-action", eventbus.WithLogger(logger)),
		workflowPub,
		worker.WithWorkerLogger(logger),
	)

	publishWorker := worker.NewWorker(
		engine.PublishEvent,
		func(ctx context.Context, input, params map[string]any) (engine.NodeResult, error) {
			// Use execution ID from dispatch context (injected by worker)
			execID, _ := input["_execution_id"].(string)
			if execID == "" {
				execID = uuid.New().String()
			}
			return publishExecutor.Execute(ctx, execID, input, params)
		},
		eventbus.NewNATSEventBus(js, messaging.CommandNodeExecuteSubject(string(engine.PublishEvent), "v1"), "worker-publish", eventbus.WithLogger(logger)),
		workflowPub,
		worker.WithWorkerLogger(logger),
	)

	// 11. Start all components
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	components := []interface{ Start(context.Context) error }{
		orchestrator, sched, eventRouter, linkHandler, startWorker, actionWorker, publishWorker,
	}

	for _, c := range components {
		component := c
		go func() {
			if err := component.Start(ctx); err != nil {
				logger.Error("component error", slog.Any("error", err))
			}
		}()
	}

	startLegacyBridge(
		ctx,
		logger,
		workflowPub,
		eventbus.NewNATSEventBus(js, "workflow.events.execution", "compat-legacy-execution", eventbus.WithLogger(logger)),
	)
	startLegacyBridge(
		ctx,
		logger,
		workflowPub,
		eventbus.NewNATSEventBus(js, "workflow.events.scheduler", "compat-legacy-scheduler", eventbus.WithLogger(logger)),
	)
	startLegacyBridge(
		ctx,
		logger,
		workflowPub,
		eventbus.NewNATSEventBus(js, "workflow.events.results", "compat-legacy-results", eventbus.WithLogger(logger)),
	)
	startLegacyBridge(
		ctx,
		logger,
		workflowPub,
		eventbus.NewNATSEventBus(js, "workflow.nodes.>", "compat-legacy-dispatch", eventbus.WithLogger(logger)),
	)

	logger.Info("🚀 Engine started - workflows can now be triggered via API",
		slog.String("endpoint", "POST /api/workflows/:id/execute"),
	)

	// 12. Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan
	logger.Info("shutting down...")
}

func startLegacyBridge(
	ctx context.Context,
	logger *slog.Logger,
	publisher eventbus.Publisher,
	subscriber eventbus.Subscriber,
) {
	go func() {
		err := subscriber.Subscribe(ctx, func(ctx context.Context, event cloudevents.Event) error {
			translated, _, ok, err := messaging.TranslateLegacyEvent(event)
			if err != nil {
				return err
			}
			if !ok {
				return nil
			}

			logger.InfoContext(ctx, "translated legacy workflow event",
				slog.String("legacy_type", event.Type()),
				slog.String("v2_type", translated.Type()),
			)

			return publisher.Publish(ctx, translated)
		})
		if err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("legacy compatibility bridge error", slog.Any("error", err))
		}
	}()
}

// InMemoryWorkflowRepo is a simple in-memory workflow repository.
type InMemoryWorkflowRepo struct {
	workflows map[string]*engine.Workflow
}

func (r *InMemoryWorkflowRepo) GetByID(_ context.Context, id string) (*engine.Workflow, error) {
	if w, ok := r.workflows[id]; ok {
		return w, nil
	}
	return nil, fmt.Errorf("workflow %s not found", id)
}

// FindByEventTrigger finds workflows that match the given event trigger.
func (r *InMemoryWorkflowRepo) FindByEventTrigger(_ context.Context, eventName, domain string) ([]*engine.Workflow, error) { //nolint:unparam // implements WorkflowRepository interface
	var matches []*engine.Workflow
	for _, wf := range r.workflows {
		start := wf.GetStartNode()
		if start == nil || start.Trigger == nil {
			continue
		}
		if start.Trigger.Type != engine.TriggerEvent {
			continue
		}
		triggerEvent, _ := start.Trigger.Criteria["event_name"].(string)
		triggerDomain, _ := start.Trigger.Criteria["domain"].(string)

		if triggerEvent == eventName && triggerDomain == domain {
			matches = append(matches, wf)
		}
	}
	return matches, nil
}
