package eventbus

import (
	"context"
	"testing"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cheriehsieh/orchestration/internal/messaging"
)

type fakeJetStream struct {
	addedStreams   []nats.StreamConfig
	published      []*nats.Msg
	existingStream map[string]nats.StreamConfig
}

func (f *fakeJetStream) AddStream(cfg *nats.StreamConfig, _ ...nats.JSOpt) (*nats.StreamInfo, error) {
	f.addedStreams = append(f.addedStreams, *cfg)
	return &nats.StreamInfo{Config: *cfg}, nil
}

func (f *fakeJetStream) StreamInfo(name string, _ ...nats.JSOpt) (*nats.StreamInfo, error) {
	if cfg, ok := f.existingStream[name]; ok {
		return &nats.StreamInfo{Config: cfg}, nil
	}

	return nil, nats.ErrStreamNotFound
}

func (f *fakeJetStream) PublishMsg(msg *nats.Msg, _ ...nats.PubOpt) (*nats.PubAck, error) {
	clone := nats.NewMsg(msg.Subject)
	clone.Data = append([]byte(nil), msg.Data...)
	for key, values := range msg.Header {
		for _, value := range values {
			clone.Header.Add(key, value)
		}
	}

	f.published = append(f.published, clone)

	return &nats.PubAck{Stream: "TEST"}, nil
}

func (f *fakeJetStream) PullSubscribe(_ string, _ string, _ ...nats.SubOpt) (*nats.Subscription, error) {
	return nil, nil
}

func TestBootstrapWorkflowStreamsAvoidsBroadLegacySubjects(t *testing.T) {
	t.Parallel()

	js := &fakeJetStream{}

	err := BootstrapWorkflowStreams(js, true)
	require.NoError(t, err)

	require.Len(t, js.addedStreams, 6)

	subjects := make([]string, 0, 12)
	for _, stream := range js.addedStreams {
		subjects = append(subjects, stream.Subjects...)
	}

	assert.Contains(t, subjects, "workflow.command.>")
	assert.Contains(t, subjects, "workflow.event.>")
	assert.Contains(t, subjects, "workflow.runtime.>")
	assert.Contains(t, subjects, "workflow.dlq.>")
	assert.Contains(t, subjects, "workflow.events.execution")
	assert.Contains(t, subjects, "workflow.events.scheduler")
	assert.Contains(t, subjects, "workflow.events.results")
	assert.Contains(t, subjects, "workflow.nodes.*.*")

	assert.NotContains(t, subjects, "workflow.events.>")
	assert.NotContains(t, subjects, "workflow.nodes.>")

	assert.Equal(t, messaging.StreamNameCommands, js.addedStreams[0].Name)
	assert.Equal(t, messaging.StreamNameEvents, js.addedStreams[1].Name)
	assert.Equal(t, messaging.StreamNameRuntime, js.addedStreams[2].Name)
	assert.Equal(t, messaging.StreamNameDLQ, js.addedStreams[3].Name)
}

func TestNATSEventBusPublishWithOptions(t *testing.T) {
	t.Parallel()

	js := &fakeJetStream{}
	bus := NewNATSEventBus(js, messaging.SubjectRuntimeExecutionStartedV1, "workflow-api")

	event := cloudevents.NewEvent()
	event.SetID("execution-123")
	event.SetSource("test/workflow-api")
	event.SetType(messaging.EventTypeRuntimeExecutionStartedV1)
	event.SetDataContentType(cloudevents.ApplicationJSON)
	require.NoError(t, event.SetData(cloudevents.ApplicationJSON, map[string]any{
		"workflow_id":  "wf-123",
		"execution_id": "execution-123",
	}))

	headers := nats.Header{}
	headers.Set("Nats-Msg-Id", "execution-123")
	headers.Set("traceparent", "00-abc-123-01")

	err := bus.PublishWithOptions(
		context.Background(),
		event,
		WithPublishSubject(messaging.SubjectCommandExecuteV1),
		WithPublishHeaders(headers),
	)
	require.NoError(t, err)

	require.Len(t, js.published, 1)
	assert.Equal(t, messaging.SubjectCommandExecuteV1, js.published[0].Subject)
	assert.Equal(t, "execution-123", js.published[0].Header.Get("Nats-Msg-Id"))
	assert.Equal(t, "00-abc-123-01", js.published[0].Header.Get("traceparent"))

	got := cloudevents.NewEvent()
	require.NoError(t, got.UnmarshalJSON(js.published[0].Data))
	assert.Equal(t, event.ID(), got.ID())
	assert.Equal(t, event.Type(), got.Type())
}
