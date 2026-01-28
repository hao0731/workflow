# Event Marketplace User Scenarios Design

## Overview

This document describes the design for demonstrating the Event Marketplace feature using a realistic HR-to-Chat service integration scenario.

**Scenario Summary:**
- **HR Service Workflow** — Manually triggered with `name` and `employee_id` input. Runs an "on-boarding" task, then publishes a `new_employee` event to the marketplace.
- **Chat Service Workflow** — Subscribes to `hr.new_employee` events. When triggered, runs a "new-channel-member" placeholder task that logs/acknowledges receipt.

---

## Event Flow

```
┌─────────────────────────────────────────────────────────────────────────┐
│                        End-to-End Event Flow                            │
│                                                                         │
│  [API Call]                                                             │
│      │ POST /api/workflows/hr-onboarding/execute                        │
│      │ { "name": "Alice", "employee_id": "EMP-001" }                    │
│      ▼                                                                  │
│  [HR Service Workflow]                                                  │
│      │                                                                  │
│      ├── StartNode (receives input)                                     │
│      │                                                                  │
│      ├── on-boarding (task node - placeholder)                          │
│      │                                                                  │
│      └── PublishEvent node                                              │
│           │ event: hr.new_employee                                      │
│           │ payload: { name, employee_id }                              │
│           ▼                                                             │
│  [NATS: marketplace.hr.new_employee]                                    │
│           │                                                             │
│           │  Event Router detects trigger                               │
│           ▼                                                             │
│  [Chat Service Workflow] (auto-spawned)                                 │
│      │                                                                  │
│      ├── StartNode (event trigger, input_map)                           │
│      │                                                                  │
│      └── new-channel-member (placeholder task)                          │
│                                                                         │
└─────────────────────────────────────────────────────────────────────────┘
```

---

## HR Service Workflow

**File:** `workflows/hr-onboarding.yaml`

```yaml
id: hr-onboarding
name: "HR Employee Onboarding"
description: "Registers new employees and publishes event to marketplace"
version: "1.0.0"

# Register event in marketplace
events:
  - name: new_employee
    domain: hr
    description: "Fired when a new employee is onboarded"
    schema:
      type: object
      properties:
        name:
          type: string
          description: "Employee full name"
        employee_id:
          type: string
          description: "Unique employee identifier"
      required:
        - name
        - employee_id

nodes:
  - id: start
    type: StartNode
    name: "Start"

  - id: on-boarding
    type: LogNode
    name: "On-boarding Task"
    parameters:
      message: "Processing onboarding for {{.input.name}} ({{.input.employee_id}})"

  - id: publish-new-employee
    type: PublishEvent
    name: "Publish New Employee Event"
    parameters:
      event_name: new_employee
      domain: hr
      payload:
        name: "{{.input.name}}"
        employee_id: "{{.input.employee_id}}"

connections:
  - from: start
    to: on-boarding
  - from: on-boarding
    to: publish-new-employee
```

**Key points:**
- Uses `LogNode` as placeholder for "on-boarding" task
- Event schema defines `name` and `employee_id` as required fields
- `PublishEvent` node maps input data to the event payload

---

## Chat Service Workflow

**File:** `workflows/chat-new-member.yaml`

```yaml
id: chat-new-member
name: "Chat New Channel Member"
description: "Adds new employees to chat channels when they join"
version: "1.0.0"

nodes:
  - id: start
    type: StartNode
    name: "New Employee Trigger"
    trigger:
      type: event
      criteria:
        event_name: new_employee
        domain: hr
      input_map:
        employee_name: "{{.event.name}}"
        employee_id: "{{.event.employee_id}}"

  - id: new-channel-member
    type: LogNode
    name: "Add to Channel"
    parameters:
      message: "Adding {{.input.employee_name}} ({{.input.employee_id}}) to general channel"

connections:
  - from: start
    to: new-channel-member
```

**Key points:**
- `StartNode` uses `trigger.type: event` instead of manual trigger
- `trigger.criteria` matches the HR event by `event_name` and `domain`
- `input_map` transforms event payload fields to workflow input fields
- Uses `LogNode` as placeholder to demonstrate the event was received

---

## Verification Plan

### Step 1: Register Workflows

```bash
# Deploy HR workflow (registers event + workflow)
curl -X POST http://localhost:8083/api/workflows \
  -H "Content-Type: application/json" \
  -d @workflows/hr-onboarding.yaml

# Deploy Chat Service workflow (registers as subscriber)
curl -X POST http://localhost:8083/api/workflows \
  -H "Content-Type: application/json" \
  -d @workflows/chat-new-member.yaml
```

### Step 2: Verify Event in Marketplace

```bash
# Check HR event is registered
curl http://localhost:8083/api/events/hr/new_employee

# Expected: { "name": "new_employee", "domain": "hr", "full_name": "hr.new_employee", ... }
```

### Step 3: Trigger HR Workflow

```bash
# Start HR onboarding with employee data
curl -X POST http://localhost:8083/api/workflows/hr-onboarding/execute \
  -H "Content-Type: application/json" \
  -d '{ "name": "Alice Smith", "employee_id": "EMP-001" }'
```

### Step 4: Observe Chat Service Triggered

- Check engine logs for: `"Adding Alice Smith (EMP-001) to general channel"`
- Verify both execution records exist in the event store

---

## Technical Notes

### NATS Subject Convention
- Event subject: `marketplace.hr.new_employee`
- Event Router subscribes to: `marketplace.>`

### Event Payload (CloudEvent)
```json
{
  "specversion": "1.0",
  "type": "hr.new_employee",
  "source": "workflow://hr-onboarding",
  "id": "<uuid>",
  "data": {
    "name": "Alice Smith",
    "employee_id": "EMP-001"
  },
  "parentexecutionid": "<execution-id>"
}
```

### Trace Context
The `parentexecutionid` extension links the Chat Service execution to the originating HR workflow, enabling end-to-end observability.
