# Case Engine Layer Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add a CMMN-inspired Case Engine layer to support dynamic, knowledge-worker-driven processes (e.g., approval flows where approvers can request additional documents, delegate, or add sub-approvals at runtime).

**Architecture:** Cases orchestrate workflow instances. The Case Engine sits above the existing Workflow Engine, managing plan items (discretionary tasks) that knowledge workers can activate at any point. When a discretionary task is activated, the Case Engine spawns and waits for a child workflow execution.

**Tech Stack:** Go 1.25+, MongoDB, NATS JetStream, Echo v4, CloudEvents v2

---

## Task 1: Case Data Models

**Files:**
- Create: `internal/caseengine/models.go`
- Create: `internal/caseengine/events.go`
- Test: `internal/caseengine/models_test.go`

**Step 1: Write the failing test for CaseDefinition**

```go
// internal/caseengine/models_test.go
package caseengine

import (
	"testing"
	"github.com/stretchr/testify/assert"
)

func TestCaseDefinition_GetPlanItem(t *testing.T) {
	def := CaseDefinition{
		ID:   "approval-case",
		Name: "Approval Case",
		PlanItems: []PlanItem{
			{ID: "request-docs", Name: "Request Documents", Discretionary: true},
			{ID: "approve", Name: "Approve", Discretionary: false},
		},
	}
	
	item := def.GetPlanItem("request-docs")
	assert.NotNil(t, item)
	assert.Equal(t, "Request Documents", item.Name)
	assert.True(t, item.Discretionary)
	
	missing := def.GetPlanItem("nonexistent")
	assert.Nil(t, missing)
}

func TestCaseInstance_GetActiveDiscretionaryTasks(t *testing.T) {
	instance := CaseInstance{
		ID:           "case-123",
		DefinitionID: "approval-case",
		State:        CaseStateActive,
		PlanItemStates: map[string]PlanItemState{
			"request-docs":   PlanItemStateAvailable,
			"add-approver":   PlanItemStateAvailable,
			"approve":        PlanItemStateActive,
		},
	}
	
	definition := CaseDefinition{
		PlanItems: []PlanItem{
			{ID: "request-docs", Discretionary: true},
			{ID: "add-approver", Discretionary: true},
			{ID: "approve", Discretionary: false},
		},
	}
	
	available := instance.GetAvailableDiscretionaryItems(definition)
	assert.Len(t, available, 2)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/caseengine/... -v -run "TestCaseDefinition|TestCaseInstance"`
Expected: FAIL with "package not found"

**Step 3: Write minimal implementation**

```go
// internal/caseengine/models.go
package caseengine

import "time"

// CaseState represents the lifecycle state of a case instance.
type CaseState string

const (
	CaseStateActive    CaseState = "active"
	CaseStateSuspended CaseState = "suspended"
	CaseStateCompleted CaseState = "completed"
	CaseStateClosed    CaseState = "closed"
)

// PlanItemState represents the lifecycle state of a plan item.
type PlanItemState string

const (
	PlanItemStateAvailable PlanItemState = "available"   // Can be activated
	PlanItemStateEnabled   PlanItemState = "enabled"     // Ready to start (sentry satisfied)
	PlanItemStateActive    PlanItemState = "active"      // Currently executing
	PlanItemStateCompleted PlanItemState = "completed"   // Finished successfully
	PlanItemStateFailed    PlanItemState = "failed"      // Finished with error
	PlanItemStateDisabled  PlanItemState = "disabled"    // Cannot be activated
)

// PlanItemType defines the kind of plan item.
type PlanItemType string

const (
	PlanItemTypeHumanTask   PlanItemType = "HumanTask"
	PlanItemTypeProcessTask PlanItemType = "ProcessTask" // Spawns a workflow
	PlanItemTypeCaseTask    PlanItemType = "CaseTask"    // Spawns a sub-case
	PlanItemTypeMilestone   PlanItemType = "Milestone"
	PlanItemTypeStage       PlanItemType = "Stage"
)

// PlanItem is a task or stage within a case definition.
type PlanItem struct {
	ID            string            `json:"id" bson:"id"`
	Type          PlanItemType      `json:"type" bson:"type"`
	Name          string            `json:"name" bson:"name"`
	Discretionary bool              `json:"discretionary" bson:"discretionary"`
	WorkflowID    string            `json:"workflow_id,omitempty" bson:"workflow_id,omitempty"` // For ProcessTask
	Parameters    map[string]any    `json:"parameters,omitempty" bson:"parameters,omitempty"`
	EntrySentry   *Sentry           `json:"entry_sentry,omitempty" bson:"entry_sentry,omitempty"`
	ExitSentry    *Sentry           `json:"exit_sentry,omitempty" bson:"exit_sentry,omitempty"`
}

// Sentry defines entry/exit criteria (event + condition).
type Sentry struct {
	OnEvent   string `json:"on_event,omitempty" bson:"on_event,omitempty"`     // e.g., "document.uploaded"
	Condition string `json:"condition,omitempty" bson:"condition,omitempty"`   // Expression to evaluate
}

// CaseDefinition is the template for a case.
type CaseDefinition struct {
	ID          string     `json:"id" bson:"_id"`
	Name        string     `json:"name" bson:"name"`
	Description string     `json:"description,omitempty" bson:"description,omitempty"`
	Version     string     `json:"version,omitempty" bson:"version,omitempty"`
	PlanItems   []PlanItem `json:"plan_items" bson:"plan_items"`
	CreatedAt   time.Time  `json:"created_at" bson:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at" bson:"updated_at"`
}

