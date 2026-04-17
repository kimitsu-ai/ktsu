package state

import (
	"context"
	"database/sql"
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

func TestSQLiteStore_reflected_roundtrip(t *testing.T) {
	ctx := context.Background()
	dbFile := "test_reflected.db"
	defer os.Remove(dbFile)

	s, err := NewSQLiteStore(dbFile)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}

	// Create a run for foreign-key satisfaction.
	run := &types.Run{
		ID:           "run-ref",
		WorkflowName: "wf",
		Status:       types.RunStatusRunning,
		CreatedAt:    time.Now().Round(time.Second),
		UpdatedAt:    time.Now().Round(time.Second),
	}
	if err := s.CreateRun(ctx, run); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}

	trueVal := true
	falseVal := false
	now := time.Now().Round(time.Second)

	// Agent step that reflected.
	stepReflected := &types.Step{
		ID:        "s-reflected",
		RunID:     "run-ref",
		Status:    types.StepStatusComplete,
		Output:    map[string]interface{}{"r": 1},
		StartedAt: &now,
		EndedAt:   &now,
		Reflected: &trueVal,
	}
	if err := s.CreateStep(ctx, stepReflected); err != nil {
		t.Fatalf("CreateStep reflected: %v", err)
	}

	// Agent step that did not reflect.
	stepNotReflected := &types.Step{
		ID:        "s-not-reflected",
		RunID:     "run-ref",
		Status:    types.StepStatusComplete,
		Output:    map[string]interface{}{"r": 2},
		StartedAt: &now,
		EndedAt:   &now,
		Reflected: &falseVal,
	}
	if err := s.CreateStep(ctx, stepNotReflected); err != nil {
		t.Fatalf("CreateStep not-reflected: %v", err)
	}

	// Non-agent step (nil reflected).
	stepNil := &types.Step{
		ID:        "s-nil",
		RunID:     "run-ref",
		Status:    types.StepStatusComplete,
		Output:    map[string]interface{}{"r": 3},
		StartedAt: &now,
		EndedAt:   &now,
		Reflected: nil,
	}
	if err := s.CreateStep(ctx, stepNil); err != nil {
		t.Fatalf("CreateStep nil: %v", err)
	}

	got, err := s.GetStep(ctx, "run-ref", "s-reflected")
	if err != nil {
		t.Fatalf("GetStep reflected: %v", err)
	}
	if got.Reflected == nil || *got.Reflected != true {
		t.Errorf("s-reflected: want Reflected=true, got %v", got.Reflected)
	}

	got, err = s.GetStep(ctx, "run-ref", "s-not-reflected")
	if err != nil {
		t.Fatalf("GetStep not-reflected: %v", err)
	}
	if got.Reflected == nil || *got.Reflected != false {
		t.Errorf("s-not-reflected: want Reflected=false, got %v", got.Reflected)
	}

	got, err = s.GetStep(ctx, "run-ref", "s-nil")
	if err != nil {
		t.Fatalf("GetStep nil: %v", err)
	}
	if got.Reflected != nil {
		t.Errorf("s-nil: want Reflected=nil, got %v", got.Reflected)
	}

	// UpdateStep: flip reflected from true to false.
	stepReflected.Reflected = &falseVal
	if err := s.UpdateStep(ctx, stepReflected); err != nil {
		t.Fatalf("UpdateStep: %v", err)
	}
	got, err = s.GetStep(ctx, "run-ref", "s-reflected")
	if err != nil {
		t.Fatalf("GetStep after update: %v", err)
	}
	if got.Reflected == nil || *got.Reflected != false {
		t.Errorf("after update: want Reflected=false, got %v", got.Reflected)
	}
}

func TestSQLiteStore_ListRuns(t *testing.T) {
	ctx := context.Background()
	dbFile := "test_list_runs.db"
	defer os.Remove(dbFile)

	s, err := NewSQLiteStore(dbFile)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}

	now := time.Now().Round(time.Second)
	runs := []*types.Run{
		{ID: "run-a", WorkflowName: "wf-one", Status: types.RunStatusComplete, CreatedAt: now.Add(-3 * time.Minute), UpdatedAt: now},
		{ID: "run-b", WorkflowName: "wf-two", Status: types.RunStatusFailed, CreatedAt: now.Add(-2 * time.Minute), UpdatedAt: now},
		{ID: "run-c", WorkflowName: "wf-one", Status: types.RunStatusRunning, CreatedAt: now.Add(-1 * time.Minute), UpdatedAt: now},
	}
	for _, r := range runs {
		if err := s.CreateRun(ctx, r); err != nil {
			t.Fatalf("CreateRun: %v", err)
		}
	}

	t.Run("no filter returns all sorted desc", func(t *testing.T) {
		got, err := s.ListRuns(ctx, ListRunsFilter{})
		if err != nil {
			t.Fatalf("ListRuns: %v", err)
		}
		if len(got) != 3 {
			t.Fatalf("want 3, got %d", len(got))
		}
		if got[0].ID != "run-c" || got[1].ID != "run-b" || got[2].ID != "run-a" {
			t.Errorf("unexpected order: %v %v %v", got[0].ID, got[1].ID, got[2].ID)
		}
	})

	t.Run("filter by workflow name", func(t *testing.T) {
		got, err := s.ListRuns(ctx, ListRunsFilter{WorkflowName: "wf-one"})
		if err != nil {
			t.Fatalf("ListRuns: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("want 2, got %d", len(got))
		}
	})

	t.Run("filter by status", func(t *testing.T) {
		got, err := s.ListRuns(ctx, ListRunsFilter{Status: types.RunStatusFailed})
		if err != nil {
			t.Fatalf("ListRuns: %v", err)
		}
		if len(got) != 1 || got[0].ID != "run-b" {
			t.Errorf("want run-b only, got %v", got)
		}
	})

	t.Run("limit", func(t *testing.T) {
		got, err := s.ListRuns(ctx, ListRunsFilter{Limit: 1})
		if err != nil {
			t.Fatalf("ListRuns: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("want 1, got %d", len(got))
		}
		if got[0].ID != "run-c" {
			t.Errorf("want most recent run-c, got %s", got[0].ID)
		}
	})
}

func TestSQLiteStore_reflected_migration(t *testing.T) {
	// Simulate a pre-existing DB without the reflected column.
	dbFile := "test_migration.db"
	defer os.Remove(dbFile)

	// Create DB with old schema manually.
	db, err := sql.Open("sqlite", dbFile)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	_, err = db.Exec(`CREATE TABLE steps (
		run_id TEXT,
		id TEXT,
		status TEXT,
		error TEXT,
		output TEXT,
		metrics TEXT,
		started_at DATETIME,
		ended_at DATETIME,
		PRIMARY KEY (run_id, id)
	)`)
	db.Close()
	if err != nil {
		t.Fatalf("create old schema: %v", err)
	}

	// Open via NewSQLiteStore — migration must succeed.
	s, err := NewSQLiteStore(dbFile)
	if err != nil {
		t.Fatalf("NewSQLiteStore migration failed: %v", err)
	}
	_ = s
}
