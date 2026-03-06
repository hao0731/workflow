# Workflow V2 Phase 1 Runtime Model Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Align the workflow runtime model and workflow definition persistence with `docs/new-technical-design.md` before changing messaging behavior.

**Architecture:** Keep the existing `dsl -> engine -> scheduler` flow, but stop collapsing custom node identities and persist workflow definition metadata as first-class MongoDB fields. This phase is intentionally storage- and model-focused so later messaging work can reuse a stable runtime contract.

**Tech Stack:** Go 1.25, Echo, MongoDB, testify, CloudEvents

### Task 1: Preserve Custom Node Identity End-to-End

**Files:**
- Modify: `internal/engine/workflow.go`
- Modify: `internal/dsl/converter.go`
- Modify: `internal/scheduler/scheduler.go`
- Modify: `internal/registry/models.go`
- Test: `internal/dsl/converter_test.go`
- Test: `internal/scheduler/router_test.go`

**Step 1: Write the failing tests**

- Add a converter test that parses a DSL node with type `http-request@v1` and asserts the runtime model preserves that exact dispatch identity.
- Add a scheduler-focused test that ensures dispatch subject selection and node validation use the preserved versioned identity instead of the coarse `ActionNode` bucket.

**Step 2: Run the targeted tests to verify they fail**

Run:

```bash
go test ./internal/dsl ./internal/scheduler -run 'TestDefaultConverter_Convert|TestEventRouter' -v
```

Expected: at least one assertion fails because custom nodes are still normalized to `ActionNode`.

**Step 3: Implement the runtime-model change**

- Add an explicit field to `engine.Node` for dispatch identity, for example `TypeRef` or `FullType`, and keep `Type` only for orchestration semantics.
- Update the DSL converter so built-in nodes produce `Type == TypeRef`, while custom nodes keep `Type == ActionNode` and store `TypeRef == <nodeType>@<version>`.
- Update scheduler validation and dispatch to prefer the versioned identity field.
- Update registry subject helpers only if needed to keep `<nodeType>@<version>` and NATS subject generation consistent.

**Step 4: Re-run the targeted tests**

Run:

```bash
go test ./internal/dsl ./internal/scheduler -run 'TestDefaultConverter_Convert|TestEventRouter' -v
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/engine/workflow.go internal/dsl/converter.go internal/scheduler/scheduler.go internal/registry/models.go internal/dsl/converter_test.go internal/scheduler/router_test.go
git commit -m "refactor(workflow): preserve versioned node identity in runtime model"
```

### Task 2: Expand Workflow Definition Storage Schema in MongoDB

**Files:**
- Modify: `internal/dsl/models.go`
- Modify: `internal/dsl/store.go`
- Modify: `internal/dsl/mongo_store.go`
- Modify: `internal/dsl/registry.go`
- Test: `internal/dsl/mongo_store_test.go`

**Step 1: Write the failing tests**

- Add store tests that expect MongoDB documents to include workflow metadata fields from the DSL: `name`, `description`, `version`, `events`, and `source`.
- Add a retrieval test that asserts the store can reconstruct both the runtime workflow and its persisted metadata.

**Step 2: Run the store tests**

Run:

```bash
go test ./internal/dsl -run 'TestMongoWorkflowStore' -v
```

Expected: FAIL because the current document schema only persists `_id`, `source`, `nodes`, and `connections`.

**Step 3: Implement the persistence update**

- Extend the stored MongoDB document to persist metadata required by `docs/new-technical-design.md`.
- Introduce a store-level record type or metadata response so callers do not need to reverse-engineer the raw source every time.
- Keep backward compatibility for existing documents where practical by treating absent fields as zero values.
- Add or update indexes needed for `_id`, `updated_at`, and future event-trigger lookup.

**Step 4: Re-run the store tests**

Run:

```bash
go test ./internal/dsl -run 'TestMongoWorkflowStore' -v
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/dsl/models.go internal/dsl/store.go internal/dsl/mongo_store.go internal/dsl/registry.go internal/dsl/mongo_store_test.go
git commit -m "feat(workflow): persist workflow definition metadata in mongodb"
```

### Task 3: Align Workflow API Read/Write Responses With the Stored Definition Model

**Files:**
- Modify: `internal/dsl/api/handler.go`
- Modify: `cmd/workflow-api/main.go`
- Test: `internal/dsl/api/handler_test.go`
- Modify: `README.md`

**Step 1: Write the failing handler tests**

- Add coverage for `GET /api/workflows/:id/source` so it must return real `source`, `name`, `description`, `version`, and trigger stats.
- Add coverage for create/update flows so persisted DSL metadata and events remain round-trippable through the API.

**Step 2: Run the handler tests**

Run:

```bash
go test ./internal/dsl/api -run 'TestWorkflowHandler_(Create|Update|GetSource)' -v
```

Expected: FAIL because the source endpoint currently returns empty metadata fields.

**Step 3: Implement the API alignment**

- Update the handler to use the richer workflow definition record produced by the store.
- Ensure create/update still validate DSL before persistence, but now return metadata consistent with the persisted document.
- Keep `cmd/workflow-api` bootstrap compatible with the richer store contract.
- Update `README.md` examples if any response shape or supported endpoints changed materially.

**Step 4: Re-run the handler tests**

Run:

```bash
go test ./internal/dsl/api -run 'TestWorkflowHandler_(Create|Update|GetSource)' -v
```

Expected: PASS.

**Step 5: Commit**

```bash
git add internal/dsl/api/handler.go cmd/workflow-api/main.go internal/dsl/api/handler_test.go README.md
git commit -m "feat(workflow-api): expose persisted workflow definition metadata"
```

