package state

import (
	"context"
	"testing"
	"time"

	"github.com/kimitsu-ai/ktsu/pkg/types"
)

func TestMemStore_CreateAndGetRun(t *testing.T) {
	ctx := context.Background()
	s := NewMemStore()

	run := &types.Run{
		ID:           "run-1",
		WorkflowName: "my-workflow",
		Status:       types.RunStatusPending,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	if err := s.CreateRun(ctx, run); err != nil {
		t.Fatalf("CreateRun: unexpected error: %v", err)
	}

	got, err := s.GetRun(ctx, "run-1")
	if err != nil {
		t.Fatalf("GetRun: unexpected error: %v", err)
	}

	if got.ID != run.ID {
		t.Errorf("got ID %q, want %q", got.ID, run.ID)
	}
	if got.WorkflowName != run.WorkflowName {
		t.Errorf("got WorkflowName %q, want %q", got.WorkflowName, run.WorkflowName)
	}

	// Verify it's a copy: mutating the returned value should not affect stored value.
	got.WorkflowName = "changed"
	got2, _ := s.GetRun(ctx, "run-1")
	if got2.WorkflowName == "changed" {
		t.Error("GetRun returned a reference, not a copy")
	}
}

func TestMemStore_UpdateRun(t *testing.T) {
	ctx := context.Background()
	s := NewMemStore()

	run := &types.Run{
		ID:     "run-2",
		Status: types.RunStatusPending,
	}
	if err := s.CreateRun(ctx, run); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}

	run.Status = types.RunStatusRunning
	if err := s.UpdateRun(ctx, run); err != nil {
		t.Fatalf("UpdateRun: %v", err)
	}

	got, err := s.GetRun(ctx, "run-2")
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if got.Status != types.RunStatusRunning {
		t.Errorf("got status %q, want %q", got.Status, types.RunStatusRunning)
	}
}

func TestMemStore_GetRun_notFound(t *testing.T) {
	ctx := context.Background()
	s := NewMemStore()

	_, err := s.GetRun(ctx, "does-not-exist")
	if err == nil {
		t.Fatal("expected error for missing run, got nil")
	}
}

func TestMemStore_CreateAndGetStep(t *testing.T) {
	ctx := context.Background()
	s := NewMemStore()

	run := &types.Run{ID: "run-3", Status: types.RunStatusPending}
	if err := s.CreateRun(ctx, run); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}

	step := &types.Step{
		ID:     "step-1",
		RunID:  "run-3",
		Name:   "my-step",
		Type:   types.StepTypeTransform,
		Status: types.StepStatusPending,
	}
	if err := s.CreateStep(ctx, step); err != nil {
		t.Fatalf("CreateStep: %v", err)
	}

	got, err := s.GetStep(ctx, "run-3", "step-1")
	if err != nil {
		t.Fatalf("GetStep: %v", err)
	}
	if got.ID != step.ID {
		t.Errorf("got ID %q, want %q", got.ID, step.ID)
	}
	if got.Name != step.Name {
		t.Errorf("got Name %q, want %q", got.Name, step.Name)
	}

	// Verify it's a copy.
	got.Name = "changed"
	got2, _ := s.GetStep(ctx, "run-3", "step-1")
	if got2.Name == "changed" {
		t.Error("GetStep returned a reference, not a copy")
	}
}

func TestMemStore_UpdateStep(t *testing.T) {
	ctx := context.Background()
	s := NewMemStore()

	run := &types.Run{ID: "run-4", Status: types.RunStatusPending}
	if err := s.CreateRun(ctx, run); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}

	step := &types.Step{
		ID:     "step-2",
		RunID:  "run-4",
		Name:   "step-two",
		Status: types.StepStatusPending,
	}
	if err := s.CreateStep(ctx, step); err != nil {
		t.Fatalf("CreateStep: %v", err)
	}

	step.Status = types.StepStatusRunning
	if err := s.UpdateStep(ctx, step); err != nil {
		t.Fatalf("UpdateStep: %v", err)
	}

	got, err := s.GetStep(ctx, "run-4", "step-2")
	if err != nil {
		t.Fatalf("GetStep: %v", err)
	}
	if got.Status != types.StepStatusRunning {
		t.Errorf("got status %q, want %q", got.Status, types.StepStatusRunning)
	}
}

