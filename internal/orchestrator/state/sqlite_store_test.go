package state

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/kimitsu-ai/ktsu/pkg/types"
)

func TestSQLiteStore_CRUD(t *testing.T) {
	ctx := context.Background()
	dbFile := "test_ktsu.db"
	defer os.Remove(dbFile)

	s, err := NewSQLiteStore(dbFile)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}

	// Test Run CRUD
	run := &types.Run{
		ID:           "run-1",
		WorkflowName: "test-wf",
		Status:       types.RunStatusRunning,
		CreatedAt:    time.Now().Round(time.Second),
		UpdatedAt:    time.Now().Round(time.Second),
		Metadata:     map[string]string{"foo": "bar"},
	}

	if err := s.CreateRun(ctx, run); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}

	gotRun, err := s.GetRun(ctx, "run-1")
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if gotRun.ID != run.ID || gotRun.WorkflowName != run.WorkflowName || gotRun.Metadata["foo"] != "bar" {
		t.Errorf("GetRun mismatch: %+v", gotRun)
	}

	run.Status = types.RunStatusComplete
	run.UpdatedAt = time.Now().Round(time.Second)
	if err := s.UpdateRun(ctx, run); err != nil {
		t.Fatalf("UpdateRun: %v", err)
	}

	gotRun, _ = s.GetRun(ctx, "run-1")
	if gotRun.Status != types.RunStatusComplete {
		t.Errorf("UpdateRun status mismatch: %v", gotRun.Status)
	}

	// Test Step CRUD
	now := time.Now().Round(time.Second)
	step := &types.Step{
		ID:        "step-1",
		RunID:     "run-1",
		Status:    types.StepStatusComplete,
		Output:    map[string]interface{}{"result": 42},
		StartedAt: &now,
		EndedAt:   &now,
		Metrics:   types.StepMetrics{TokensIn: 10},
	}

	if err := s.CreateStep(ctx, step); err != nil {
		t.Fatalf("CreateStep: %v", err)
	}

	gotStep, err := s.GetStep(ctx, "run-1", "step-1")
	if err != nil {
		t.Fatalf("GetStep: %v", err)
	}
	if gotStep.ID != step.ID || gotStep.Metrics.TokensIn != 10 || gotStep.Output["result"].(float64) != 42 {
		t.Errorf("GetStep mismatch: %+v", gotStep)
	}

	steps, err := s.ListSteps(ctx, "run-1")
	if err != nil {
		t.Fatalf("ListSteps: %v", err)
	}
	if len(steps) != 1 {
		t.Errorf("ListSteps len mismatch: %d", len(steps))
	}

	// Test Envelope
	env, err := s.GetEnvelope(ctx, "run-1")
	if err != nil {
		t.Fatalf("GetEnvelope: %v", err)
	}
	if env.RunID != "run-1" || len(env.Steps) != 1 || env.Totals.TokensIn != 10 {
		t.Errorf("GetEnvelope mismatch: %+v", env)
	}
}
