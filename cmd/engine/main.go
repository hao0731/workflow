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

	// 5. Define a conditional workflow
	conditionalWorkflow := &engine.Workflow{
		ID: "conditional-workflow-1",
		Nodes: []engine.Node{
			{ID: "start", Type: engine.StartNode, Name: "Start"},
			{ID: "check-value", Type: engine.IfNode, Name: "Check Value", Parameters: map[string]any{"threshold": 10}},
			{ID: "high-path", Type: engine.ActionNode, Name: "High Path", Parameters: map[string]any{"message": "Value is high"}},
			{ID: "low-path", Type: engine.ActionNode, Name: "Low Path", Parameters: map[string]any{"message": "Value is low"}},
		},
		Connections: []engine.Connection{
			{FromNode: "start", ToNode: "check-value"},
			{FromNode: "check-value", FromPort: engine.PortTrue, ToNode: "high-path"},
			{FromNode: "check-value", FromPort: engine.PortFalse, ToNode: "low-path"},
		},
	}

	workflowRepo := &InMemoryWorkflowRepo{workflows: map[string]*engine.Workflow{conditionalWorkflow.ID: conditionalWorkflow}}
	eventStoreImpl := eventstore.NewMongoEventStore(db, "events")

	// 6. Initialize Orchestrator (publishes to scheduler subject)
	orchestratorPub := eventbus.NewNATSEventBus(js, "workflow.events.scheduler", "orchestrator-pub", eventbus.WithLogger(logger))
	orchestratorSub := eventbus.NewNATSEventBus(js, "workflow.events.execution", "orchestrator", eventbus.WithLogger(logger))

	orchestrator := engine.NewOrchestrator(
		eventStoreImpl, orchestratorPub, orchestratorSub, workflowRepo,
		engine.WithOrchestratorLogger(logger),
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

	// 8. Initialize Workers (these could be in separate processes/containers)
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
			output := make(map[string]any)
			for k, v := range input {
				output[k] = v
			}
			for k, v := range params {
				output[k] = v
			}
			return engine.NodeResult{Output: output, Port: engine.DefaultPort}, nil
		},
		eventbus.NewNATSEventBus(js, "workflow.nodes.ActionNode", "worker-action", eventbus.WithLogger(logger)),
		resultPub,
		worker.WithWorkerLogger(logger),
	)

	ifWorker := worker.NewWorker(
		engine.IfNode,
		func(ctx context.Context, input, params map[string]any) (engine.NodeResult, error) {
			threshold := toFloat64(params["threshold"])
			value := toFloat64(input["value"])

			logger.InfoContext(ctx, "evaluating IF condition",
				slog.Float64("value", value),
				slog.Float64("threshold", threshold),
			)

			port := engine.PortFalse
			if value >= threshold {
				port = engine.PortTrue
			}
			return engine.NodeResult{Output: input, Port: port}, nil
		},
		eventbus.NewNATSEventBus(js, "workflow.nodes.IfNode", "worker-if", eventbus.WithLogger(logger)),
		resultPub,
		worker.WithWorkerLogger(logger),
	)

	// 9. Start all components
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		if err := orchestrator.Start(ctx); err != nil {
			logger.Error("orchestrator error", slog.Any("error", err))
		}
	}()

	go func() {
		if err := sched.Start(ctx); err != nil {
			logger.Error("scheduler error", slog.Any("error", err))
		}
	}()

	go func() {
		if err := startWorker.Start(ctx); err != nil {
			logger.Error("start worker error", slog.Any("error", err))
		}
	}()

	go func() {
		if err := actionWorker.Start(ctx); err != nil {
			logger.Error("action worker error", slog.Any("error", err))
		}
	}()

	go func() {
		if err := ifWorker.Start(ctx); err != nil {
			logger.Error("if worker error", slog.Any("error", err))
		}
	}()

	// 10. Trigger workflow execution
	time.Sleep(1 * time.Second)
	executionID := fmt.Sprintf("exec-%d", time.Now().Unix())
	logger.Info("triggering execution", slog.String("execution_id", executionID))

	startEvent := cloudevents.NewEvent()
	startEvent.SetID(uuid.New().String())
	startEvent.SetSource(engine.EventSource)
	startEvent.SetType(engine.ExecutionStarted)
	startEvent.SetSubject(executionID)
	_ = startEvent.SetData(cloudevents.ApplicationJSON, engine.ExecutionStartedData{
		WorkflowID: conditionalWorkflow.ID,
		InputData:  map[string]any{"value": 15}, // Will take "true" path
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

func toFloat64(v any) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case int:
		return float64(val)
	case int64:
		return float64(val)
	default:
		return 0
	}
}
