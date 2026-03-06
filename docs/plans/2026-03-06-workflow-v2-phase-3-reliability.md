# Workflow V2 Phase 3 Reliability Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add the reliability features that the new design requires: idempotency, dedup storage, schema validation, DLQ routing, and durable join wait state.

**Architecture:** Extend the current Cassandra-backed `EventStore` instead of replacing it. Reliability work should build on the Phase 2 contract so the scheduler validates the same `workflow.event.node.executed.v1` messages that workers publish.

**Tech Stack:** Go 1.25, Cassandra, NATS JetStream, MongoDB, CloudEvents, testify

### Task 1: Extend EventStore With Dedup APIs and Cassandra Tables

**Files:**
- Modify: `internal/eventstore/eventstore.go`
- Modify: `internal/eventstore/cassandra.go`
- Modify: `internal/eventstore/cassandra_test.go`
- Modify: `scripts/cassandra-init.cql`
- Modify: `docs/new-technical-design.md`

**Step 1: Write the failing tests**

- Add Cassandra event store tests that expect `ExistsByDedupKey` and `SaveDedupRecord`.
- Add schema assertions for a dedicated `dedup_keys` table and, if needed, an execution-event table layout closer to the new design.

**Step 2: Run the targeted tests**

Run:

```bash
go test ./internal/eventstore -run 'TestCassandraEventStore' -v
```

Expected: FAIL because the interface and Cassandra adapter do not expose dedup operations yet.

**Step 3: Implement the EventStore change**

- Extend `EventStore` with dedup read/write methods.
- Update the Cassandra adapter and init script with a `dedup_keys` table that supports `IF NOT EXISTS` writes and TTL.
- Keep existing append/read behavior working so callers can migrate incrementally.

**Step 4: Re-run the targeted tests**

Run:

```bash
go test ./internal/eventstore -run 'TestCassandraEventStore' -v
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/eventstore/eventstore.go internal/eventstore/cassandra.go internal/eventstore/cassandra_test.go scripts/cassandra-init.cql docs/new-technical-design.md
git commit -m "feat(eventstore): add cassandra-backed dedup record support"
```

### Task 2: Add Idempotent Publish and Command Handling

**Files:**
- Modify: `internal/eventbus/nats.go`
- Modify: `internal/dsl/api/handler.go`
- Modify: `internal/api/command_consumer.go`
- Modify: `internal/scheduler/scheduler.go`
- Test: `internal/dsl/api/handler_test.go`
- Test: `internal/scheduler/link_handler_test.go` or create new scheduler dedup tests

**Step 1: Write the failing tests**

- Add tests for REST execute and NATS execute command paths that reject or short-circuit duplicates by dedup key.
- Add scheduler tests that ignore duplicate node results for the same `executionid + nodeid + runindex + attempt + idempotencykey`.

**Step 2: Run the targeted tests**

Run:

```bash
go test ./internal/dsl/api ./internal/scheduler -run 'Idempot|Dedup' -v
```

Expected: FAIL because the current code neither stores dedup keys nor sets `Nats-Msg-Id`.

**Step 3: Implement idempotent handling**

- Set `Nats-Msg-Id` from CloudEvent `id` for every publish path.
- Generate and persist business dedup keys in `workflow-api` and scheduler.
- Return the existing execution ID for duplicate execute commands where possible instead of starting a second run.

**Step 4: Re-run the targeted tests**

Run:

```bash
go test ./internal/dsl/api ./internal/scheduler -run 'Idempot|Dedup' -v
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/eventbus/nats.go internal/dsl/api/handler.go internal/api/command_consumer.go internal/scheduler/scheduler.go internal/dsl/api/handler_test.go internal/scheduler/link_handler_test.go
git commit -m "feat(runtime): add idempotent command and node-result handling"
```

### Task 3: Add Schema Registry Client and Cached Lookup

