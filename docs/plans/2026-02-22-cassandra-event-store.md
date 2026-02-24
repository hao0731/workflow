# Cassandra Event Store Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Migrate event history storage from MongoDB-only to a hybrid Cassandra (writes) + MongoDB (read models) architecture.

**Architecture:** CQRS split — `CassandraEventStore` handles append + partition-key reads, `MongoEventStore` handles complex aggregated reads, and `HybridEventStore` composes both behind the existing `EventStore` interface. A `ProjectionConsumer` subscribes to NATS events to keep MongoDB read models in sync.

**Tech Stack:** Go 1.25, gocql, MongoDB, NATS JetStream, Docker Compose, testify

---

## Task 1: Add gocql Dependency

**Files:**
- Modify: `go.mod`

**Step 1: Install the gocql driver**

```bash
cd /Users/cheriehsieh/Program/orchestration
go get github.com/gocql/gocql
```

**Step 2: Verify the dependency was added**

```bash
grep gocql go.mod
```

Expected: `github.com/gocql/gocql v1.x.x`

**Step 3: Commit**

```bash
git add go.mod go.sum
git commit -m "chore: add gocql cassandra driver dependency"
```

---

## Task 2: Add Cassandra Configuration

**Files:**
- Modify: `internal/config/config.go`
- Modify: `.env` (if exists, otherwise create)

**Step 1: Add Cassandra fields to the Config struct**

Add these fields after the NATS block in `internal/config/config.go`:

```go
// Cassandra
CassandraHosts    string
CassandraKeyspace string
CassandraPort     int

// Migration
MigrationPhase string // "shadow", "read_switch", "cleanup"
```

**Step 2: Populate them in Load()**

Add after the `WorkflowStore` line in the `return &Config{...}` block:

```go
// Cassandra
CassandraHosts:    getEnv("CASSANDRA_HOSTS", "localhost"),
CassandraKeyspace: getEnv("CASSANDRA_KEYSPACE", "orchestration"),
CassandraPort:     getIntEnv("CASSANDRA_PORT", 9042),

// Migration
MigrationPhase: getEnv("MIGRATION_PHASE", "shadow"),
```

**Step 3: Verify it compiles**

```bash
cd /Users/cheriehsieh/Program/orchestration
go build ./...
```

Expected: No errors.

**Step 4: Commit**

```bash
git add internal/config/config.go
git commit -m "feat(config): add cassandra and migration phase configuration"
```

---

## Task 3: Add Cassandra to Docker Compose

**Files:**
- Modify: `docker-compose.yaml`

**Step 1: Add cassandra service and volume**

Add right after the `nats` service block:

```yaml
  cassandra:
    image: cassandra:4.1
    container_name: orchestration-cassandra
    ports:
      - "9042:9042"
    volumes:
      - cassandra_data:/var/lib/cassandra
    environment:
      CASSANDRA_CLUSTER_NAME: orchestration
      MAX_HEAP_SIZE: 512M
      HEAP_NEWSIZE: 128M
    healthcheck:
      test: ["CMD-SHELL", "cqlsh -e 'DESCRIBE KEYSPACES' || exit 1"]
      interval: 15s
      timeout: 10s
      retries: 10
```

Add `cassandra_data:` under the `volumes:` section.

**Step 2: Verify Cassandra starts**

```bash
cd /Users/cheriehsieh/Program/orchestration
docker compose up cassandra -d
docker compose logs cassandra --follow
```

Wait for `Startup complete` in the logs (takes ~30-60s). Then verify:

```bash
docker exec orchestration-cassandra cqlsh -e "DESCRIBE KEYSPACES"
```

Expected: Lists default keyspaces (system, system_auth, etc.)

**Step 3: Commit**

```bash
git add docker-compose.yaml
git commit -m "infra: add cassandra 4.1 to docker-compose"
```

---

## Task 4: Create Cassandra Schema Init Script

**Files:**
- Create: `scripts/cassandra-init.cql`

**Step 1: Write the schema file**

Create `scripts/cassandra-init.cql` with:

