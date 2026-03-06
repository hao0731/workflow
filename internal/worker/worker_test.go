package worker

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cheriehsieh/orchestration/internal/engine"
	"github.com/cheriehsieh/orchestration/internal/messaging"
	"github.com/cheriehsieh/orchestration/internal/scheduler"
)

type capturePublisher struct {
	events []cloudevents.Event
}

type stubJetStream struct {
	nats.JetStreamContext
}

func (p *capturePublisher) Publish(_ context.Context, event cloudevents.Event) error {
	p.events = append(p.events, event)
	return nil
}

func noopLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestFirstPartyWorkerSubjects(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []string{
		messaging.CommandNodeExecuteSubjectFromFullType(string(engine.StartNode)),
		messaging.CommandNodeExecuteSubjectFromFullType(string(engine.JoinNode)),
		messaging.CommandNodeExecuteSubjectFromFullType(string(engine.PublishEvent)),
	}, FirstPartyWorkerSubjects())
}

func TestNewFirstPartyWorkers_RegistersBuiltInNodeTypes(t *testing.T) {
	t.Parallel()

	workers := NewFirstPartyWorkers(&stubJetStream{}, &capturePublisher{}, nil, noopLogger())
	require.Len(t, workers, 3)

	assert.Equal(t, engine.StartNode, workers[0].NodeType)
	assert.Equal(t, engine.JoinNode, workers[1].NodeType)
	assert.Equal(t, engine.PublishEvent, workers[2].NodeType)
	assert.Equal(t, FirstPartyWorkerSubjects()[0], workers[0].Subject)
	assert.Equal(t, FirstPartyWorkerSubjects()[1], workers[1].Subject)
	assert.Equal(t, FirstPartyWorkerSubjects()[2], workers[2].Subject)
	assert.NotNil(t, workers[0].Service)
	assert.NotNil(t, workers[1].Service)
	assert.NotNil(t, workers[2].Service)
}

func TestWorker_HandleDispatch_PublishesNodeExecutedV1(t *testing.T) {
	t.Parallel()

	publisher := &capturePublisher{}
	w := NewWorker(
		engine.NodeType("http-request@v1"),
		func(_ context.Context, input, _ map[string]any) (engine.NodeResult, error) {
			return engine.NodeResult{
				Output: map[string]any{"input": input["message"]},
				Port:   engine.PortSuccess,
			}, nil
		},
		nil,
		publisher,
	)

	event := cloudevents.NewEvent()
	event.SetID("dispatch-1")
	event.SetSource("scheduler")
	event.SetType(messaging.CommandNodeExecuteSubjectFromFullType("http-request@v1"))
	event.SetSubject("exec-77")
	event.SetExtension("workflowid", "workflow-77")
	event.SetExtension("idempotencykey", "node:exec-77:send-email:2:1:v1")
	_ = event.SetData(cloudevents.ApplicationJSON, scheduler.NodeDispatchData{
		ExecutionID: "exec-77",
		NodeID:      "send-email",
		NodeType:    "http-request@v1",
		InputData:   map[string]any{"message": "hello"},
		RunIndex:    2,
	})

	err := w.handleDispatch(context.Background(), event)
	require.NoError(t, err)

	require.Len(t, publisher.events, 1)
	result := publisher.events[0]
	assert.Equal(t, messaging.EventTypeNodeExecutedV1, result.Type())
	assert.Equal(t, "workflow-77", result.Extensions()["workflowid"])
	assert.Equal(t, "exec-77", result.Extensions()["executionid"])
	assert.Equal(t, "send-email", result.Extensions()["nodeid"])
	assert.EqualValues(t, 2, result.Extensions()["runindex"])
	assert.EqualValues(t, 1, result.Extensions()["attempt"])
	assert.Equal(t, "worker/http-request@v1", result.Extensions()["producer"])
	assert.Equal(t, "node:exec-77:send-email:2:1:v1", result.Extensions()["idempotencykey"])
}

func TestWorker_HandleDispatch_PublishesNodeExecutedV1OnError(t *testing.T) {
	t.Parallel()

	publisher := &capturePublisher{}
	w := NewWorker(
		engine.NodeType("http-request@v1"),
		func(_ context.Context, _, _ map[string]any) (engine.NodeResult, error) {
			return engine.NodeResult{}, errors.New("boom")
		},
		nil,
		publisher,
	)

	event := cloudevents.NewEvent()
	event.SetID("dispatch-2")
	event.SetSource("scheduler")
	event.SetType(messaging.CommandNodeExecuteSubjectFromFullType("http-request@v1"))
	event.SetSubject("exec-78")
	event.SetExtension("workflowid", "workflow-78")
	_ = event.SetData(cloudevents.ApplicationJSON, scheduler.NodeDispatchData{
		ExecutionID: "exec-78",
		NodeID:      "send-email",
		NodeType:    "http-request@v1",
		RunIndex:    0,
	})

	err := w.handleDispatch(context.Background(), event)
	require.NoError(t, err)

	require.Len(t, publisher.events, 1)
	assert.Equal(t, messaging.EventTypeNodeExecutedV1, publisher.events[0].Type())

	var data scheduler.NodeResultData
	require.NoError(t, publisher.events[0].DataAs(&data))
	assert.Equal(t, "boom", data.Error)
}