**Files:**
- Create: `internal/schema/client.go`
- Create: `internal/schema/cache.go`
- Create: `internal/schema/client_test.go`
- Modify: `cmd/workflow-api/main.go`
- Modify: `cmd/engine/main.go`

**Step 1: Write the failing tests**

- Add tests for schema lookup by event type and version.
- Add cache tests for TTL behavior and refresh on version or ETag change.

**Step 2: Run the schema tests**

Run:

```bash
go test ./internal/schema -v
```

Expected: FAIL because the package does not exist yet.

**Step 3: Implement the schema client**

- Create a lightweight client abstraction for `GET /schemas/events/{eventType}/versions/{version}`.
- Add in-memory TTL caching so scheduler validation does not hit the registry for every result event.
- Wire configuration into the service bootstrap without forcing scheduler validation to be enabled before Task 4.

**Step 4: Re-run the schema tests**

Run:

```bash
go test ./internal/schema -v
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/schema/client.go internal/schema/cache.go internal/schema/client_test.go cmd/workflow-api/main.go cmd/engine/main.go
git commit -m "feat(schema): add cached workflow schema registry client"
```

### Task 4: Validate Worker Results and Publish Validation Failures to DLQ

**Files:**
- Modify: `internal/scheduler/scheduler.go`
- Modify: `internal/scheduler/validator.go`
- Create: `internal/scheduler/dlq.go`
- Test: `internal/scheduler/router_test.go`
- Test: `internal/scheduler/link_handler_test.go` or create `internal/scheduler/scheduler_validation_test.go`

**Step 1: Write the failing tests**

- Add scheduler tests that reject invalid `workflow.event.node.executed.v1` payloads and publish `workflow.dlq.scheduler.validation.v1`.
- Add coverage for ACK-on-DLQ behavior so invalid events do not loop forever.

**Step 2: Run the scheduler tests**

Run:

```bash
go test ./internal/scheduler -run 'Validation|DLQ' -v
```

Expected: FAIL because the scheduler currently forwards worker results without schema validation.

**Step 3: Implement validation and DLQ**

- Use the schema client from Task 3 to validate node results before orchestration handoff.
- Publish invalid events to the DLQ plane with enough context for replay and diagnosis.
- Keep validation isolated in scheduler so workers stay thin.

**Step 4: Re-run the scheduler tests**

Run:

```bash
go test ./internal/scheduler -run 'Validation|DLQ' -v
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/scheduler/scheduler.go internal/scheduler/validator.go internal/scheduler/dlq.go internal/scheduler/router_test.go internal/scheduler/scheduler_validation_test.go
git commit -m "feat(scheduler): validate node results and route invalid payloads to dlq"
```

### Task 5: Replace In-Memory Join State With a Durable Store

**Files:**
- Modify: `internal/engine/join.go`
- Create: `internal/engine/join_store_mongo.go` or `internal/engine/join_store_cassandra.go`
- Modify: `cmd/engine/main.go`
- Test: `internal/engine/orchestrator_test.go`
- Test: `internal/engine/join_store_test.go`

**Step 1: Write the failing tests**

- Add tests that restart or recreate the join manager and assert pending join state can be recovered.
- Add optimistic-concurrency tests for concurrent predecessor completion.

**Step 2: Run the engine tests**

Run:

```bash
go test ./internal/engine -run 'Join|Orchestrator' -v
```

Expected: FAIL because the current join state store is process-local only.

**Step 3: Implement the durable join store**

- Keep `JoinStateStore` as the abstraction, but provide a durable adapter.
- Wire the durable store into the service bootstrap instead of `NewInMemoryJoinStateStore`.
- Preserve the current `JoinStateManager` concurrency semantics.

**Step 4: Re-run the engine tests**

Run:

```bash
go test ./internal/engine -run 'Join|Orchestrator' -v
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/engine/join.go internal/engine/join_store_mongo.go cmd/engine/main.go internal/engine/orchestrator_test.go internal/engine/join_store_test.go
git commit -m "refactor(orchestrator): persist join wait state in durable storage"
```