func TestMemStore_GetStep_notFound(t *testing.T) {
	ctx := context.Background()
	s := NewMemStore()

	_, err := s.GetStep(ctx, "run-x", "step-x")
	if err == nil {
		t.Fatal("expected error for missing step, got nil")
	}
}

func TestMemStore_CreateRun_duplicate(t *testing.T) {
	ctx := context.Background()
	s := NewMemStore()

	run := &types.Run{ID: "run-dup", Status: types.RunStatusPending}
	if err := s.CreateRun(ctx, run); err != nil {
		t.Fatalf("first CreateRun: unexpected error: %v", err)
	}
	if err := s.CreateRun(ctx, run); err == nil {
		t.Fatal("second CreateRun with same ID: expected error, got nil")
	}
}

func TestMemStore_CreateStep_duplicate(t *testing.T) {
	ctx := context.Background()
	s := NewMemStore()

	run := &types.Run{ID: "run-step-dup", Status: types.RunStatusPending}
	if err := s.CreateRun(ctx, run); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}

	step := &types.Step{ID: "step-dup", RunID: "run-step-dup", Status: types.StepStatusPending}
	if err := s.CreateStep(ctx, step); err != nil {
		t.Fatalf("first CreateStep: unexpected error: %v", err)
	}
	if err := s.CreateStep(ctx, step); err == nil {
		t.Fatal("second CreateStep with same ID: expected error, got nil")
	}
}

func TestMemStore_CreateRun_copyOnStore(t *testing.T) {
	ctx := context.Background()
	s := NewMemStore()

	run := &types.Run{
		ID:       "run-copy",
		Status:   types.RunStatusPending,
		Metadata: map[string]string{"key": "original"},
	}
	if err := s.CreateRun(ctx, run); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}

	// Mutate the original map after storing.
	run.Metadata["key"] = "mutated"

	got, err := s.GetRun(ctx, "run-copy")
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if got.Metadata["key"] != "original" {
		t.Errorf("stored Metadata[\"key\"] = %q, want %q (map aliasing bug)", got.Metadata["key"], "original")
	}
}

func TestMemStore_GetEnvelope_buildsFromSteps(t *testing.T) {
	ctx := context.Background()
	s := NewMemStore()

	run := &types.Run{
		ID:           "run-5",
		WorkflowName: "test-workflow",
		Status:       types.RunStatusRunning,
	}
	if err := s.CreateRun(ctx, run); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}

	now := time.Now()
	ended := now.Add(100 * time.Millisecond)

	completeStep := &types.Step{
		ID:        "step-complete",
		RunID:     "run-5",
		Name:      "complete-step",
		Status:    types.StepStatusComplete,
		StartedAt: &now,
		EndedAt:   &ended,
		Output:    map[string]interface{}{"key": "value"},
	}
	pendingStep := &types.Step{
		ID:     "step-pending",
		RunID:  "run-5",
		Name:   "pending-step",
		Status: types.StepStatusPending,
	}

	if err := s.CreateStep(ctx, completeStep); err != nil {
		t.Fatalf("CreateStep complete: %v", err)
	}
	if err := s.CreateStep(ctx, pendingStep); err != nil {
		t.Fatalf("CreateStep pending: %v", err)
	}

	env, err := s.GetEnvelope(ctx, "run-5")
	if err != nil {
		t.Fatalf("GetEnvelope: %v", err)
	}

	if env.RunID != "run-5" {
		t.Errorf("env.RunID = %q, want %q", env.RunID, "run-5")
	}
	if env.Workflow != "test-workflow" {
		t.Errorf("env.Workflow = %q, want %q", env.Workflow, "test-workflow")
	}

	if _, ok := env.Steps["step-pending"]; ok {
		t.Error("envelope should not include pending step")
	}

	so, ok := env.Steps["step-complete"]
	if !ok {
		t.Fatal("envelope missing complete step")
	}
	if so.Output["key"] != "value" {
		t.Errorf("step output key = %v, want %q", so.Output["key"], "value")
	}
	if so.Timestamp.IsZero() {
		t.Error("step output Timestamp should not be zero for completed step with EndedAt set")
	}
	if so.Metrics.DurationMS <= 0 {
		t.Errorf("DurationMS = %d, want > 0", so.Metrics.DurationMS)
	}
}