```cql
-- Create keyspace (RF=1 for dev, RF=3 for production)
CREATE KEYSPACE IF NOT EXISTS orchestration
  WITH replication = {'class': 'SimpleStrategy', 'replication_factor': 1};

-- Primary event table
CREATE TABLE IF NOT EXISTS orchestration.events (
    subject         TEXT,
    time            TIMESTAMP,
    id              TEXT,
    type            TEXT,
    source          TEXT,
    datacontenttype TEXT,
    data            TEXT,
    extensions      MAP<TEXT, TEXT>,
    PRIMARY KEY (subject, time)
) WITH CLUSTERING ORDER BY (time ASC)
  AND compaction = {'class': 'TimeWindowCompactionStrategy',
                    'compaction_window_size': 1,
                    'compaction_window_unit': 'DAYS'};
```

**Step 2: Apply the schema**

```bash
docker exec -i orchestration-cassandra cqlsh < scripts/cassandra-init.cql
```

**Step 3: Verify the table was created**

```bash
docker exec orchestration-cassandra cqlsh -e "DESCRIBE TABLE orchestration.events"
```

Expected: Shows the full table schema.

**Step 4: Commit**

```bash
git add scripts/cassandra-init.cql
git commit -m "feat: add cassandra schema init script"
```

---

## Task 5: Implement CassandraEventStore — Append

**Files:**
- Create: `internal/eventstore/cassandra.go`
- Create: `internal/eventstore/cassandra_test.go`

**Step 1: Write the failing test for Append**

Create `internal/eventstore/cassandra_test.go`:

```go
package eventstore

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/gocql/gocql"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestCassandra(t *testing.T) (*gocql.Session, func()) {
	t.Helper()
	cluster := gocql.NewCluster("localhost")
	cluster.Port = 9042
	cluster.Keyspace = "orchestration"
	cluster.Consistency = gocql.LocalQuorum

	session, err := cluster.CreateSession()
	if err != nil {
		t.Skipf("Cassandra not available: %v", err)
	}

	// Clean up before test
	_ = session.Query("TRUNCATE orchestration.events").Exec()

	return session, func() {
		session.Close()
	}
}

func TestCassandraEventStore_Append(t *testing.T) {
	session, cleanup := setupTestCassandra(t)
	defer cleanup()

	store := NewCassandraEventStore(session)
	ctx := context.Background()

	event := cloudevents.NewEvent()
	event.SetID("evt-001")
	event.SetSource("test")
	event.SetType("orchestration.execution.started")
	event.SetSubject("exec-123")
	event.SetTime(time.Now().Truncate(time.Millisecond))
	_ = event.SetData(cloudevents.ApplicationJSON, map[string]any{"workflow_id": "wf-1"})
	event.SetExtension("workflowid", "wf-1")

	err := store.Append(ctx, event)
	require.NoError(t, err)

	// Verify data was written by querying directly
	var id, typ, source, subject string
	err = session.Query(
		"SELECT id, type, source, subject FROM orchestration.events WHERE subject = ? LIMIT 1",
		"exec-123",
	).Scan(&id, &typ, &source, &subject)
	require.NoError(t, err)
	assert.Equal(t, "evt-001", id)
	assert.Equal(t, "orchestration.execution.started", typ)
	assert.Equal(t, "test", source)
}
```

**Step 2: Run the test to verify it fails**

```bash
cd /Users/cheriehsieh/Program/orchestration
go test ./internal/eventstore/ -run TestCassandraEventStore_Append -v
```

Expected: FAIL — `NewCassandraEventStore` undefined.

**Step 3: Write the implementation**

Create `internal/eventstore/cassandra.go`:

