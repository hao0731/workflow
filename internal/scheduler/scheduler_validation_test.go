package scheduler

import (
	"context"
	"testing"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cheriehsieh/orchestration/internal/engine"
	"github.com/cheriehsieh/orchestration/internal/messaging"
)

func TestSchedulerValidationFailurePublishesDLQ(t *testing.T) {
	t.Parallel()

	store := &captureEventStore{}
	runtimePublisher := &capturePublisher{}
	dlqPublisher := &capturePublisher{}

	s := NewScheduler(
		store,
		runtimePublisher,
		nil,
		nil,
		&captureDispatcher{},
		&staticWorkflowRepo{},
		WithSchedulerLogger(noopLogger()),
		WithValidationDLQPublisher(dlqPublisher),
	)

	event := cloudevents.NewEvent()
	event.SetID("node-result-invalid")
	event.SetSource("worker/http-request-v1")
	event.SetType(messaging.EventTypeNodeExecutedV1)
	event.SetSubject("exec-invalid")
	event.SetExtension("workflowid", "workflow-invalid")
	event.SetExtension("executionid", "exec-invalid")
	event.SetExtension("attempt", 1)
	event.SetExtension("idempotencykey", "node:exec-invalid::0:1:v1")
	event.SetExtension("producer", "worker/http-request@v1")
	_ = event.SetData(cloudevents.ApplicationJSON, NodeResultData{
		ExecutionID: "exec-invalid",
		OutputPort:  engine.PortSuccess,
		OutputData:  map[string]any{"status": "ok"},
		RunIndex:    0,
	})

	err := s.handleResult(context.Background(), event)
	require.NoError(t, err)

	assert.Empty(t, store.events)
	assert.Empty(t, runtimePublisher.events)
	require.Len(t, dlqPublisher.events, 1)
	assert.Equal(t, messaging.EventTypeDLQSchedulerValidationV1, dlqPublisher.events[0].Type())

	var payload ValidationFailureData
	require.NoError(t, dlqPublisher.events[0].DataAs(&payload))
	assert.Equal(t, "nodeid", payload.MissingExtensions[0])
	assert.Equal(t, event.ID(), payload.EventID)
}
