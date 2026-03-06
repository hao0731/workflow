package messaging

import (
	"fmt"
	"strings"
)

const (
	subjectPrefix = "workflow"

	PlaneCommand = "command"
	PlaneEvent   = "event"
	PlaneRuntime = "runtime"
	PlaneDLQ     = "dlq"
)

const (
	SubjectCommandExecuteV1          = "workflow.command.execute.v1"
	SubjectRuntimeExecutionStartedV1 = "workflow.runtime.execution.started.v1"
	SubjectRuntimeNodeScheduledV1    = "workflow.runtime.node.scheduled.v1"
	SubjectEventNodeExecutedV1       = "workflow.event.node.executed.v1"
	SubjectRuntimeNodeExecutedV1     = "workflow.runtime.node.executed.v1"
	SubjectDLQSchedulerValidationV1  = "workflow.dlq.scheduler.validation.v1"
)

const (
	EventTypeCommandExecuteV1          = SubjectCommandExecuteV1
	EventTypeRuntimeExecutionStartedV1 = SubjectRuntimeExecutionStartedV1
	EventTypeRuntimeNodeScheduledV1    = SubjectRuntimeNodeScheduledV1
	EventTypeNodeExecutedV1            = SubjectEventNodeExecutedV1
	EventTypeRuntimeNodeExecutedV1     = SubjectRuntimeNodeExecutedV1
	EventTypeDLQSchedulerValidationV1  = SubjectDLQSchedulerValidationV1
)

func PlaneSubject(plane string) string {
	return fmt.Sprintf("%s.%s", subjectPrefix, plane)
}

func PlaneWildcardSubject(plane string) string {
	return PlaneSubject(plane) + ".>"
}

func CommandNodeExecuteSubject(nodeType, version string) string {
	if version == "" {
		version = "v1"
	}

	return fmt.Sprintf("%s.node.execute.%s.%s", PlaneSubject(PlaneCommand), nodeType, version)
}

func CommandNodeExecuteSubjectFromFullType(fullType string) string {
	nodeType, version := SplitFullType(fullType)
	return CommandNodeExecuteSubject(nodeType, version)
}

func SplitFullType(fullType string) (nodeType, version string) {
	parts := strings.SplitN(fullType, "@", 2)
	nodeType = parts[0]
	if len(parts) == 2 && parts[1] != "" {
		return nodeType, parts[1]
	}

	return nodeType, "v1"
}
