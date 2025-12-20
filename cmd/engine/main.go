package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"

	cloudevents "github.com/cloudevents/sdk-go/v2"

	"github.com/cheriehsieh/orchestration/internal/config"
	"github.com/cheriehsieh/orchestration/internal/engine"
	"github.com/cheriehsieh/orchestration/internal/eventbus"
	"github.com/cheriehsieh/orchestration/internal/eventstore"
	"github.com/cheriehsieh/orchestration/internal/scheduler"
	"github.com/cheriehsieh/orchestration/internal/worker"
)

func main() {
	// 1. Load configuration
	cfg := config.Load()

	// 2. Setup logger
	logger := config.SetupLogger(cfg.Env)
	logger.Info("starting workflow engine",
		slog.String("env", cfg.Env),
		slog.Bool("debug", cfg.Debug),
	)

	// 3. Connect to MongoDB
	client, err := mongo.Connect(context.Background(), options.Client().ApplyURI(cfg.MongoURI))
	if err != nil {
		logger.Error("failed to connect to MongoDB", slog.Any("error", err))
		os.Exit(1)
	}
	defer func() {
		if err := client.Disconnect(context.Background()); err != nil {
			logger.Error("failed to disconnect MongoDB", slog.Any("error", err))
		}
	}()
	db := client.Database(cfg.MongoDatabase)

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

	// Create streams
	streams := []nats.StreamConfig{
		{Name: "WORKFLOW_EVENTS", Subjects: []string{"workflow.events.>"}},
		{Name: "WORKFLOW_NODES", Subjects: []string{"workflow.nodes.>"}},
	}
	for _, cfg := range streams {
		if _, err := js.AddStream(&cfg); err != nil {
			logger.Debug("stream may already exist", slog.String("stream", cfg.Name), slog.Any("error", err))
		}
	}

	// 5. Define a conditional workflow with Join
	workflow := &engine.Workflow{
		ID: "join-workflow-1",
		Nodes: []engine.Node{
			{ID: "start", Type: engine.StartNode, Name: "Start"},
			{ID: "parallel-1", Type: engine.ActionNode, Name: "Parallel 1", Parameters: map[string]any{"msg": "p1"}},
			{ID: "parallel-2", Type: engine.ActionNode, Name: "Parallel 2", Parameters: map[string]any{"msg": "p2"}},
			{ID: "join", Type: engine.JoinNode, Name: "Join"},
			{ID: "end", Type: engine.ActionNode, Name: "End"},
		},
		Connections: []engine.Connection{
			{FromNode: "start", ToNode: "parallel-1"},
			{FromNode: "start", ToNode: "parallel-2"},
			{FromNode: "parallel-1", ToNode: "join", ToPort: "p1_out"},
			{FromNode: "parallel-2", ToNode: "join", ToPort: "p2_out"},
			{FromNode: "join", ToNode: "end"},
		},
	}

	workflowRepo := &InMemoryWorkflowRepo{workflows: map[string]*engine.Workflow{workflow.ID: workflow}}
	eventStoreImpl := eventstore.NewMongoEventStore(db, "events")

	// 6. Initialize Orchestrator with JoinStateManager
	joinStore := engine.NewInMemoryJoinStateStore()
	joinManager := engine.NewJoinStateManager(joinStore)

	orchestratorPub := eventbus.NewNATSEventBus(js, "workflow.events.scheduler", "orchestrator-pub", eventbus.WithLogger(logger))
	orchestratorSub := eventbus.NewNATSEventBus(js, "workflow.events.execution", "orchestrator", eventbus.WithLogger(logger))

	orchestrator := engine.NewOrchestrator(
		eventStoreImpl, orchestratorPub, orchestratorSub, workflowRepo,
		engine.WithOrchestratorLogger(logger),
		engine.WithJoinStateManager(joinManager), // Inject manager
	)

	// 7. Initialize Scheduler
	schedulerSub := eventbus.NewNATSEventBus(js, "workflow.events.scheduler", "scheduler", eventbus.WithLogger(logger))
	schedulerPub := eventbus.NewNATSEventBus(js, "workflow.events.execution", "scheduler-pub", eventbus.WithLogger(logger))
	resultSub := eventbus.NewNATSEventBus(js, "workflow.events.results", "scheduler-results", eventbus.WithLogger(logger))
	dispatcher := scheduler.NewNATSDispatcher(js, "workflow.nodes")

	sched := scheduler.NewScheduler(
		eventStoreImpl, schedulerPub, schedulerSub, resultSub, dispatcher, workflowRepo,
		scheduler.WithSchedulerLogger(logger),
	)

	// 8. Initialize Workers
	resultPub := eventbus.NewNATSEventBus(js, "workflow.events.results", "worker-results", eventbus.WithLogger(logger))

	startWorker := worker.NewWorker(
		engine.StartNode,
		func(ctx context.Context, input, params map[string]any) (engine.NodeResult, error) {
			logger.InfoContext(ctx, "executing START node")
			return engine.NodeResult{Output: input, Port: engine.DefaultPort}, nil
		},
		eventbus.NewNATSEventBus(js, "workflow.nodes.StartNode", "worker-start", eventbus.WithLogger(logger)),
		resultPub,
		worker.WithWorkerLogger(logger),
	)

	actionWorker := worker.NewWorker(
		engine.ActionNode,
		func(ctx context.Context, input, params map[string]any) (engine.NodeResult, error) {
			logger.InfoContext(ctx, "executing ACTION node", slog.Any("params", params))
			return engine.NodeResult{Output: map[string]any{"processed": true}, Port: engine.DefaultPort}, nil
		},
		eventbus.NewNATSEventBus(js, "workflow.nodes.ActionNode", "worker-action", eventbus.WithLogger(logger)),
		resultPub,
		worker.WithWorkerLogger(logger),
	)

	joinWorker := worker.NewWorker(
		engine.JoinNode,
		func(ctx context.Context, input, params map[string]any) (engine.NodeResult, error) {
			logger.InfoContext(ctx, "executing JOIN node", slog.Any("input", input))
			return engine.NodeResult{Output: input, Port: engine.DefaultPort}, nil
		},
		eventbus.NewNATSEventBus(js, "workflow.nodes.JoinNode", "worker-join", eventbus.WithLogger(logger)),
		resultPub,
		worker.WithWorkerLogger(logger),
	)

	// 9. Start all components
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	components := []interface{ Start(context.Context) error }{
		orchestrator, sched, startWorker, actionWorker, joinWorker,
	}

	for _, c := range components {
		component := c
		go func() {
			if err := component.Start(ctx); err != nil {
				logger.Error("component error", slog.Any("error", err))
			}
		}()
	}

	// 10. Trigger workflow
	time.Sleep(1 * time.Second)
	executionID := fmt.Sprintf("exec-%d", time.Now().Unix())
	logger.Info("triggering execution", slog.String("execution_id", executionID))

	startEvent := cloudevents.NewEvent()
	startEvent.SetID(uuid.New().String())
	startEvent.SetSource(engine.EventSource)
	startEvent.SetType(engine.ExecutionStarted)
	startEvent.SetSubject(executionID)
	_ = startEvent.SetData(cloudevents.ApplicationJSON, engine.ExecutionStartedData{
		WorkflowID: workflow.ID,
		InputData:  map[string]any{"init": "value"},
	})

	if err := eventStoreImpl.Append(ctx, startEvent); err != nil {
		logger.Error("failed to store start event", slog.Any("error", err))
		os.Exit(1)
	}
	if err := orchestratorSub.Publish(ctx, startEvent); err != nil {
		logger.Error("failed to publish start event", slog.Any("error", err))
		os.Exit(1)
	}

	// 11. Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan
	logger.Info("shutting down...")
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