```go
package eventstore

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/gocql/gocql"
)

// Compile-time interface check (partial — GetExecutionsByWorkflow not supported).
var _ EventStore = (*CassandraEventStore)(nil)

// CassandraEventStore implements event persistence using Cassandra.
type CassandraEventStore struct {
	session *gocql.Session
}

// NewCassandraEventStore creates a new Cassandra-backed event store.
func NewCassandraEventStore(session *gocql.Session) *CassandraEventStore {
	return &CassandraEventStore{session: session}
}

func (s *CassandraEventStore) Append(ctx context.Context, event cloudevents.Event) error {
	stored := FromCloudEvent(event)

	dataJSON, err := json.Marshal(stored.Data)
	if err != nil {
		return fmt.Errorf("marshal event data: %w", err)
	}

	extensions := make(map[string]string)
	for k, v := range stored.Extensions {
		extensions[k] = fmt.Sprintf("%v", v)
	}

	return s.session.Query(
		`INSERT INTO orchestration.events
		 (subject, time, id, type, source, datacontenttype, data, extensions)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		stored.Subject,
		stored.Time,
		stored.ID,
		stored.Type,
		stored.Source,
		stored.DataContentType,
		string(dataJSON),
		extensions,
	).WithContext(ctx).Exec()
}

func (s *CassandraEventStore) GetBySubject(ctx context.Context, subject string) ([]cloudevents.Event, error) {
	return s.queryEvents(ctx, subject, nil)
}

func (s *CassandraEventStore) GetEventsByExecution(ctx context.Context, executionID string, since *time.Time) ([]cloudevents.Event, error) {
	return s.queryEvents(ctx, executionID, since)
}

func (s *CassandraEventStore) GetExecutionsByWorkflow(ctx context.Context, workflowID string) ([]ExecutionSummary, error) {
	return nil, fmt.Errorf("GetExecutionsByWorkflow not supported by CassandraEventStore: use HybridEventStore")
}

