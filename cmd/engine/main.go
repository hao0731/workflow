package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/cheriehsieh/orchestration/internal/engine"
	"github.com/cheriehsieh/orchestration/internal/eventbus"
	"github.com/cheriehsieh/orchestration/internal/eventstore"
	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/google/uuid"
	"github.com/nats-io/nats.go"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func main() {
	// 1. Connect to MongoDB
	mongoURI := os.Getenv("MONGO_URI")
	if mongoURI == "" {
		mongoURI = "mongodb://localhost:27017"
	}
	client, err := mongo.Connect(context.Background(), options.Client().ApplyURI(mongoURI))
	if err != nil {
		log.Fatal(err)
	}
	defer client.Disconnect(context.Background())
	db := client.Database("orchestration")

	// 2. Connect to NATS
	natsURL := os.Getenv("NATS_URL")
	if natsURL == "" {
		natsURL = nats.DefaultURL
	}
	nc, err := nats.Connect(natsURL)
	if err != nil {
		log.Fatal(err)
	}
	defer nc.Close()

	js, err := nc.JetStream()
	if err != nil {
		log.Fatal(err)
	}

	// Create stream if it doesn't exist
	streamName := "WORKFLOW_EVENTS"
	subject := "workflow.events.>"
	_, err = js.AddStream(&nats.StreamConfig{
		Name:     streamName,
		Subjects: []string{subject},
	})
	if err != nil {
		log.Printf("Stream %s may already exist: %v", streamName, err)
	}

	// 3. Define a sample workflow
	sampleWorkflow := &engine.Workflow{
		ID: "sample-workflow-1",
		Nodes: []engine.Node{
			{ID: "start", Type: engine.StartNode, Name: "Start"},
			{ID: "set-data", Type: engine.ActionNode, Name: "Set Data", Parameters: map[string]interface{}{"value": "Hello World"}},
			{ID: "print-data", Type: engine.ActionNode, Name: "Print Data"},
		},
		Connections: []engine.Connection{
			{FromNode: "start", ToNode: "set-data"},
			{FromNode: "set-data", ToNode: "print-data"},
		},
	}

	// 4. Initialize Engine components
	eventStoreImpl := eventstore.NewMongoEventStore(db, "events")
	orchestratorBus := eventbus.NewNATSEventBus(js, "workflow.events.execution", "orchestrator")
	workerBus := eventbus.NewNATSEventBus(js, "workflow.events.execution", "worker")
	workflowRepo := &InMemoryWorkflowRepo{workflows: map[string]*engine.Workflow{sampleWorkflow.ID: sampleWorkflow}}

	orchestrator := engine.NewOrchestrator(eventStoreImpl, orchestratorBus, orchestratorBus, workflowRepo)
	worker := engine.NewWorker(eventStoreImpl, workerBus, workerBus, workflowRepo)

	// Register node handlers
	worker.Register(engine.StartNode, func(ctx context.Context, input, params map[string]interface{}) (map[string]interface{}, error) {
		log.Println("--- Start Node Executed ---")
		return input, nil
	})

	worker.Register(engine.ActionNode, func(ctx context.Context, input, params map[string]interface{}) (map[string]interface{}, error) {
		log.Printf("--- Action Node Executed (%v) ---", params)
		output := make(map[string]interface{})
		for k, v := range input {
			output[k] = v
		}
		for k, v := range params {
			output[k] = v
		}
		return output, nil
	})

	// 5. Start Orchestrator and Worker
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		if err := orchestrator.Start(ctx); err != nil {
			log.Printf("Orchestrator error: %v", err)
		}
	}()

	go func() {
		if err := worker.Start(ctx); err != nil {
			log.Printf("Worker error: %v", err)
		}
	}()

	// 6. Trigger a workflow execution
	time.Sleep(1 * time.Second)
	executionID := fmt.Sprintf("exec-%d", time.Now().Unix())
	log.Printf("Triggering execution %s", executionID)

	startEvent := cloudevents.NewEvent()
	startEvent.SetID(uuid.New().String())
	startEvent.SetSource(engine.EventSource)
	startEvent.SetType(engine.ExecutionStarted)
	startEvent.SetSubject(executionID)
	startEvent.SetData(cloudevents.ApplicationJSON, engine.ExecutionStartedData{
		WorkflowID: sampleWorkflow.ID,
		InputData:  map[string]interface{}{"initiated_by": "prototype-client"},
	})

	if err := eventStoreImpl.Append(ctx, startEvent); err != nil {
		log.Fatalf("Error storing start event: %v", err)
	}
	if err := orchestratorBus.Publish(ctx, startEvent); err != nil {
		log.Fatalf("Error publishing start event: %v", err)
	}

	// 7. Wait for signal to exit
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan
	log.Println("Shutting down...")
}

// InMemoryWorkflowRepo is a simple in-memory workflow repository for prototyping.
type InMemoryWorkflowRepo struct {
	workflows map[string]*engine.Workflow
}

func (r *InMemoryWorkflowRepo) GetByID(ctx context.Context, id string) (*engine.Workflow, error) {
	if w, ok := r.workflows[id]; ok {
		return w, nil
	}
	return nil, fmt.Errorf("workflow %s not found", id)
}
