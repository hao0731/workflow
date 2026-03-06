package engine

import (
	"context"
	"errors"
	"sync"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var (
	ErrJoinStateNotFound = errors.New("join state not found")
	ErrJoinStateConflict = errors.New("join state conflict (CAS failure)")
)

var _ JoinStateStore = (*MongoJoinStateStore)(nil)

// PendingJoin tracks the state of a join node waiting for inputs.
type PendingJoin struct {
	ExecutionID     string                    `json:"execution_id"`
	JoinNodeID      string                    `json:"join_node_id"`
	WorkflowID      string                    `json:"workflow_id"`
	RequiredNodes   []string                  `json:"required_nodes"`
	CompletedNodes  map[string]bool           `json:"completed_nodes"`
	CollectedInputs map[string]map[string]any `json:"collected_inputs"`
	Version         uint64                    `json:"version"` // For Optimistic Concurrency Control
}

// JoinStateStore defines the interface for storing pending join states.
type JoinStateStore interface {
	// Get retrieves the current state.
	Get(ctx context.Context, executionID, joinNodeID string) (*PendingJoin, error)
	// Save creates or updates the state. Must handle CAS using Version.
	Save(ctx context.Context, state *PendingJoin) error
	// Delete removes the state.
	Delete(ctx context.Context, executionID, joinNodeID string) error
}

// JoinStateManager handles business logic for join nodes.
type JoinStateManager struct {
	store JoinStateStore
}

func NewJoinStateManager(store JoinStateStore) *JoinStateManager {
	return &JoinStateManager{store: store}
}

// ProcessJoin handles a completion event for a join node.
// It loads state, updates it, and saves it. Returns true if join is complete.
func (m *JoinStateManager) ProcessJoin(ctx context.Context, executionID, joinNodeID, workflowID string, predecessors []string, fromNodeID, toPort string, outputData map[string]any) (bool, map[string]any, error) {
	// Simple retry loop for optimistic locking
	for i := 0; i < 3; i++ {
		state, err := m.store.Get(ctx, executionID, joinNodeID)
		if err != nil {
			if errors.Is(err, ErrJoinStateNotFound) {
				// Initialize new state
				state = &PendingJoin{
					ExecutionID:     executionID,
					JoinNodeID:      joinNodeID,
					WorkflowID:      workflowID,
					RequiredNodes:   predecessors,
					CompletedNodes:  make(map[string]bool),
					CollectedInputs: make(map[string]map[string]any),
					Version:         0,
				}
			} else {
				return false, nil, err
			}
		}

		// Update state
		state.CompletedNodes[fromNodeID] = true
		if toPort == "" {
			toPort = fromNodeID
		}
		if state.CollectedInputs == nil {
			state.CollectedInputs = make(map[string]map[string]any)
		}
		state.CollectedInputs[toPort] = outputData

		// Check if done
		allDone := true
		for _, req := range state.RequiredNodes {
			if !state.CompletedNodes[req] {
				allDone = false
				break
			}
		}

		// Save state
		if err := m.store.Save(ctx, state); err != nil {
			if errors.Is(err, ErrJoinStateConflict) {
				continue // Retry
			}
			return false, nil, err
		}

		if allDone {
			// Clean up
			_ = m.store.Delete(ctx, executionID, joinNodeID)

			inputs := make(map[string]any)
			for port, data := range state.CollectedInputs {
				inputs[port] = data
			}
			return true, inputs, nil
		}

		return false, nil, nil
	}
	return false, nil, ErrJoinStateConflict
}

// InMemoryJoinStateStore implements JoinStateStore using a map.
type InMemoryJoinStateStore struct {
	mu     sync.RWMutex
	states map[string]*PendingJoin
}

func NewInMemoryJoinStateStore() *InMemoryJoinStateStore {
	return &InMemoryJoinStateStore{
		states: make(map[string]*PendingJoin),
	}
}

func (s *InMemoryJoinStateStore) key(executionID, joinNodeID string) string {
	return executionID + ":" + joinNodeID
}

func (s *InMemoryJoinStateStore) Get(ctx context.Context, executionID, joinNodeID string) (*PendingJoin, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	state, ok := s.states[s.key(executionID, joinNodeID)]
	if !ok {
		return nil, ErrJoinStateNotFound
	}
	// Return copy to prevent external mutation affecting the store
	copy := *state
	return &copy, nil
}

func (s *InMemoryJoinStateStore) Save(ctx context.Context, state *PendingJoin) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	key := s.key(state.ExecutionID, state.JoinNodeID)
	existing, exists := s.states[key]

	if exists {
		if state.Version != existing.Version {
			return ErrJoinStateConflict
		}
	} else if state.Version != 0 {
		return ErrJoinStateConflict
	}

	// Store copy
	newState := *state
	newState.Version++
	s.states[key] = &newState
	state.Version = newState.Version // Update caller's version
	return nil
}

