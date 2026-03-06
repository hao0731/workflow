package messaging

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCommandNodeExecuteSubject(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		nodeType string
		version  string
		want     string
	}{
		{
			name:     "built in start node",
			nodeType: "StartNode",
			version:  "v1",
			want:     "workflow.command.node.execute.StartNode.v1",
		},
		{
			name:     "built in publish event node",
			nodeType: "PublishEvent",
			version:  "v1",
			want:     "workflow.command.node.execute.PublishEvent.v1",
		},
		{
			name:     "custom node version",
			nodeType: "http-request",
			version:  "v2",
			want:     "workflow.command.node.execute.http-request.v2",
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.want, CommandNodeExecuteSubject(tc.nodeType, tc.version))
		})
	}
}

func TestCommandNodeExecuteSubjectFromFullType(t *testing.T) {
	t.Parallel()

	assert.Equal(
		t,
		"workflow.command.node.execute.http-request.v3",
		CommandNodeExecuteSubjectFromFullType("http-request@v3"),
	)
	assert.Equal(
		t,
		"workflow.command.node.execute.StartNode.v1",
		CommandNodeExecuteSubjectFromFullType("StartNode"),
	)
}

func TestCoreWorkflowSubjects(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "workflow.command.execute.v1", SubjectCommandExecuteV1)
	assert.Equal(t, "workflow.runtime.execution.started.v1", SubjectRuntimeExecutionStartedV1)
	assert.Equal(t, "workflow.runtime.node.scheduled.v1", SubjectRuntimeNodeScheduledV1)
	assert.Equal(t, "workflow.event.node.executed.v1", SubjectEventNodeExecutedV1)
	assert.Equal(t, "workflow.runtime.node.executed.v1", SubjectRuntimeNodeExecutedV1)
	assert.Equal(t, "workflow.dlq.scheduler.validation.v1", SubjectDLQSchedulerValidationV1)

	assert.Equal(t, SubjectCommandExecuteV1, EventTypeCommandExecuteV1)
	assert.Equal(t, SubjectRuntimeExecutionStartedV1, EventTypeRuntimeExecutionStartedV1)
	assert.Equal(t, SubjectRuntimeNodeScheduledV1, EventTypeRuntimeNodeScheduledV1)
	assert.Equal(t, SubjectEventNodeExecutedV1, EventTypeNodeExecutedV1)
	assert.Equal(t, SubjectRuntimeNodeExecutedV1, EventTypeRuntimeNodeExecutedV1)
	assert.Equal(t, SubjectDLQSchedulerValidationV1, EventTypeDLQSchedulerValidationV1)
}

func TestWorkflowStreamConfigs(t *testing.T) {
	t.Parallel()

	streams := WorkflowStreams()
	require.Len(t, streams, 4)

	assert.Equal(t, StreamNameCommands, streams[0].Name)
	assert.Equal(t, []string{"workflow.command.>"}, streams[0].Subjects)

	assert.Equal(t, StreamNameEvents, streams[1].Name)
	assert.Equal(t, []string{"workflow.event.>"}, streams[1].Subjects)

	assert.Equal(t, StreamNameRuntime, streams[2].Name)
	assert.Equal(t, []string{"workflow.runtime.>"}, streams[2].Subjects)

	assert.Equal(t, StreamNameDLQ, streams[3].Name)
	assert.Equal(t, []string{"workflow.dlq.>"}, streams[3].Subjects)
}
