# Workflow V2 Phase 4 Service Split Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Split the legacy monolithic engine composition into the service topology described in `docs/new-technical-design.md`, then retire obsolete entrypoints.

**Architecture:** Reuse the packages stabilized in Phases 1 through 3, but move service wiring out of `cmd/engine` into standalone binaries for `workflow-api`, `orchestrator`, `scheduler`, and first-party workers. The end state should make it obvious which process owns each subject and dependency.

**Tech Stack:** Go 1.25, Echo, NATS JetStream, MongoDB, Cassandra, Docker Compose

### Task 1: Extract Shared Bootstrap Helpers

**Files:**
- Create: `internal/bootstrap/config.go`
- Create: `internal/bootstrap/nats.go`
- Create: `internal/bootstrap/mongo.go`
- Create: `internal/bootstrap/cassandra.go`
- Modify: `cmd/workflow-api/main.go`
- Modify: `cmd/engine/main.go`
- Modify: `cmd/registry/main.go`
- Test: `internal/bootstrap/bootstrap_test.go`

**Step 1: Write the failing tests**

- Add bootstrap tests for connection setup and stream initialization helpers.
- Focus on deterministic helper behavior, not real external connections.

**Step 2: Run the bootstrap tests**

Run:

```bash
go test ./internal/bootstrap -v
```

Expected: FAIL because the package does not exist yet.

**Step 3: Implement the shared bootstrap package**

- Move repeated MongoDB, Cassandra, NATS, and logger wiring out of `cmd/*`.
- Keep the helpers small and side-effect-light so the later service split does not duplicate startup logic again.

**Step 4: Re-run the bootstrap tests**

Run:

```bash
go test ./internal/bootstrap -v
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/bootstrap/config.go internal/bootstrap/nats.go internal/bootstrap/mongo.go internal/bootstrap/cassandra.go internal/bootstrap/bootstrap_test.go cmd/workflow-api/main.go cmd/engine/main.go cmd/registry/main.go
git commit -m "refactor(bootstrap): extract shared service startup helpers"
```

### Task 2: Add a Standalone Orchestrator Service

**Files:**
- Create: `cmd/orchestrator/main.go`
- Modify: `internal/engine/orchestrator.go`
- Modify: `docker-compose.app.yaml`
- Test: `internal/engine/orchestrator_test.go`

**Step 1: Write the failing tests**

- Add tests or smoke-test hooks that assert the orchestrator can be started with only its required dependencies.
- Ensure the service subscribes only to runtime subjects it owns.

**Step 2: Run the orchestrator tests**

Run:

```bash
go test ./internal/engine -run 'Orchestrator' -v
```

Expected: FAIL or require code changes because startup is still coupled to `cmd/engine`.

**Step 3: Implement the standalone service**

- Create `cmd/orchestrator/main.go`.
- Wire Mongo workflow definitions, Cassandra event store, durable join state, NATS runtime subscribers, and publishers.
- Update Docker Compose so orchestrator can be run independently from scheduler and workers.

**Step 4: Re-run the orchestrator tests**

Run:

```bash
go test ./internal/engine -run 'Orchestrator' -v
```

Expected: PASS.

**Step 5: Commit**

```bash
git add cmd/orchestrator/main.go internal/engine/orchestrator.go docker-compose.app.yaml internal/engine/orchestrator_test.go
git commit -m "feat(orchestrator): add standalone workflow orchestrator service"
```

### Task 3: Add a Standalone Scheduler Service

**Files:**
- Create: `cmd/scheduler/main.go`
- Modify: `internal/scheduler/scheduler.go`
- Modify: `docker-compose.app.yaml`
- Test: `internal/scheduler/router_test.go`
- Test: `internal/scheduler/scheduler_validation_test.go`

**Step 1: Write the failing tests**

- Add scheduler service tests or startup seams that assert only scheduler-owned subjects are bound.
- Cover both node dispatch and node result validation wiring.