func (s *InMemoryJoinStateStore) Delete(ctx context.Context, executionID, joinNodeID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.states, s.key(executionID, joinNodeID))
	return nil
}

// FindToPort finds the ToPort for a connection from sourceNode to targetNode.
func FindToPort(workflow *Workflow, sourceNode, targetNode string) string {
	for _, c := range workflow.Connections {
		if c.FromNode == sourceNode && c.ToNode == targetNode {
			if c.ToPort != "" {
				return c.ToPort
			}
			return sourceNode
		}
	}
	return sourceNode
}

func IsJoinNode(n *Node) bool {
	return n != nil && n.Type == JoinNode
}

// MongoJoinStateStore persists join state with optimistic concurrency.
type MongoJoinStateStore struct {
	collection *mongo.Collection
}

// NewMongoJoinStateStore creates a MongoDB-backed join-state store.
func NewMongoJoinStateStore(db *mongo.Database, collectionName string) *MongoJoinStateStore {
	return &MongoJoinStateStore{
		collection: db.Collection(collectionName),
	}
}

func (s *MongoJoinStateStore) Get(ctx context.Context, executionID, joinNodeID string) (*PendingJoin, error) {
	var state PendingJoin
	err := s.collection.FindOne(ctx, bson.M{"_id": joinStateKey(executionID, joinNodeID)}).Decode(&state)
	if errors.Is(err, mongo.ErrNoDocuments) {
		return nil, ErrJoinStateNotFound
	}
	if err != nil {
		return nil, err
	}

	return &state, nil
}

func (s *MongoJoinStateStore) Save(ctx context.Context, state *PendingJoin) error {
	nextVersion := state.Version + 1
	replacement := bson.M{
		"_id":              joinStateKey(state.ExecutionID, state.JoinNodeID),
		"execution_id":     state.ExecutionID,
		"join_node_id":     state.JoinNodeID,
		"workflow_id":      state.WorkflowID,
		"required_nodes":   state.RequiredNodes,
		"completed_nodes":  state.CompletedNodes,
		"collected_inputs": state.CollectedInputs,
		"version":          nextVersion,
	}

	filter := bson.M{
		"_id":     joinStateKey(state.ExecutionID, state.JoinNodeID),
		"version": state.Version,
	}

	opts := options.Replace()
	if state.Version == 0 {
		opts.SetUpsert(true)
	}

	result, err := s.collection.ReplaceOne(ctx, filter, replacement, opts)
	if err != nil {
		if mongo.IsDuplicateKeyError(err) {
			return ErrJoinStateConflict
		}
		return err
	}

	if state.Version != 0 && result.MatchedCount == 0 {
		return ErrJoinStateConflict
	}

	state.Version = nextVersion
	return nil
}

func (s *MongoJoinStateStore) Delete(ctx context.Context, executionID, joinNodeID string) error {
	_, err := s.collection.DeleteOne(ctx, bson.M{"_id": joinStateKey(executionID, joinNodeID)})
	return err
}

func joinStateKey(executionID, joinNodeID string) string {
	return executionID + ":" + joinNodeID
}
