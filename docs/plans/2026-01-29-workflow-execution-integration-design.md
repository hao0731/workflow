# Workflow API + Engine Integration Design

## Overview

Enable workflow execution by adding a `POST /api/workflows/:id/execute` endpoint to `workflow-api` that publishes an `ExecutionStarted` CloudEvent to NATS, which the engine's Orchestrator receives to start execution.

## Architecture

```
┌──────────────┐   NATS    ┌──────────────┐
│ workflow-api │ ────────► │   engine     │
│  (port 8083) │           │              │
│              │           │ • Orchestrator│
│ POST execute │           │ • Scheduler  │
└──────────────┘           │ • Workers    │
       │                   └──────────────┘
       └────────┬──────────────────┘
                ▼
         ┌──────────────┐
         │   MongoDB    │
         │ • workflows  │
         │ • events     │
         └──────────────┘
```

## API Specification

### `POST /api/workflows/:id/execute`

**Request:**
```json
{
  "input": {
    "order_id": "ORD-12345",
    "total": 199.99
  }
}
```

**Response (201 Created):**
```json
{
  "execution_id": "exec-1706488800",
  "workflow_id": "simple-flow",
  "status": "started",
  "started_at": "2026-01-29T00:28:00Z"
}
```

**Errors:**
- `404` — Workflow not found
- `400` — Invalid input JSON

## Implementation

### Files to Modify

| File | Change |
|------|--------|
| `cmd/workflow-api/main.go` | Add NATS connection, inject EventBus into handler |
| `internal/dsl/api/handler.go` | Add `ExecuteWorkflow` method, register route |

### Handler Logic

1. Verify workflow exists in registry
2. Parse input from request body
3. Generate execution ID (`exec-{timestamp}`)
4. Create `orchestration.execution.started` CloudEvent
5. Publish to NATS subject `workflow.events.execution`
6. Return execution ID to client

## Verification

```bash
# 1. Start services
docker compose up -d mongodb nats
go run ./cmd/engine/main.go &
go run ./cmd/workflow-api/main.go

# 2. Create workflow (YAML)
curl -X POST http://localhost:8083/api/workflows \
  -H "Content-Type: text/yaml" \
  -d 'id: test-flow
name: Test
version: "1.0"
nodes:
  - id: start
    type: StartNode
  - id: action
    type: ActionNode
connections:
  - from: start
    to: action'

# 3. Execute workflow
curl -X POST http://localhost:8083/api/workflows/test-flow/execute \
  -H "Content-Type: application/json" \
  -d '{"input": {"message": "hello"}}'

# 4. Check execution
curl http://localhost:8083/api/executions/{execution_id}
```