**Step 2: Run the scheduler tests**

Run:

```bash
go test ./internal/scheduler -v
```

Expected: FAIL or require refactoring because the runtime still assumes `cmd/engine` owns composition.

**Step 3: Implement the standalone service**

- Create `cmd/scheduler/main.go`.
- Wire runtime schedule consumption, worker command publishing, node-result validation, and DLQ publishing.
- Update Docker Compose to run scheduler independently.

**Step 4: Re-run the scheduler tests**

Run:

```bash
go test ./internal/scheduler -v
```

Expected: PASS.

**Step 5: Commit**

```bash
git add cmd/scheduler/main.go internal/scheduler/scheduler.go docker-compose.app.yaml internal/scheduler/router_test.go internal/scheduler/scheduler_validation_test.go
git commit -m "feat(scheduler): add standalone workflow scheduler service"
```

### Task 4: Move First-Party Workers Out of the Engine Process

**Files:**
- Create: `cmd/worker-firstparty/main.go`
- Modify: `internal/worker/worker.go`
- Modify: `internal/engine/publish.go`
- Modify: `docker-compose.app.yaml`
- Test: `internal/worker/worker_test.go` or create if missing

**Step 1: Write the failing tests**

- Add worker tests that assert built-in workers can be started independently and consume only their owned dispatch subjects.
- Cover `StartNode`, `JoinNode`, and `PublishEvent` first-party worker registration/wiring.

**Step 2: Run the worker tests**

Run:

```bash
go test ./internal/worker -v
```

Expected: FAIL or require implementation because built-in worker composition still lives inside `cmd/engine`.

**Step 3: Implement the first-party worker service**

- Create a dedicated binary that runs the repository-provided workers.
- Include `JoinNode` if it is part of the supported built-in node catalog.
- Remove first-party worker wiring from `cmd/engine`.

**Step 4: Re-run the worker tests**

Run:

```bash
go test ./internal/worker -v
```

Expected: PASS.

**Step 5: Commit**

```bash
git add cmd/worker-firstparty/main.go internal/worker/worker.go internal/engine/publish.go docker-compose.app.yaml internal/worker/worker_test.go
git commit -m "feat(worker): add standalone first-party worker service"
```

### Task 5: Retire Legacy Entrypoints and Finalize the Supported Runtime Topology

**Files:**
- Modify: `cmd/engine/main.go`
- Modify: `cmd/api/main.go`
- Modify: `README.md`
- Modify: `docker-compose.app.yaml`
- Modify: `docs/new-technical-design.md`

**Step 1: Write the failing verification checks**

- Add or update smoke-test commands in documentation/scripts so CI or developers can verify the supported services are `workflow-api`, `orchestrator`, `scheduler`, `worker-firstparty`, and `registry`.
- Add a minimal runtime startup check if the repo already has one; otherwise document the manual verification sequence.

**Step 2: Run the verification flow**

Run:

```bash
go test ./...
```

Expected: some failures or obsolete behavior until legacy entrypoints are retired or clearly marked unsupported.

**Step 3: Implement the cleanup**

- Remove or deprecate `cmd/api` as a supported path.
- Reduce `cmd/engine` to a thin compatibility wrapper or remove it entirely once no caller depends on it.
- Update Compose, README, and technical design docs so there is one unambiguous supported topology.

**Step 4: Re-run the full verification**

Run:

```bash
go test ./...
go build ./cmd/workflow-api ./cmd/orchestrator ./cmd/scheduler ./cmd/worker-firstparty ./cmd/registry
```

Expected: PASS, aside from environment-dependent integration skips.

**Step 5: Commit**

```bash
git add cmd/engine/main.go cmd/api/main.go README.md docker-compose.app.yaml docs/new-technical-design.md
git commit -m "chore(runtime): retire legacy entrypoints and finalize workflow v2 topology"
```
