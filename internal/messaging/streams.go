package messaging

import "github.com/nats-io/nats.go"

const (
	StreamNameCommands     = "WF_COMMANDS"
	StreamNameEvents       = "WF_EVENTS"
	StreamNameRuntime      = "WF_RUNTIME"
	StreamNameDLQ          = "WF_DLQ"
	StreamNameLegacyEvents = "WF_LEGACY_EVENTS"
	StreamNameLegacyNodes  = "WF_LEGACY_NODES"
)

func WorkflowStreams() []nats.StreamConfig {
	return []nats.StreamConfig{
		{
			Name:     StreamNameCommands,
			Subjects: []string{PlaneWildcardSubject(PlaneCommand)},
		},
		{
			Name:     StreamNameEvents,
			Subjects: []string{PlaneWildcardSubject(PlaneEvent)},
		},
		{
			Name:     StreamNameRuntime,
			Subjects: []string{PlaneWildcardSubject(PlaneRuntime)},
		},
		{
			Name:     StreamNameDLQ,
			Subjects: []string{PlaneWildcardSubject(PlaneDLQ)},
		},
	}
}

func LegacyWorkflowStreams() []nats.StreamConfig {
	return []nats.StreamConfig{
		{
			Name: StreamNameLegacyEvents,
			Subjects: []string{
				"workflow.events.execution",
				"workflow.events.scheduler",
				"workflow.events.results",
			},
		},
		{
			Name: StreamNameLegacyNodes,
			Subjects: []string{
				"workflow.nodes.*",
				"workflow.nodes.*.*",
			},
		},
	}
}
