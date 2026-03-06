# Workflow V2 Phase 2 Messaging Contract Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Move the runtime from the legacy `workflow.events.*` / `workflow.nodes.*` contract to the versioned `workflow.command.*`, `workflow.runtime.*`, `workflow.event.*`, and `workflow.dlq.*` contract defined in `docs/new-technical-design.md`.

**Architecture:** Introduce a single source of truth for subject names, event types, and stream bootstrap, then update publishers and subscribers one service boundary at a time. Use a temporary compatibility bridge so migration does not require a flag day.

**Tech Stack:** Go 1.25, NATS JetStream, CloudEvents, testify

### Task 1: Add a Central Messaging Contract Package

**Files:**
- Create: `internal/messaging/contracts.go`
- Create: `internal/messaging/streams.go`
- Test: `internal/messaging/contracts_test.go`
- Modify: `docs/new-technical-design.md`

**Step 1: Write the failing tests**

- Add tests for canonical subject builders, expected stream names, and required event-type constants.
- Cover both built-in worker dispatch subjects and generic runtime subjects.

**Step 2: Run the new package tests**

Run:

```bash
go test ./internal/messaging -v
```

Expected: FAIL because the package does not exist yet.

**Step 3: Implement the messaging contract package**

- Define constants/builders for the four planes: `command`, `event`, `runtime`, `dlq`.
- Include helpers for `workflow.command.node.execute.<nodeType>.v<version>`.
- Add stream bootstrap definitions for `WF_COMMANDS`, `WF_EVENTS`, `WF_RUNTIME`, and `WF_DLQ`.
- Update `docs/new-technical-design.md` only if implementation naming differs from the current text.

**Step 4: Re-run the package tests**

Run:

```bash
go test ./internal/messaging -v
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/messaging/contracts.go internal/messaging/streams.go internal/messaging/contracts_test.go docs/new-technical-design.md
git commit -m "feat(messaging): add workflow v2 subject and stream contract package"
```

### Task 2: Update NATS Bootstrap to Create Plane-Specific Streams

**Files:**
- Modify: `cmd/workflow-api/main.go`
- Modify: `cmd/engine/main.go`
- Modify: `cmd/registry/main.go`
- Modify: `internal/eventbus/nats.go`
- Test: `internal/eventbus/nats_test.go` or create if missing

**Step 1: Write the failing tests**

- Add eventbus/bootstrap tests that assert the configured streams no longer use broad `workflow.events.>` or `workflow.nodes.>` subjects.
- Add coverage for publisher support of JetStream headers needed later by idempotency work.

**Step 2: Run the targeted tests**

Run:

```bash
go test ./internal/eventbus -v
```

Expected: FAIL because stream bootstrap and publisher behavior still reflect the old contract.

**Step 3: Implement the bootstrap update**

- Replace ad hoc stream creation in the `cmd/*` entrypoints with helpers from `internal/messaging`.
- Keep `internal/eventbus.NATSEventBus` generic, but let publishers accept optional subject override and header injection so the same component can be reused across services.
- Do not remove legacy streams yet if they are still needed by the compatibility bridge in Task 4.

**Step 4: Re-run the targeted tests**

Run:

```bash
go test ./internal/eventbus -v
```

Expected: PASS.

**Step 5: Commit**

```bash
git add cmd/workflow-api/main.go cmd/engine/main.go cmd/registry/main.go internal/eventbus/nats.go internal/eventbus/nats_test.go
git commit -m "refactor(nats): bootstrap plane-specific workflow streams"
```

### Task 3: Move Workflow API Onto the V2 Execute Flow

**Files:**
- Modify: `internal/dsl/api/handler.go`
- Modify: `cmd/workflow-api/main.go`
- Create: `internal/api/command_consumer.go`
- Test: `internal/dsl/api/handler_test.go`
- Test: `internal/api/command_consumer_test.go`

**Step 1: Write the failing tests**

- Add handler tests that assert REST execution publishes `workflow.runtime.execution.started.v1`.
- Add new consumer tests for `workflow.command.execute.v1` so `workflow-api` can bridge external NATS commands into the same execution-start path.

**Step 2: Run the targeted tests**

Run:

```bash
go test ./internal/dsl/api ./internal/api -run 'Execute|CommandConsumer' -v
```

Expected: FAIL because the service currently only publishes to `workflow.events.execution` and does not consume NATS execute commands.

**Step 3: Implement the API contract change**

- Route REST and NATS execute commands through one shared execution-start function.
- Populate the required CloudEvents extensions that already belong to Phase 2: `workflowid`, `executionid`, and `producer`.
- Publish `workflow.runtime.execution.started.v1` instead of the old `workflow.events.execution` subject.

**Step 4: Re-run the targeted tests**

Run:

```bash
go test ./internal/dsl/api ./internal/api -run 'Execute|CommandConsumer' -v
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/dsl/api/handler.go cmd/workflow-api/main.go internal/api/command_consumer.go internal/dsl/api/handler_test.go internal/api/command_consumer_test.go
git commit -m "feat(workflow-api): bridge execute requests to workflow v2 runtime events"
```

### Task 4: Migrate Orchestrator, Scheduler, and Worker Subjects With a Compatibility Bridge

**Files:**
- Modify: `internal/engine/events.go`
- Modify: `internal/engine/orchestrator.go`
- Modify: `internal/scheduler/scheduler.go`
- Modify: `internal/scheduler/dispatcher.go`
- Modify: `internal/worker/worker.go`
- Create: `internal/messaging/compat_bridge.go`
- Test: `internal/engine/orchestrator_test.go`
- Test: `internal/scheduler/router_test.go`

**Step 1: Write the failing tests**

- Update runtime tests to assert publishers/subscribers use the v2 subjects and event types.
- Add compatibility-bridge tests that confirm legacy messages can still be translated during cutover.

**Step 2: Run the targeted tests**

Run:

```bash
go test ./internal/engine ./internal/scheduler ./internal/worker ./internal/messaging -v
```

Expected: FAIL because runtime components still use `orchestration.*` event types and legacy subjects.

**Step 3: Implement the runtime migration**

- Change orchestrator output to `workflow.runtime.node.scheduled.v1`.
- Change scheduler dispatch to `workflow.command.node.execute.<nodeType>.v<version>` and worker results to `workflow.event.node.executed.v1`.
- Change scheduler-to-orchestrator handoff to `workflow.runtime.node.executed.v1`.
- Add a short-lived compatibility bridge or feature flag so old subjects can be accepted until Phase 4 retires the legacy entrypoints.

**Step 4: Re-run the targeted tests**

Run:

```bash
go test ./internal/engine ./internal/scheduler ./internal/worker ./internal/messaging -v
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/engine/events.go internal/engine/orchestrator.go internal/scheduler/scheduler.go internal/scheduler/dispatcher.go internal/worker/worker.go internal/messaging/compat_bridge.go internal/engine/orchestrator_test.go internal/scheduler/router_test.go
git commit -m "refactor(runtime): migrate orchestrator scheduler and workers to workflow v2 subjects"
```