// queryEvents retrieves events by partition key (subject), optionally filtered by time.
func (s *CassandraEventStore) queryEvents(ctx context.Context, subject string, since *time.Time) ([]cloudevents.Event, error) {
	var query *gocql.Query
	if since != nil {
		query = s.session.Query(
			"SELECT subject, time, id, type, source, datacontenttype, data, extensions FROM orchestration.events WHERE subject = ? AND time > ?",
			subject, *since,
		)
	} else {
		query = s.session.Query(
			"SELECT subject, time, id, type, source, datacontenttype, data, extensions FROM orchestration.events WHERE subject = ?",
			subject,
		)
	}

	iter := query.WithContext(ctx).Iter()
	var events []cloudevents.Event

	var (
		sub, id, typ, source, dataContentType, dataJSON string
		ts                                              time.Time
		extensions                                      map[string]string
	)
	for iter.Scan(&sub, &ts, &id, &typ, &source, &dataContentType, &dataJSON, &extensions) {
		event := cloudevents.NewEvent()
		event.SetID(id)
		event.SetSource(source)
		event.SetType(typ)
		event.SetSubject(sub)
		event.SetTime(ts)

		var data map[string]any
		if err := json.Unmarshal([]byte(dataJSON), &data); err == nil {
			_ = event.SetData(cloudevents.ApplicationJSON, data)
		}

		for k, v := range extensions {
			event.SetExtension(k, v)
		}

		events = append(events, event)
	}

	if err := iter.Close(); err != nil {
		return nil, fmt.Errorf("cassandra query: %w", err)
	}

	return events, nil
}
```

**Step 4: Run test to verify it passes**

```bash
cd /Users/cheriehsieh/Program/orchestration
go test ./internal/eventstore/ -run TestCassandraEventStore_Append -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/eventstore/cassandra.go internal/eventstore/cassandra_test.go
git commit -m "feat(eventstore): implement CassandraEventStore.Append with test"
```

---

## Task 6: Test CassandraEventStore — GetBySubject & GetEventsByExecution

**Files:**
- Modify: `internal/eventstore/cassandra_test.go`

**Step 1: Write the failing test for GetBySubject**

Add to `cassandra_test.go`:

```go
func TestCassandraEventStore_GetBySubject(t *testing.T) {
	session, cleanup := setupTestCassandra(t)
	defer cleanup()

	store := NewCassandraEventStore(session)
	ctx := context.Background()

	// Insert two events for same subject
	for i, typ := range []string{"execution.started", "execution.completed"} {
		e := cloudevents.NewEvent()
		e.SetID(fmt.Sprintf("evt-%d", i))
		e.SetSource("test")
		e.SetType(typ)
		e.SetSubject("exec-get-subject")
		e.SetTime(time.Now().Add(time.Duration(i) * time.Second).Truncate(time.Millisecond))
		_ = e.SetData(cloudevents.ApplicationJSON, map[string]any{"i": i})
		require.NoError(t, store.Append(ctx, e))
	}

	events, err := store.GetBySubject(ctx, "exec-get-subject")
	require.NoError(t, err)
	assert.Len(t, events, 2)
	assert.Equal(t, "execution.started", events[0].Type())
	assert.Equal(t, "execution.completed", events[1].Type())
}
```

**Step 2: Write the failing test for GetEventsByExecution with since filter**

Add to `cassandra_test.go`:

```go
func TestCassandraEventStore_GetEventsByExecution(t *testing.T) {
	session, cleanup := setupTestCassandra(t)
	defer cleanup()

	store := NewCassandraEventStore(session)
	ctx := context.Background()

	base := time.Now().Truncate(time.Millisecond)
	for i := 0; i < 3; i++ {
		e := cloudevents.NewEvent()
		e.SetID(fmt.Sprintf("evt-exec-%d", i))
		e.SetSource("test")
		e.SetType("node.completed")
		e.SetSubject("exec-since-test")
		e.SetTime(base.Add(time.Duration(i) * time.Minute))
		_ = e.SetData(cloudevents.ApplicationJSON, map[string]any{"step": i})
		require.NoError(t, store.Append(ctx, e))
	}

	// Get events since 30 seconds after base — should return last 2
	since := base.Add(30 * time.Second)
	events, err := store.GetEventsByExecution(ctx, "exec-since-test", &since)
	require.NoError(t, err)
	assert.Len(t, events, 2)
}
```

**Step 3: Run tests**

```bash
cd /Users/cheriehsieh/Program/orchestration
go test ./internal/eventstore/ -run TestCassandraEventStore -v
```

Expected: All 3 Cassandra tests PASS.

**Step 4: Commit**

```bash
git add internal/eventstore/cassandra_test.go
git commit -m "test(eventstore): add GetBySubject and GetEventsByExecution cassandra tests"
```

---

## Task 7: Implement HybridEventStore

**Files:**
- Create: `internal/eventstore/hybrid.go`
- Create: `internal/eventstore/hybrid_test.go`

**Step 1: Write the failing test**

Create `internal/eventstore/hybrid_test.go`:

```go
package eventstore

