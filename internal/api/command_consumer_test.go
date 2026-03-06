package api

import (
	"context"
	"testing"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cheriehsieh/orchestration/internal/messaging"
)

type captureExecutionStarter struct {
	requests []ExecutionStartCommand
	result   *ExecutionStartResult
	err      error
}

func (s *captureExecutionStarter) StartExecution(_ context.Context, command ExecutionStartCommand) (*ExecutionStartResult, error) {
	s.requests = append(s.requests, command)
	if s.err != nil {
		return nil, s.err
	}

	return s.result, nil
}

func TestCommandConsumer_ConsumeExecuteCommand(t *testing.T) {
	t.Parallel()

	starter := &captureExecutionStarter{
		result: &ExecutionStartResult{
			ExecutionID: "exec-123",
			WorkflowID:  "wf-123",
			Status:      "started",
		},
	}
	consumer := NewCommandConsumer(nil, starter)

	event := cloudevents.NewEvent()
	event.SetID("cmd-123")
	event.SetSource("hr-system")
	event.SetType(messaging.EventTypeCommandExecuteV1)
	event.SetExtension("workflowid", "wf-123")
	event.SetExtension("executionid", "exec-123")
	event.SetExtension("producer", "hr-system")
	require.NoError(t, event.SetData(cloudevents.ApplicationJSON, map[string]any{
		"workflowId": "wf-123",
		"input": map[string]any{
			"employeeId": "emp-1001",
		},
	}))

	err := consumer.Handle(context.Background(), event)
	require.NoError(t, err)

	require.Len(t, starter.requests, 1)
	assert.Equal(t, "wf-123", starter.requests[0].WorkflowID)
	assert.Equal(t, "exec-123", starter.requests[0].ExecutionID)
	assert.Equal(t, "hr-system", starter.requests[0].Producer)
	assert.Equal(t, map[string]any{"employeeId": "emp-1001"}, starter.requests[0].Input)
}

func TestCommandConsumer_ConsumeExecuteCommand_ForwardsIdempotencyKey(t *testing.T) {
	t.Parallel()

	starter := &captureExecutionStarter{
		result: &ExecutionStartResult{
			ExecutionID: "exec-123",
			WorkflowID:  "wf-123",
			Status:      "started",
		},
	}
	consumer := NewCommandConsumer(nil, starter)

	event := cloudevents.NewEvent()
	event.SetID("cmd-123")
	event.SetSource("hr-system")
	event.SetType(messaging.EventTypeCommandExecuteV1)
	event.SetExtension("workflowid", "wf-123")
	event.SetExtension("executionid", "exec-123")
	event.SetExtension("idempotencykey", "cmd:hr-system:employee:emp-1001:onboard:v1")
	require.NoError(t, event.SetData(cloudevents.ApplicationJSON, map[string]any{
		"workflowId": "wf-123",
		"input": map[string]any{
			"employeeId": "emp-1001",
		},
	}))

	err := consumer.Handle(context.Background(), event)
	require.NoError(t, err)

	require.Len(t, starter.requests, 1)
	assert.Equal(t, "cmd:hr-system:employee:emp-1001:onboard:v1", starter.requests[0].IdempotencyKey)
}
