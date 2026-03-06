package messaging

import (
	"fmt"

	cloudevents "github.com/cloudevents/sdk-go/v2"
)

const (
	LegacyEventTypeExecutionStarted   = "orchestration.execution.started"
	LegacyEventTypeNodeScheduled      = "orchestration.node.scheduled"
	LegacyEventTypeNodeCompleted      = "orchestration.node.completed"
	LegacyEventTypeNodeFailed         = "orchestration.node.failed"
	LegacyEventTypeExecutionCompleted = "orchestration.execution.completed"
	LegacyEventTypeExecutionFailed    = "orchestration.execution.failed"
	LegacyEventTypeNodeDispatch       = "orchestration.node.dispatch"
	LegacyEventTypeNodeResult         = "orchestration.node.result"
)

// TranslateLegacyEvent maps a legacy workflow event to the v2 contract and
// returns the NATS subject it should be published to.
func TranslateLegacyEvent(event cloudevents.Event) (cloudevents.Event, string, bool, error) {
	translated := event

	switch event.Type() {
	case LegacyEventTypeExecutionStarted:
		translated.SetType(EventTypeRuntimeExecutionStartedV1)
	case LegacyEventTypeNodeScheduled:
		translated.SetType(EventTypeRuntimeNodeScheduledV1)
	case LegacyEventTypeNodeCompleted, LegacyEventTypeNodeFailed:
		translated.SetType(EventTypeRuntimeNodeExecutedV1)
	case LegacyEventTypeExecutionCompleted:
		translated.SetType("workflow.runtime.execution.completed.v1")
	case LegacyEventTypeExecutionFailed:
		translated.SetType("workflow.runtime.execution.failed.v1")
	case LegacyEventTypeNodeResult:
		translated.SetType(EventTypeNodeExecutedV1)
	case LegacyEventTypeNodeDispatch:
		nodeType := eventExtensionString(event, "nodetype")
		if nodeType == "" {
			var payload map[string]any
			if err := event.DataAs(&payload); err != nil {
				return cloudevents.Event{}, "", false, fmt.Errorf("decode legacy node dispatch: %w", err)
			}
			nodeType, _ = payload["node_type"].(string)
		}
		if nodeType == "" {
			return cloudevents.Event{}, "", false, fmt.Errorf("legacy node dispatch missing node_type")
		}

		translated.SetType(CommandNodeExecuteSubjectFromFullType(nodeType))
	default:
		return cloudevents.Event{}, "", false, nil
	}

	return translated, translated.Type(), true, nil
}

func eventExtensionString(event cloudevents.Event, key string) string {
	value, ok := event.Extensions()[key]
	if !ok {
		return ""
	}

	strValue, ok := value.(string)
	if ok {
		return strValue
	}

	return fmt.Sprint(value)
}
