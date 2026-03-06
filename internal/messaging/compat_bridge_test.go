package messaging_test

import (
	"testing"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/cheriehsieh/orchestration/internal/engine"
	"github.com/cheriehsieh/orchestration/internal/messaging"
)

func TestTranslateLegacyEvent(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name        string
		subject     string
		legacyType  string
		nodeType    string
		wantSubject string
		wantType    string
	}{
		{
			name:        "execution started",
			subject:     "exec-1",
			legacyType:  engine.LegacyExecutionStarted,
			wantSubject: messaging.SubjectRuntimeExecutionStartedV1,
			wantType:    messaging.EventTypeRuntimeExecutionStartedV1,
		},
		{
			name:        "node scheduled",
			subject:     "exec-1",
			legacyType:  engine.LegacyNodeExecutionScheduled,
			wantSubject: messaging.SubjectRuntimeNodeScheduledV1,
			wantType:    messaging.EventTypeRuntimeNodeScheduledV1,
		},
		{
			name:        "worker result",
			subject:     "exec-1",
			legacyType:  messaging.LegacyEventTypeNodeResult,
			wantSubject: messaging.SubjectEventNodeExecutedV1,
			wantType:    messaging.EventTypeNodeExecutedV1,
		},
		{
			name:        "node dispatch",
			subject:     "exec-1",
			legacyType:  messaging.LegacyEventTypeNodeDispatch,
			nodeType:    "http-request@v1",
			wantSubject: messaging.CommandNodeExecuteSubjectFromFullType("http-request@v1"),
			wantType:    messaging.CommandNodeExecuteSubjectFromFullType("http-request@v1"),
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			event := cloudevents.NewEvent()
			event.SetID("legacy-1")
			event.SetSource("legacy")
			event.SetType(tc.legacyType)
			event.SetSubject(tc.subject)
			if tc.nodeType != "" {
				event.SetExtension("nodetype", tc.nodeType)
			}
			require.NoError(t, event.SetData(cloudevents.ApplicationJSON, map[string]any{
				"node_type": tc.nodeType,
			}))

			translated, publishSubject, ok, err := messaging.TranslateLegacyEvent(event)
			require.NoError(t, err)
			require.True(t, ok)

			assert.Equal(t, tc.wantSubject, publishSubject)
			assert.Equal(t, tc.wantType, translated.Type())
			assert.Equal(t, event.Subject(), translated.Subject())
		})
	}
}
