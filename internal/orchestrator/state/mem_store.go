package state

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/kimitsu-ai/ktsu/pkg/types"
)

var (
	errRunNotFound      = errors.New("run not found")
	errRunAlreadyExists = errors.New("run already exists")
	errStepNotFound     = errors.New("step not found")
	errStepAlreadyExists = errors.New("step already exists")
)

// MemStore is an in-memory implementation of Store, intended for testing.
type MemStore struct {
	mu    sync.RWMutex
	runs  map[string]*types.Run
	steps map[string]map[string]*types.Step // runID → stepID → Step
}

// NewMemStore returns a new, empty MemStore.
func NewMemStore() *MemStore {
	return &MemStore{
		runs:  make(map[string]*types.Run),
		steps: make(map[string]map[string]*types.Step),
	}
}

// copyRun returns a shallow copy of r.
func copyRun(r *types.Run) *types.Run {
	cp := *r
	return &cp
}

// copyStep returns a shallow copy of s.
func copyStep(s *types.Step) *types.Step {
	cp := *s
	return &cp
}

// CreateRun stores a copy of the run. Returns an error if the ID already exists.
func (m *MemStore) CreateRun(_ context.Context, run *types.Run) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.runs[run.ID]; exists {
		return errRunAlreadyExists
	}
	m.runs[run.ID] = copyRun(run)
	return nil
}

// UpdateRun replaces the stored run with a copy of the provided value.
// Returns an error if the run does not exist.
func (m *MemStore) UpdateRun(_ context.Context, run *types.Run) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.runs[run.ID]; !exists {
		return errRunNotFound
	}
	m.runs[run.ID] = copyRun(run)
	return nil
}

// GetRun returns a copy of the stored run. Returns an error if not found.
func (m *MemStore) GetRun(_ context.Context, runID string) (*types.Run, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	r, exists := m.runs[runID]
	if !exists {
		return nil, errRunNotFound
	}
	return copyRun(r), nil
}

// CreateStep stores a copy of the step under steps[runID][stepID].
// Returns an error if the step already exists.
func (m *MemStore) CreateStep(_ context.Context, step *types.Step) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.steps[step.RunID]; !ok {
		m.steps[step.RunID] = make(map[string]*types.Step)
	}
	if _, exists := m.steps[step.RunID][step.ID]; exists {
		return errStepAlreadyExists
	}
	m.steps[step.RunID][step.ID] = copyStep(step)
	return nil
}

// UpdateStep replaces the stored step with a copy of the provided value.
// Returns an error if the step does not exist.
func (m *MemStore) UpdateStep(_ context.Context, step *types.Step) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	runSteps, ok := m.steps[step.RunID]
	if !ok {
		return errStepNotFound
	}
	if _, exists := runSteps[step.ID]; !exists {
		return errStepNotFound
	}
	m.steps[step.RunID][step.ID] = copyStep(step)
	return nil
}

// GetStep returns a copy of the stored step. Returns an error if not found.
func (m *MemStore) GetStep(_ context.Context, runID, stepID string) (*types.Step, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	runSteps, ok := m.steps[runID]
	if !ok {
		return nil, errStepNotFound
	}
	s, exists := runSteps[stepID]
	if !exists {
		return nil, errStepNotFound
	}
	return copyStep(s), nil
}

// GetEnvelope builds a *types.Envelope on-the-fly from all completed or skipped
// steps for the given run. Returns an error if the run does not exist.
func (m *MemStore) GetEnvelope(_ context.Context, runID string) (*types.Envelope, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	run, exists := m.runs[runID]
	if !exists {
		return nil, errRunNotFound
	}

	env := &types.Envelope{
		RunID:    runID,
		Workflow: run.WorkflowName,
		Steps:    make(map[string]types.StepOutput),
	}

	for _, step := range m.steps[runID] {
		if step.Status != types.StepStatusComplete && step.Status != types.StepStatusSkipped {
			continue
		}

		output := step.Output
		if output == nil {
			output = make(map[string]interface{})
		}

		var durationMS int64
		if step.StartedAt != nil && step.EndedAt != nil {
			durationMS = step.EndedAt.Sub(*step.StartedAt).Milliseconds()
		}

		var ts time.Time
		if step.EndedAt != nil {
			ts = *step.EndedAt
		}

		env.Steps[step.ID] = types.StepOutput{
			Output:    output,
			Metrics:   types.StepMetrics{DurationMS: durationMS},
			Timestamp: ts,
		}
	}

	return env, nil
}
