package eventstore

import (
	"context"
	"testing"
	"time"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProjectionConsumer_ProjectsEventToMongo(t *testing.T) {
	mongo := &fakeEventStore{}
	consumer := NewProjectionConsumer(mongo)

	event := cloudevents.NewEvent()
	event.SetID("proj-evt-1")
	event.SetSource("test")
	event.SetType("orchestration.execution.started")
	event.SetSubject("exec-proj-1")
	event.SetTime(time.Now().Truncate(time.Millisecond))
	_ = event.SetData(cloudevents.ApplicationJSON, map[string]any{"workflow_id": "wf-1"})

	err := consumer.HandleEvent(context.Background(), event)
	require.NoError(t, err)
	assert.Len(t, mongo.appended, 1)
	assert.Equal(t, "proj-evt-1", mongo.appended[0].ID())
}

func TestProjectionConsumer_Start_WithoutSubscriber_ReturnsError(t *testing.T) {
	mongo := &fakeEventStore{}
	consumer := NewProjectionConsumer(mongo)

	err := consumer.Start(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "subscriber not configured")
}