import (
	"context"
	"testing"
	"time"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeEventStore is a minimal in-memory EventStore for unit testing.
type fakeEventStore struct {
	appended []cloudevents.Event
}

func (f *fakeEventStore) Append(_ context.Context, event cloudevents.Event) error {
	f.appended = append(f.appended, event)
	return nil
}
func (f *fakeEventStore) GetBySubject(_ context.Context, subject string) ([]cloudevents.Event, error) {
	var result []cloudevents.Event
	for _, e := range f.appended {
		if e.Subject() == subject {
			result = append(result, e)
		}
	}
	return result, nil
}
func (f *fakeEventStore) GetEventsByExecution(_ context.Context, execID string, since *time.Time) ([]cloudevents.Event, error) {
	var result []cloudevents.Event
	for _, e := range f.appended {
		if e.Subject() == execID {
			if since == nil || e.Time().After(*since) {
				result = append(result, e)
			}
		}
	}
	return result, nil
}
func (f *fakeEventStore) GetExecutionsByWorkflow(_ context.Context, workflowID string) ([]ExecutionSummary, error) {
	return []ExecutionSummary{{ID: "exec-from-mongo", WorkflowID: workflowID, Status: "running", StartedAt: time.Now()}}, nil
}

func TestHybridEventStore_Append_DelegatesToCassandra(t *testing.T) {
	cassandra := &fakeEventStore{}
	mongo := &fakeEventStore{}
	store := NewHybridEventStore(cassandra, mongo)

	event := cloudevents.NewEvent()
	event.SetID("e1")
	event.SetSource("test")
	event.SetType("test.event")
	event.SetSubject("exec-1")

	err := store.Append(context.Background(), event)
	require.NoError(t, err)
	assert.Len(t, cassandra.appended, 1, "should write to cassandra")
	assert.Len(t, mongo.appended, 0, "should NOT write to mongo")
}

func TestHybridEventStore_GetBySubject_ReadsFromCassandra(t *testing.T) {
	cassandra := &fakeEventStore{}
	mongo := &fakeEventStore{}
	store := NewHybridEventStore(cassandra, mongo)

	event := cloudevents.NewEvent()
	event.SetID("e1")
	event.SetSource("test")
	event.SetType("test.event")
	event.SetSubject("exec-1")
	cassandra.appended = append(cassandra.appended, event)

	events, err := store.GetBySubject(context.Background(), "exec-1")
	require.NoError(t, err)
	assert.Len(t, events, 1)
}

func TestHybridEventStore_GetExecutionsByWorkflow_ReadsFromMongo(t *testing.T) {
	cassandra := &fakeEventStore{}
	mongo := &fakeEventStore{}
	store := NewHybridEventStore(cassandra, mongo)

	summaries, err := store.GetExecutionsByWorkflow(context.Background(), "wf-1")
	require.NoError(t, err)
	assert.Len(t, summaries, 1)
	assert.Equal(t, "exec-from-mongo", summaries[0].ID)
}
```

**Step 2: Run test to verify it fails**

```bash
cd /Users/cheriehsieh/Program/orchestration
go test ./internal/eventstore/ -run TestHybridEventStore -v
```

Expected: FAIL — `NewHybridEventStore` undefined.

**Step 3: Write the implementation**

Create `internal/eventstore/hybrid.go`:

```go
package eventstore

import (
	"context"
	"time"

	cloudevents "github.com/cloudevents/sdk-go/v2"
)

// Compile-time interface compliance check.
var _ EventStore = (*HybridEventStore)(nil)

// HybridEventStore routes writes to Cassandra and complex reads to MongoDB.
type HybridEventStore struct {
	cassandra EventStore // append + partition-key reads
	mongo     EventStore // complex aggregated reads
}

// NewHybridEventStore creates a composite event store.
func NewHybridEventStore(cassandra, mongo EventStore) *HybridEventStore {
	return &HybridEventStore{cassandra: cassandra, mongo: mongo}
}

func (h *HybridEventStore) Append(ctx context.Context, event cloudevents.Event) error {
	return h.cassandra.Append(ctx, event)
}

func (h *HybridEventStore) GetBySubject(ctx context.Context, subject string) ([]cloudevents.Event, error) {
	return h.cassandra.GetBySubject(ctx, subject)
}

func (h *HybridEventStore) GetEventsByExecution(ctx context.Context, executionID string, since *time.Time) ([]cloudevents.Event, error) {
	return h.cassandra.GetEventsByExecution(ctx, executionID, since)
}

func (h *HybridEventStore) GetExecutionsByWorkflow(ctx context.Context, workflowID string) ([]ExecutionSummary, error) {
	return h.mongo.GetExecutionsByWorkflow(ctx, workflowID)
}
```

**Step 4: Run test to verify it passes**

```bash
cd /Users/cheriehsieh/Program/orchestration
go test ./internal/eventstore/ -run TestHybridEventStore -v
```

Expected: All 3 tests PASS.

**Step 5: Commit**

```bash
git add internal/eventstore/hybrid.go internal/eventstore/hybrid_test.go
git commit -m "feat(eventstore): implement HybridEventStore composite"
```

---

## Task 8: Implement ProjectionConsumer

**Files:**
- Create: `internal/eventstore/projection.go`
- Create: `internal/eventstore/projection_test.go`

**Step 1: Write the failing test**

Create `internal/eventstore/projection_test.go`:

```go
package eventstore

import (
	"context"
	"testing"
	"time"

	cloudevents "github.com/cloudevents/sdk-go/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProjectionConsumer_ProjectsEventToMongo(t *testing.T) {
	mongo := &fakeEventStore{}
	consumer := NewProjectionConsumer(mongo)

	event := cloudevents.NewEvent()
	event.SetID("proj-evt-1")
	event.SetSource("test")
	event.SetType("orchestration.execution.started")
	event.SetSubject("exec-proj-1")
	event.SetTime(time.Now().Truncate(time.Millisecond))
	_ = event.SetData(cloudevents.ApplicationJSON, map[string]any{"workflow_id": "wf-1"})

	err := consumer.HandleEvent(context.Background(), event)
	require.NoError(t, err)
	assert.Len(t, mongo.appended, 1)
	assert.Equal(t, "proj-evt-1", mongo.appended[0].ID())
}
```

**Step 2: Run test to verify it fails**

```bash
cd /Users/cheriehsieh/Program/orchestration
go test ./internal/eventstore/ -run TestProjectionConsumer -v
```

Expected: FAIL — `NewProjectionConsumer` undefined.

**Step 3: Write the implementation**

Create `internal/eventstore/projection.go`:

```go
package eventstore

import (
	"context"
	"log/slog"

	cloudevents "github.com/cloudevents/sdk-go/v2"
)

// ProjectionConsumer projects events from the event log into MongoDB read models.
type ProjectionConsumer struct {
	mongo  EventStore
	logger *slog.Logger
}

// NewProjectionConsumer creates a new projection consumer.
func NewProjectionConsumer(mongo EventStore, opts ...ProjectionOption) *ProjectionConsumer {
	p := &ProjectionConsumer{
		mongo:  mongo,
		logger: slog.Default(),
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// ProjectionOption configures the ProjectionConsumer.
type ProjectionOption func(*ProjectionConsumer)

// WithProjectionLogger sets the logger.
func WithProjectionLogger(logger *slog.Logger) ProjectionOption {
	return func(p *ProjectionConsumer) {
		p.logger = logger
	}
}

// HandleEvent processes a single event and projects it into MongoDB.
// This is the callback to be used with an eventbus.Subscriber.
func (p *ProjectionConsumer) HandleEvent(ctx context.Context, event cloudevents.Event) error {
	p.logger.InfoContext(ctx, "projecting event",
		slog.String("id", event.ID()),
		slog.String("type", event.Type()),
		slog.String("subject", event.Subject()),
	)

	// Append the raw event to MongoDB for read queries
	if err := p.mongo.Append(ctx, event); err != nil {
		p.logger.ErrorContext(ctx, "failed to project event",
			slog.String("id", event.ID()),
			slog.Any("error", err),
		)
		return err
	}

	return nil
}
```

**Step 4: Run test to verify it passes**

```bash
cd /Users/cheriehsieh/Program/orchestration
go test ./internal/eventstore/ -run TestProjectionConsumer -v
```

Expected: PASS

**Step 5: Commit**

```bash
git add internal/eventstore/projection.go internal/eventstore/projection_test.go
git commit -m "feat(eventstore): implement ProjectionConsumer for NATS-to-MongoDB projection"
```

---

## Task 9: Wire Cassandra Into main.go (Phase 1 — Shadow Write)

**Files:**
- Modify: `cmd/engine/main.go`

**Step 1: Add Cassandra connection after MongoDB connection**

Add after the `db := client.Database(cfg.MongoDatabase)` line (~line 45):

```go
	// 3b. Connect to Cassandra
	cassandraCluster := gocql.NewCluster(cfg.CassandraHosts)
	cassandraCluster.Port = cfg.CassandraPort
	cassandraCluster.Keyspace = cfg.CassandraKeyspace
	cassandraCluster.Consistency = gocql.LocalQuorum

	cassandraSession, err := cassandraCluster.CreateSession()
	if err != nil {
		logger.Error("failed to connect to Cassandra", slog.Any("error", err))
		os.Exit(1)
	}
	defer cassandraSession.Close()
```

**Step 2: Replace the event store initialization**

Replace line 145:

```diff
- eventStoreImpl := eventstore.NewMongoEventStore(db, "events")
+ // Hybrid Event Store: Cassandra for writes, MongoDB for read models
+ cassandraStore := eventstore.NewCassandraEventStore(cassandraSession)
+ mongoReadModel := eventstore.NewMongoEventStore(db, "events")
+ eventStoreImpl := eventstore.NewHybridEventStore(cassandraStore, mongoReadModel)
```

**Step 3: Add the ProjectionConsumer to the components list**

Add after the `linkHandler` initialization (~line 192):

```go
	// 9c. Initialize Projection Consumer (Cassandra → MongoDB)
	projectionSub := eventbus.NewNATSEventBus(js, "workflow.events.>", "projection-consumer", eventbus.WithLogger(logger))
	projectionConsumer := eventstore.NewProjectionConsumer(
		mongoReadModel,
		eventstore.WithProjectionLogger(logger),
	)
```

Note: The `ProjectionConsumer` needs a `Start` method to subscribe. Add this to the components list. We need to add a `Start` method to `ProjectionConsumer` first — see next step.

**Step 4: Add Start method to ProjectionConsumer**

Add to `internal/eventstore/projection.go`:

```go
// Subscriber is the interface for subscribing to events.
type Subscriber interface {
	Subscribe(ctx context.Context, handler func(context.Context, cloudevents.Event) error) error
}

// WithSubscriber sets the event subscriber.
func WithSubscriber(sub Subscriber) ProjectionOption {
	return func(p *ProjectionConsumer) {
		p.subscriber = sub
	}
}
```

Add `subscriber` to the struct:

```go
type ProjectionConsumer struct {
	mongo      EventStore
	subscriber Subscriber
	logger     *slog.Logger
}
```

Add the `Start` method:

```go
// Start begins listening for events and projecting them.
func (p *ProjectionConsumer) Start(ctx context.Context) error {
	if p.subscriber == nil {
		return fmt.Errorf("projection consumer: subscriber not configured")
	}
	return p.subscriber.Subscribe(ctx, p.HandleEvent)
}
```

**Step 5: Add the imports to main.go**

Add `"github.com/gocql/gocql"` to the import block.

**Step 6: Verify it compiles**

```bash
cd /Users/cheriehsieh/Program/orchestration
go build ./...
```

Expected: No errors.

**Step 7: Commit**

```bash
git add cmd/engine/main.go internal/eventstore/projection.go
git commit -m "feat: wire hybrid event store and projection consumer into main"
```

---

## Task 10: Update .env.example and README

**Files:**
- Create or Modify: `.env.example`
- Modify: `README.md` (if exists)

**Step 1: Add Cassandra env vars to .env.example**

```env
# Cassandra
CASSANDRA_HOSTS=localhost
CASSANDRA_KEYSPACE=orchestration
CASSANDRA_PORT=9042

# Migration Phase: shadow | read_switch | cleanup
MIGRATION_PHASE=shadow
```

**Step 2: Commit**

```bash
git add .env.example
git commit -m "docs: add cassandra env vars to .env.example"
```

---

## Task 11: Run Full Test Suite

**Files:** None (verification only)

**Step 1: Start all infrastructure**

```bash
cd /Users/cheriehsieh/Program/orchestration
docker compose up -d
```

Wait for all services to be healthy.

**Step 2: Apply Cassandra schema**

```bash
docker exec -i orchestration-cassandra cqlsh < scripts/cassandra-init.cql
```

**Step 3: Run all tests**

```bash
cd /Users/cheriehsieh/Program/orchestration
go test ./... -v
```

Expected: All tests pass. Cassandra tests may be skipped if Cassandra container is not reachable from the test runner (the `t.Skip` in `setupTestCassandra` handles this).

**Step 4: Run build verification**

```bash
cd /Users/cheriehsieh/Program/orchestration
go build ./...
go vet ./...
```

Expected: No errors, no warnings.
