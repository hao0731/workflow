package dsl

import (
	"context"
	"errors"
	"sync"

	"github.com/cheriehsieh/orchestration/internal/engine"
)

// workflowEntry stores a workflow and its original source.
type workflowEntry struct {
	workflow *engine.Workflow
	source   []byte
}

// InMemoryWorkflowStore provides in-memory storage for workflows.
type InMemoryWorkflowStore struct {
	mu        sync.RWMutex
	workflows map[string]*workflowEntry
}

// NewInMemoryWorkflowStore creates a new in-memory workflow store.
func NewInMemoryWorkflowStore() *InMemoryWorkflowStore {
	return &InMemoryWorkflowStore{
		workflows: make(map[string]*workflowEntry),
	}
}

func (s *InMemoryWorkflowStore) Register(ctx context.Context, wf *engine.Workflow, source []byte) error {
	if wf == nil {
		return errors.New("workflow is nil")
	}
	if wf.ID == "" {
		return errors.New("workflow ID is required")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	s.workflows[wf.ID] = &workflowEntry{workflow: wf, source: source}
	return nil
}

func (s *InMemoryWorkflowStore) GetByID(ctx context.Context, id string) (*engine.Workflow, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entry, ok := s.workflows[id]
	if !ok {
		return nil, ErrWorkflowNotFound
	}
	return entry.workflow, nil
}

func (s *InMemoryWorkflowStore) GetSource(ctx context.Context, id string) ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entry, ok := s.workflows[id]
	if !ok {
		return nil, ErrWorkflowNotFound
	}
	return entry.source, nil
}

func (s *InMemoryWorkflowStore) List(ctx context.Context) ([]string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	ids := make([]string, 0, len(s.workflows))
	for id := range s.workflows {
		ids = append(ids, id)
	}
	return ids, nil
}

func (s *InMemoryWorkflowStore) Delete(ctx context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.workflows[id]; !ok {
		return ErrWorkflowNotFound
	}
	delete(s.workflows, id)
	return nil
}

// FindByEventTrigger finds workflows with StartNode triggers matching the event.
func (s *InMemoryWorkflowStore) FindByEventTrigger(ctx context.Context, eventName, domain string) ([]*engine.Workflow, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var matches []*engine.Workflow
	for _, entry := range s.workflows {
		wf := entry.workflow
		start := wf.GetStartNode()
		if start == nil {
			continue
		}
		evName, evDomain, ok := start.GetEventTrigger()
		if !ok {
			continue
		}
		if evName == eventName && evDomain == domain {
			matches = append(matches, wf)
		}
	}
	return matches, nil
}