// GetPlanItem returns a plan item by ID.
func (d *CaseDefinition) GetPlanItem(id string) *PlanItem {
	for i := range d.PlanItems {
		if d.PlanItems[i].ID == id {
			return &d.PlanItems[i]
		}
	}
	return nil
}

// CaseInstance is a running instance of a case.
type CaseInstance struct {
	ID              string                   `json:"id" bson:"_id"`
	DefinitionID    string                   `json:"definition_id" bson:"definition_id"`
	State           CaseState                `json:"state" bson:"state"`
	PlanItemStates  map[string]PlanItemState `json:"plan_item_states" bson:"plan_item_states"`
	Data            map[string]any           `json:"data,omitempty" bson:"data,omitempty"` // Case variables
	ChildExecutions map[string]string        `json:"child_executions,omitempty" bson:"child_executions,omitempty"` // planItemID -> executionID
	CreatedAt       time.Time                `json:"created_at" bson:"created_at"`
	UpdatedAt       time.Time                `json:"updated_at" bson:"updated_at"`
}

// GetAvailableDiscretionaryItems returns plan items that can be activated.
func (c *CaseInstance) GetAvailableDiscretionaryItems(def CaseDefinition) []PlanItem {
	var items []PlanItem
	for _, item := range def.PlanItems {
		if item.Discretionary && c.PlanItemStates[item.ID] == PlanItemStateAvailable {
			items = append(items, item)
		}
	}
	return items
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/caseengine/... -v -run "TestCaseDefinition|TestCaseInstance"`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/caseengine/
git commit -m "feat(caseengine): add case data models"
```

---

## Task 2: Case Event Types

**Files:**
- Modify: `internal/caseengine/events.go`
- Test: `internal/caseengine/events_test.go`

**Step 1: Write the failing test for event construction**

```go
// internal/caseengine/events_test.go
package caseengine

import (
	"testing"
	"github.com/stretchr/testify/assert"
)

func TestNewCaseStartedEvent(t *testing.T) {
	data := CaseStartedData{
		DefinitionID: "approval-case",
		InputData:    map[string]any{"request_id": "req-123"},
	}
	
	assert.Equal(t, "approval-case", data.DefinitionID)
	assert.NotEmpty(t, CaseStarted)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/caseengine/... -v -run TestNewCaseStartedEvent`
Expected: FAIL

**Step 3: Write minimal implementation**

```go
// internal/caseengine/events.go
package caseengine

// Event types for case execution (CloudEvents type field).
const (
	CaseStarted              = "orchestration.case.started"
	CaseSuspended            = "orchestration.case.suspended"
	CaseResumed              = "orchestration.case.resumed"
	CaseCompleted            = "orchestration.case.completed"
	CaseClosed               = "orchestration.case.closed"
	PlanItemActivated        = "orchestration.case.planitem.activated"
	PlanItemCompleted        = "orchestration.case.planitem.completed"
	PlanItemFailed           = "orchestration.case.planitem.failed"
	DiscretionaryItemAdded   = "orchestration.case.discretionary.added"
)

// CaseStartedData is the data payload for CaseStarted events.
type CaseStartedData struct {
	DefinitionID string         `json:"definition_id"`
	InputData    map[string]any `json:"input_data,omitempty"`
}

// PlanItemActivatedData is the data payload when a plan item is activated.
type PlanItemActivatedData struct {
	PlanItemID  string         `json:"plan_item_id"`
	ActivatedBy string         `json:"activated_by,omitempty"` // User who activated (for discretionary)
	InputData   map[string]any `json:"input_data,omitempty"`
}

// PlanItemCompletedData is the data payload when a plan item completes.
type PlanItemCompletedData struct {
	PlanItemID  string         `json:"plan_item_id"`
	ExecutionID string         `json:"execution_id,omitempty"` // If ProcessTask
	OutputData  map[string]any `json:"output_data,omitempty"`
}

// DiscretionaryItemAddedData is the data payload for dynamically added items.
type DiscretionaryItemAddedData struct {
	PlanItem    PlanItem `json:"plan_item"`
	AddedBy     string   `json:"added_by"`
	ParentItem  string   `json:"parent_item,omitempty"` // The item requesting this addition
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/caseengine/... -v -run TestNewCaseStartedEvent`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/caseengine/events.go internal/caseengine/events_test.go
git commit -m "feat(caseengine): add case event types"
```

---

## Task 3: Case Store Interface and MongoDB Implementation

**Files:**
- Create: `internal/caseengine/store.go`
- Create: `internal/caseengine/mongo_store.go`
- Test: `internal/caseengine/mongo_store_test.go`

**Step 1: Write the failing test**

```go
// internal/caseengine/mongo_store_test.go
package caseengine

import (
	"context"
	"testing"
	"time"
	
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func setupTestDB(t *testing.T) (*mongo.Database, func()) {
	ctx := context.Background()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI("mongodb://localhost:27017"))
	require.NoError(t, err)
	
	db := client.Database("test_caseengine_" + time.Now().Format("20060102150405"))
	
	return db, func() {
		_ = db.Drop(ctx)
		_ = client.Disconnect(ctx)
	}
}

func TestCaseStore_CreateAndGetDefinition(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	
	store := NewMongoCaseStore(db)
	ctx := context.Background()
	
	def := &CaseDefinition{
		ID:   "approval-case",
		Name: "Approval Case",
		PlanItems: []PlanItem{
			{ID: "request-docs", Type: PlanItemTypeHumanTask, Discretionary: true},
		},
	}
	
	err := store.CreateDefinition(ctx, def)
	require.NoError(t, err)
	
	retrieved, err := store.GetDefinition(ctx, "approval-case")
	require.NoError(t, err)
	assert.Equal(t, "Approval Case", retrieved.Name)
	assert.Len(t, retrieved.PlanItems, 1)
}

func TestCaseStore_CreateAndGetInstance(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()
	
	store := NewMongoCaseStore(db)
	ctx := context.Background()
	
	instance := &CaseInstance{
		ID:           "case-123",
		DefinitionID: "approval-case",
		State:        CaseStateActive,
		PlanItemStates: map[string]PlanItemState{
			"request-docs": PlanItemStateAvailable,
		},
	}
	
	err := store.CreateInstance(ctx, instance)
	require.NoError(t, err)
	
	retrieved, err := store.GetInstance(ctx, "case-123")
	require.NoError(t, err)
	assert.Equal(t, CaseStateActive, retrieved.State)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/caseengine/... -v -run TestCaseStore`
Expected: FAIL

**Step 3: Write the interface and implementation**

```go
// internal/caseengine/store.go
package caseengine

import "context"

// CaseStore handles persistence for case definitions and instances.
type CaseStore interface {
	// Definition operations
	CreateDefinition(ctx context.Context, def *CaseDefinition) error
	GetDefinition(ctx context.Context, id string) (*CaseDefinition, error)
	ListDefinitions(ctx context.Context) ([]CaseDefinition, error)
	UpdateDefinition(ctx context.Context, def *CaseDefinition) error
	DeleteDefinition(ctx context.Context, id string) error
	
	// Instance operations
	CreateInstance(ctx context.Context, instance *CaseInstance) error
	GetInstance(ctx context.Context, id string) (*CaseInstance, error)
	UpdateInstance(ctx context.Context, instance *CaseInstance) error
	ListInstancesByDefinition(ctx context.Context, definitionID string) ([]CaseInstance, error)
}
```

```go
// internal/caseengine/mongo_store.go
package caseengine

import (
	"context"
	"errors"
	"time"
	
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

var (
	ErrDefinitionNotFound = errors.New("case definition not found")
	ErrInstanceNotFound   = errors.New("case instance not found")
)

type MongoCaseStore struct {
	definitions *mongo.Collection
	instances   *mongo.Collection
}

func NewMongoCaseStore(db *mongo.Database) *MongoCaseStore {
	return &MongoCaseStore{
		definitions: db.Collection("case_definitions"),
		instances:   db.Collection("case_instances"),
	}
}

func (s *MongoCaseStore) CreateDefinition(ctx context.Context, def *CaseDefinition) error {
	now := time.Now()
	def.CreatedAt = now
	def.UpdatedAt = now
	_, err := s.definitions.InsertOne(ctx, def)
	return err
}

func (s *MongoCaseStore) GetDefinition(ctx context.Context, id string) (*CaseDefinition, error) {
	var def CaseDefinition
	err := s.definitions.FindOne(ctx, bson.M{"_id": id}).Decode(&def)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrDefinitionNotFound
	}
	return &def, err
}

func (s *MongoCaseStore) ListDefinitions(ctx context.Context) ([]CaseDefinition, error) {
	cursor, err := s.definitions.Find(ctx, bson.M{})
	if err != nil {
		return nil, err
	}
	var defs []CaseDefinition
	err = cursor.All(ctx, &defs)
	return defs, err
}

func (s *MongoCaseStore) UpdateDefinition(ctx context.Context, def *CaseDefinition) error {
	def.UpdatedAt = time.Now()
	_, err := s.definitions.ReplaceOne(ctx, bson.M{"_id": def.ID}, def)
	return err
}

func (s *MongoCaseStore) DeleteDefinition(ctx context.Context, id string) error {
	_, err := s.definitions.DeleteOne(ctx, bson.M{"_id": id})
	return err
}

func (s *MongoCaseStore) CreateInstance(ctx context.Context, instance *CaseInstance) error {
	now := time.Now()
	instance.CreatedAt = now
	instance.UpdatedAt = now
	_, err := s.instances.InsertOne(ctx, instance)
	return err
}

func (s *MongoCaseStore) GetInstance(ctx context.Context, id string) (*CaseInstance, error) {
	var instance CaseInstance
	err := s.instances.FindOne(ctx, bson.M{"_id": id}).Decode(&instance)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrInstanceNotFound
	}
	return &instance, err
}

func (s *MongoCaseStore) UpdateInstance(ctx context.Context, instance *CaseInstance) error {
	instance.UpdatedAt = time.Now()
	_, err := s.instances.ReplaceOne(ctx, bson.M{"_id": instance.ID}, instance)
	return err
}

func (s *MongoCaseStore) ListInstancesByDefinition(ctx context.Context, definitionID string) ([]CaseInstance, error) {
	cursor, err := s.instances.Find(ctx, bson.M{"definition_id": definitionID})
	if err != nil {
		return nil, err
	}
	var instances []CaseInstance
	err = cursor.All(ctx, &instances)
	return instances, err
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/caseengine/... -v -run TestCaseStore`
Expected: PASS (requires MongoDB running)

**Step 5: Commit**

```bash
git add internal/caseengine/store.go internal/caseengine/mongo_store.go internal/caseengine/mongo_store_test.go
git commit -m "feat(caseengine): add case store with MongoDB implementation"
```

---

## Task 4: Case Orchestrator Core

**Files:**
- Create: `internal/caseengine/orchestrator.go`
- Test: `internal/caseengine/orchestrator_test.go`

**Step 1: Write the failing test**

```go
// internal/caseengine/orchestrator_test.go
package caseengine

import (
	"context"
	"testing"
	
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type MockCaseStore struct {
	mock.Mock
}

func (m *MockCaseStore) GetDefinition(ctx context.Context, id string) (*CaseDefinition, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*CaseDefinition), args.Error(1)
}

// ... other mock methods

func TestCaseOrchestrator_StartCase(t *testing.T) {
	store := new(MockCaseStore)
	orch := NewCaseOrchestrator(store, nil, nil)
	
	def := &CaseDefinition{
		ID:   "approval-case",
		Name: "Approval Case",
		PlanItems: []PlanItem{
			{ID: "initial-review", Type: PlanItemTypeHumanTask, Discretionary: false},
			{ID: "request-docs", Type: PlanItemTypeHumanTask, Discretionary: true},
		},
	}
	
	store.On("GetDefinition", mock.Anything, "approval-case").Return(def, nil)
	store.On("CreateInstance", mock.Anything, mock.AnythingOfType("*caseengine.CaseInstance")).Return(nil)
	
	instance, err := orch.StartCase(context.Background(), "approval-case", map[string]any{"request_id": "123"})
	require.NoError(t, err)
	
	assert.Equal(t, CaseStateActive, instance.State)
	assert.Equal(t, PlanItemStateEnabled, instance.PlanItemStates["initial-review"]) // Non-discretionary auto-enabled
	assert.Equal(t, PlanItemStateAvailable, instance.PlanItemStates["request-docs"]) // Discretionary stays available
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/caseengine/... -v -run TestCaseOrchestrator`
Expected: FAIL

**Step 3: Write minimal implementation**

```go
// internal/caseengine/orchestrator.go
package caseengine

import (
	"context"
	"log/slog"
	
	"github.com/google/uuid"
	
	"github.com/cheriehsieh/orchestration/internal/eventbus"
	"github.com/cheriehsieh/orchestration/internal/eventstore"
)

// CaseOrchestrator manages case lifecycle and plan item activation.
type CaseOrchestrator struct {
	store      CaseStore
	eventStore eventstore.EventStore
	eventBus   eventbus.Publisher
	logger     *slog.Logger
}

func NewCaseOrchestrator(
	store CaseStore,
	eventStore eventstore.EventStore,
	eventBus eventbus.Publisher,
	opts ...func(*CaseOrchestrator),
) *CaseOrchestrator {
	o := &CaseOrchestrator{
		store:      store,
		eventStore: eventStore,
		eventBus:   eventBus,
		logger:     slog.Default(),
	}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// StartCase creates a new case instance from a definition.
func (o *CaseOrchestrator) StartCase(ctx context.Context, definitionID string, input map[string]any) (*CaseInstance, error) {
	def, err := o.store.GetDefinition(ctx, definitionID)
	if err != nil {
		return nil, err
	}
	
	instance := &CaseInstance{
		ID:              uuid.NewString(),
		DefinitionID:    definitionID,
		State:           CaseStateActive,
		PlanItemStates:  make(map[string]PlanItemState),
		Data:            input,
		ChildExecutions: make(map[string]string),
	}
	
	// Initialize plan item states
	for _, item := range def.PlanItems {
		if item.Discretionary {
			instance.PlanItemStates[item.ID] = PlanItemStateAvailable
		} else {
			// Non-discretionary items are automatically enabled
			instance.PlanItemStates[item.ID] = PlanItemStateEnabled
		}
	}
	
	if err := o.store.CreateInstance(ctx, instance); err != nil {
		return nil, err
	}
	
	return instance, nil
}

// ActivateDiscretionaryItem activates a discretionary plan item.
func (o *CaseOrchestrator) ActivateDiscretionaryItem(
	ctx context.Context,
	caseID, planItemID, activatedBy string,
	input map[string]any,
) error {
	instance, err := o.store.GetInstance(ctx, caseID)
	if err != nil {
		return err
	}
	
	def, err := o.store.GetDefinition(ctx, instance.DefinitionID)
	if err != nil {
		return err
	}
	
	item := def.GetPlanItem(planItemID)
	if item == nil {
		return ErrPlanItemNotFound
	}
	
	if !item.Discretionary {
		return ErrNotDiscretionary
	}
	
	if instance.PlanItemStates[planItemID] != PlanItemStateAvailable {
		return ErrItemNotAvailable
	}
	
	instance.PlanItemStates[planItemID] = PlanItemStateActive
	
	// If ProcessTask, spawn workflow execution
	if item.Type == PlanItemTypeProcessTask && item.WorkflowID != "" {
		// TODO: Integrate with workflow engine to spawn execution
		// Store the executionID in instance.ChildExecutions[planItemID]
	}
	
	return o.store.UpdateInstance(ctx, instance)
}

// Errors
var (
	ErrPlanItemNotFound = errors.New("plan item not found")
	ErrNotDiscretionary = errors.New("plan item is not discretionary")
	ErrItemNotAvailable = errors.New("plan item is not available for activation")
)
```

Note: Add `"errors"` to imports.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/caseengine/... -v -run TestCaseOrchestrator`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/caseengine/orchestrator.go internal/caseengine/orchestrator_test.go
git commit -m "feat(caseengine): add case orchestrator core"
```

---

## Task 5: Case REST API

**Files:**
- Create: `internal/caseengine/api/handler.go`
- Test: `internal/caseengine/api/handler_test.go`

**Step 1: Write the failing test**

```go
// internal/caseengine/api/handler_test.go
package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

func TestCaseHandler_StartCase(t *testing.T) {
	e := echo.New()
	body := `{"definition_id":"approval-case","input":{"request_id":"123"}}`
	req := httptest.NewRequest(http.MethodPost, "/cases", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	
	mockOrch := new(MockOrchestrator)
	mockOrch.On("StartCase", mock.Anything, "approval-case", mock.Anything).
		Return(&caseengine.CaseInstance{ID: "case-123", State: caseengine.CaseStateActive}, nil)
	
	h := NewCaseHandler(mockOrch, nil)
	
	err := h.StartCase(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusCreated, rec.Code)
	assert.Contains(t, rec.Body.String(), "case-123")
}

func TestCaseHandler_ActivateDiscretionaryItem(t *testing.T) {
	e := echo.New()
	body := `{"activated_by":"user-456","input":{}}`
	req := httptest.NewRequest(http.MethodPost, "/cases/case-123/items/request-docs/activate", strings.NewReader(body))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	rec := httptest.NewRecorder()
	c := e.NewContext(req, rec)
	c.SetParamNames("id", "itemId")
	c.SetParamValues("case-123", "request-docs")
	
	mockOrch := new(MockOrchestrator)
	mockOrch.On("ActivateDiscretionaryItem", mock.Anything, "case-123", "request-docs", "user-456", mock.Anything).
		Return(nil)
	
	h := NewCaseHandler(mockOrch, nil)
	
	err := h.ActivateDiscretionaryItem(c)
	assert.NoError(t, err)
	assert.Equal(t, http.StatusOK, rec.Code)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/caseengine/api/... -v`
Expected: FAIL

**Step 3: Write minimal implementation**

```go
// internal/caseengine/api/handler.go
package api

import (
	"log/slog"
	"net/http"
	
	"github.com/labstack/echo/v4"
	
	"github.com/cheriehsieh/orchestration/internal/caseengine"
)

// CaseHandler handles HTTP requests for case management.
type CaseHandler struct {
	orchestrator *caseengine.CaseOrchestrator
	logger       *slog.Logger
}

func NewCaseHandler(orch *caseengine.CaseOrchestrator, logger *slog.Logger) *CaseHandler {
	if logger == nil {
		logger = slog.Default()
	}
	return &CaseHandler{orchestrator: orch, logger: logger}
}

// RegisterRoutes registers case API routes.
func (h *CaseHandler) RegisterRoutes(g *echo.Group) {
	g.POST("", h.StartCase)
	g.GET("/:id", h.GetCase)
	g.GET("/:id/items", h.GetAvailableItems)
	g.POST("/:id/items/:itemId/activate", h.ActivateDiscretionaryItem)
}

// StartCaseRequest is the request body for starting a case.
type StartCaseRequest struct {
	DefinitionID string         `json:"definition_id"`
	Input        map[string]any `json:"input"`
}

// CaseResponse is the API response for a case instance.
type CaseResponse struct {
	ID           string                              `json:"id"`
	DefinitionID string                              `json:"definition_id"`
	State        caseengine.CaseState                `json:"state"`
	ItemStates   map[string]caseengine.PlanItemState `json:"item_states"`
}

// StartCase handles POST /cases
func (h *CaseHandler) StartCase(c echo.Context) error {
	var req StartCaseRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	
	instance, err := h.orchestrator.StartCase(c.Request().Context(), req.DefinitionID, req.Input)
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	
	return c.JSON(http.StatusCreated, CaseResponse{
		ID:           instance.ID,
		DefinitionID: instance.DefinitionID,
		State:        instance.State,
		ItemStates:   instance.PlanItemStates,
	})
}

// ActivateItemRequest is the request body for activating an item.
type ActivateItemRequest struct {
	ActivatedBy string         `json:"activated_by"`
	Input       map[string]any `json:"input"`
}

// ActivateDiscretionaryItem handles POST /cases/:id/items/:itemId/activate
func (h *CaseHandler) ActivateDiscretionaryItem(c echo.Context) error {
	caseID := c.Param("id")
	itemID := c.Param("itemId")
	
	var req ActivateItemRequest
	if err := c.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	
	err := h.orchestrator.ActivateDiscretionaryItem(
		c.Request().Context(),
		caseID,
		itemID,
		req.ActivatedBy,
		req.Input,
	)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}
	
	return c.JSON(http.StatusOK, map[string]string{"status": "activated"})
}

// GetCase handles GET /cases/:id
func (h *CaseHandler) GetCase(c echo.Context) error {
	// TODO: Implement
	return nil
}

// GetAvailableItems handles GET /cases/:id/items
func (h *CaseHandler) GetAvailableItems(c echo.Context) error {
	// TODO: Implement - returns discretionary items that can be activated
	return nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/caseengine/api/... -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/caseengine/api/
git commit -m "feat(caseengine): add case REST API handler"
```

---

## Task 6: Case-Workflow Integration (ProcessTask Execution)

**Files:**
- Modify: `internal/caseengine/orchestrator.go`
- Test: `internal/caseengine/orchestrator_integration_test.go`

**Step 1: Write the failing test**

```go
// internal/caseengine/orchestrator_integration_test.go
package caseengine

import (
	"context"
	"testing"
	
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type MockWorkflowExecutor struct {
	mock.Mock
}

func (m *MockWorkflowExecutor) Execute(ctx context.Context, workflowID string, input map[string]any) (string, error) {
	args := m.Called(ctx, workflowID, input)
	return args.String(0), args.Error(1)
}

func TestCaseOrchestrator_ActivateProcessTask_SpawnsWorkflow(t *testing.T) {
	store := new(MockCaseStore)
	executor := new(MockWorkflowExecutor)
	
	orch := NewCaseOrchestrator(store, nil, nil, WithWorkflowExecutor(executor))
	
	def := &CaseDefinition{
		ID: "approval-case",
		PlanItems: []PlanItem{
			{
				ID:            "request-approval",
				Type:          PlanItemTypeProcessTask,
				Discretionary: true,
				WorkflowID:    "approval-workflow",
			},
		},
	}
	
	instance := &CaseInstance{
		ID:           "case-123",
		DefinitionID: "approval-case",
		State:        CaseStateActive,
		PlanItemStates: map[string]PlanItemState{
			"request-approval": PlanItemStateAvailable,
		},
		ChildExecutions: make(map[string]string),
	}
	
	store.On("GetInstance", mock.Anything, "case-123").Return(instance, nil)
	store.On("GetDefinition", mock.Anything, "approval-case").Return(def, nil)
	store.On("UpdateInstance", mock.Anything, mock.AnythingOfType("*caseengine.CaseInstance")).Return(nil)
	executor.On("Execute", mock.Anything, "approval-workflow", mock.Anything).Return("exec-456", nil)
	
	err := orch.ActivateDiscretionaryItem(context.Background(), "case-123", "request-approval", "user-1", nil)
	require.NoError(t, err)
	
	executor.AssertCalled(t, "Execute", mock.Anything, "approval-workflow", mock.Anything)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/caseengine/... -v -run TestCaseOrchestrator_ActivateProcessTask`
Expected: FAIL

**Step 3: Add WorkflowExecutor integration**

```go
// Add to internal/caseengine/orchestrator.go

// WorkflowExecutor interface for spawning workflow executions.
type WorkflowExecutor interface {
	Execute(ctx context.Context, workflowID string, input map[string]any) (executionID string, err error)
}

// Add field to CaseOrchestrator struct:
// workflowExecutor WorkflowExecutor

// Add functional option:
func WithWorkflowExecutor(exec WorkflowExecutor) func(*CaseOrchestrator) {
	return func(o *CaseOrchestrator) {
		o.workflowExecutor = exec
	}
}

// Update ActivateDiscretionaryItem to spawn workflow:
// Inside the ProcessTask handling:
if item.Type == PlanItemTypeProcessTask && item.WorkflowID != "" {
	if o.workflowExecutor != nil {
		execID, err := o.workflowExecutor.Execute(ctx, item.WorkflowID, input)
		if err != nil {
			return err
		}
		instance.ChildExecutions[planItemID] = execID
	}
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/caseengine/... -v -run TestCaseOrchestrator_ActivateProcessTask`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/caseengine/
git commit -m "feat(caseengine): integrate workflow execution for ProcessTask items"
```

---

## Task 7: Case DSL Extension

**Files:**
- Create: `internal/caseengine/dsl/models.go`
- Create: `internal/caseengine/dsl/parser.go`
- Test: `internal/caseengine/dsl/parser_test.go`

**Step 1: Write the failing test**

```go
// internal/caseengine/dsl/parser_test.go
package dsl

import (
	"testing"
	
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseCaseYAML(t *testing.T) {
	yaml := `
id: approval-case
name: Approval Case
description: Dynamic approval with discretionary actions

plan_items:
  - id: initial-review
    type: HumanTask
    name: Initial Review
    discretionary: false

  - id: request-documents
    type: HumanTask
    name: Request Additional Documents
    discretionary: true

  - id: add-approver
    type: ProcessTask
    name: Add Additional Approver
    discretionary: true
    workflow_id: additional-approval-workflow

  - id: final-decision
    type: HumanTask
    name: Final Decision
    discretionary: false
    entry_sentry:
      on_event: initial-review.completed
`
	
	def, err := ParseCaseYAML([]byte(yaml))
	require.NoError(t, err)
	
	assert.Equal(t, "approval-case", def.ID)
	assert.Len(t, def.PlanItems, 4)
	
	// Check discretionary items
	reqDocs := def.GetPlanItem("request-documents")
	assert.True(t, reqDocs.Discretionary)
	
	// Check ProcessTask with workflow
	addApprover := def.GetPlanItem("add-approver")
	assert.Equal(t, "additional-approval-workflow", addApprover.WorkflowID)
	
	// Check sentry
	finalDecision := def.GetPlanItem("final-decision")
	assert.NotNil(t, finalDecision.EntrySentry)
	assert.Equal(t, "initial-review.completed", finalDecision.EntrySentry.OnEvent)
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/caseengine/dsl/... -v`
Expected: FAIL

**Step 3: Write minimal implementation**

```go
// internal/caseengine/dsl/models.go
package dsl

import "github.com/cheriehsieh/orchestration/internal/caseengine"

// CaseDefinitionYAML is the YAML representation of a case definition.
type CaseDefinitionYAML struct {
	ID          string         `yaml:"id"`
	Name        string         `yaml:"name"`
	Description string         `yaml:"description,omitempty"`
	Version     string         `yaml:"version,omitempty"`
	PlanItems   []PlanItemYAML `yaml:"plan_items"`
}

// PlanItemYAML is the YAML representation of a plan item.
type PlanItemYAML struct {
	ID            string      `yaml:"id"`
	Type          string      `yaml:"type"`
	Name          string      `yaml:"name"`
	Discretionary bool        `yaml:"discretionary"`
	WorkflowID    string      `yaml:"workflow_id,omitempty"`
	Parameters    map[string]any `yaml:"parameters,omitempty"`
	EntrySentry   *SentryYAML `yaml:"entry_sentry,omitempty"`
	ExitSentry    *SentryYAML `yaml:"exit_sentry,omitempty"`
}

// SentryYAML is the YAML representation of a sentry.
type SentryYAML struct {
	OnEvent   string `yaml:"on_event,omitempty"`
	Condition string `yaml:"condition,omitempty"`
}
```

```go
// internal/caseengine/dsl/parser.go
package dsl

import (
	"gopkg.in/yaml.v3"
	
	"github.com/cheriehsieh/orchestration/internal/caseengine"
)

// ParseCaseYAML parses a YAML case definition.
func ParseCaseYAML(data []byte) (*caseengine.CaseDefinition, error) {
	var yamlDef CaseDefinitionYAML
	if err := yaml.Unmarshal(data, &yamlDef); err != nil {
		return nil, err
	}
	
	return convertToCaseDefinition(yamlDef), nil
}

func convertToCaseDefinition(y CaseDefinitionYAML) *caseengine.CaseDefinition {
	def := &caseengine.CaseDefinition{
		ID:          y.ID,
		Name:        y.Name,
		Description: y.Description,
		Version:     y.Version,
		PlanItems:   make([]caseengine.PlanItem, len(y.PlanItems)),
	}
	
	for i, item := range y.PlanItems {
		def.PlanItems[i] = caseengine.PlanItem{
			ID:            item.ID,
			Type:          caseengine.PlanItemType(item.Type),
			Name:          item.Name,
			Discretionary: item.Discretionary,
			WorkflowID:    item.WorkflowID,
			Parameters:    item.Parameters,
		}
		
		if item.EntrySentry != nil {
			def.PlanItems[i].EntrySentry = &caseengine.Sentry{
				OnEvent:   item.EntrySentry.OnEvent,
				Condition: item.EntrySentry.Condition,
			}
		}
		
		if item.ExitSentry != nil {
			def.PlanItems[i].ExitSentry = &caseengine.Sentry{
				OnEvent:   item.ExitSentry.OnEvent,
				Condition: item.ExitSentry.Condition,
			}
		}
	}
	
	return def
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/caseengine/dsl/... -v`
Expected: PASS

**Step 5: Commit**

```bash
git add internal/caseengine/dsl/
git commit -m "feat(caseengine): add YAML DSL parser for case definitions"
```

---

## Verification Plan

### Automated Tests

```bash
# Run all case engine tests
go test ./internal/caseengine/... -v -coverprofile=coverage.out

# Check coverage
go tool cover -func=coverage.out | grep total

# Run linter
golangci-lint run ./internal/caseengine/...
```

### Manual Verification

1. Start MongoDB and NATS locally
2. Start the engine with case API enabled
3. Create a case definition via API
4. Start a case instance
5. Verify discretionary items appear in GET /cases/:id/items
6. Activate a discretionary ProcessTask
7. Verify workflow execution is spawned

---

## Summary

| Task | Description | Effort |
|------|-------------|--------|
| 1 | Case Data Models | ~15 min |
| 2 | Case Event Types | ~10 min |
| 3 | MongoDB Store | ~20 min |
| 4 | Case Orchestrator Core | ~25 min |
| 5 | Case REST API | ~20 min |
| 6 | Workflow Integration | ~15 min |
| 7 | Case YAML DSL | ~20 min |

**Total Estimated Time:** ~2 hours

