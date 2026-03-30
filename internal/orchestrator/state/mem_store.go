package state

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/kimitsu-ai/ktsu/pkg/types"
)

var (
	errRunNotFound           = errors.New("run not found")
	errRunAlreadyExists      = errors.New("run already exists")
	errStepNotFound          = errors.New("step not found")
	errStepAlreadyExists     = errors.New("step already exists")
	errApprovalNotFound      = errors.New("approval not found")
	errApprovalAlreadyExists = errors.New("approval already exists")
)

// MemStore is an in-memory implementation of Store, intended for testing.
type MemStore struct {
	mu        sync.RWMutex
	runs      map[string]*types.Run
	steps     map[string]map[string]*types.Step // runID → stepID → Step
	approvals map[string]*types.Approval        // key: runID+"/"+stepID
}

// NewMemStore returns a new, empty MemStore.
func NewMemStore() *MemStore {
	return &MemStore{
		runs:      make(map[string]*types.Run),
		steps:     make(map[string]map[string]*types.Step),
		approvals: make(map[string]*types.Approval),
	}
}

// copyRun returns a deep copy of r, including its map fields.
func copyRun(r *types.Run) *types.Run {
	cp := *r
	if r.Metadata != nil {
		cp.Metadata = make(map[string]string, len(r.Metadata))
		for k, v := range r.Metadata {
			cp.Metadata[k] = v
		}
	}
	return &cp
}

// copyStep returns a deep copy of s, including its map fields.
func copyStep(s *types.Step) *types.Step {
	cp := *s
	if s.Output != nil {
		cp.Output = make(map[string]interface{}, len(s.Output))
		for k, v := range s.Output {
			cp.Output[k] = v
		}
	}
	if s.Messages != nil {
		cp.Messages = make(json.RawMessage, len(s.Messages))
		copy(cp.Messages, s.Messages)
	}
	cp.Metrics = s.Metrics
	return &cp
}

// copyApproval returns a deep copy of a.
func copyApproval(a *types.Approval) *types.Approval {
	cp := *a
	if a.Arguments != nil {
		cp.Arguments = make(map[string]any, len(a.Arguments))
		for k, v := range a.Arguments {
			cp.Arguments[k] = v
		}
	}
	if a.OriginalRequest != nil {
		cp.OriginalRequest = make(json.RawMessage, len(a.OriginalRequest))
		copy(cp.OriginalRequest, a.OriginalRequest)
	}
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

// ListSteps returns copies of all steps for the given run. Returns nil, nil if the run has no steps.
func (m *MemStore) ListSteps(_ context.Context, runID string) ([]*types.Step, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	runSteps, ok := m.steps[runID]
	if !ok {
		return nil, nil
	}
	out := make([]*types.Step, 0, len(runSteps))
	for _, s := range runSteps {
		out = append(out, copyStep(s))
	}
	return out, nil
}

// GetEnvelope builds a *types.Envelope on-the-fly from all finished steps
// (completed, skipped, or failed) for the given run. Returns an error if the
// run does not exist.
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
		Status:   string(run.Status),
		Error:    run.Error,
		Steps:    nil,
	}

	for _, step := range m.steps[runID] {
		if step.Status == types.StepStatusRunning || step.Status == types.StepStatusPending {
			continue
		}

		var output map[string]interface{}
		if step.Output != nil {
			output = make(map[string]interface{}, len(step.Output))
			for k, v := range step.Output {
				output[k] = v
			}
		} else {
			output = make(map[string]interface{})
		}

		metrics := step.Metrics
		if step.StartedAt != nil && step.EndedAt != nil {
			metrics.DurationMS = step.EndedAt.Sub(*step.StartedAt).Milliseconds()
		}

		var ts time.Time
		if step.EndedAt != nil {
			ts = *step.EndedAt
		}

		env.Steps = append(env.Steps, types.StepEntry{
			ID: step.ID,
			StepOutput: types.StepOutput{
				Output:    output,
				Metrics:   metrics,
				Timestamp: ts,
				Status:    string(step.Status),
				Error:     step.Error,
			},
		})

		env.Totals.DurationMS += metrics.DurationMS
		env.Totals.TokensIn += metrics.TokensIn
		env.Totals.TokensOut += metrics.TokensOut
		env.Totals.CostUSD += metrics.CostUSD
		env.Totals.LLMCalls += metrics.LLMCalls
		env.Totals.ToolCalls += metrics.ToolCalls
	}

	return env, nil
}

// CreateApproval stores a copy of the approval. Returns an error if the key already exists.
func (m *MemStore) CreateApproval(_ context.Context, approval *types.Approval) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := approval.RunID + "/" + approval.StepID
	if _, exists := m.approvals[key]; exists {
		return errApprovalAlreadyExists
	}
	m.approvals[key] = copyApproval(approval)
	return nil
}

// GetApproval returns a copy of the stored approval. Returns an error if not found.
func (m *MemStore) GetApproval(_ context.Context, runID, stepID string) (*types.Approval, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	key := runID + "/" + stepID
	a, exists := m.approvals[key]
	if !exists {
		return nil, errApprovalNotFound
	}
	return copyApproval(a), nil
}

// ListPendingApprovals returns copies of all approvals with status pending.
func (m *MemStore) ListPendingApprovals(_ context.Context) ([]*types.Approval, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var out []*types.Approval
	for _, a := range m.approvals {
		if a.Status == types.ApprovalStatusPending {
			out = append(out, copyApproval(a))
		}
	}
	return out, nil
}

// UpdateApproval replaces the stored approval with a copy of the provided value.
// Returns an error if the approval does not exist.
func (m *MemStore) UpdateApproval(_ context.Context, approval *types.Approval) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := approval.RunID + "/" + approval.StepID
	if _, exists := m.approvals[key]; !exists {
		return errApprovalNotFound
	}
	m.approvals[key] = copyApproval(approval)
	return nil
}
