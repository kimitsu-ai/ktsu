package state

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/kimitsu-ai/ktsu/pkg/types"
	_ "modernc.org/sqlite"
)

type SQLiteStore struct {
	db *sql.DB
	mu sync.Mutex // For migrations and basic serial safety if needed
}

func NewSQLiteStore(dsn string) (*SQLiteStore, error) {
	if dsn == "" {
		dsn = "ktsu.db"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %s: %w", dsn, err)
	}

	s := &SQLiteStore{db: db}
	if err := s.init(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *SQLiteStore) init() error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS runs (
			id TEXT PRIMARY KEY,
			workflow_name TEXT,
			status TEXT,
			error TEXT,
			metadata TEXT,
			created_at DATETIME,
			updated_at DATETIME
		)`,
		`CREATE TABLE IF NOT EXISTS steps (
			run_id TEXT,
			id TEXT,
			status TEXT,
			error TEXT,
			output TEXT,
			metrics TEXT,
			started_at DATETIME,
			ended_at DATETIME,
			PRIMARY KEY (run_id, id)
		)`,
	}

	for _, q := range queries {
		if _, err := s.db.Exec(q); err != nil {
			return fmt.Errorf("init sqlite schema: %w", err)
		}
	}
	return nil
}

func (s *SQLiteStore) CreateRun(ctx context.Context, run *types.Run) error {
	metadata, _ := json.Marshal(run.Metadata)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO runs (id, workflow_name, status, error, metadata, created_at, updated_at) 
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		run.ID, run.WorkflowName, run.Status, run.Error, string(metadata), run.CreatedAt, run.UpdatedAt)
	if err != nil {
		return fmt.Errorf("create run: %w", err)
	}
	return nil
}

func (s *SQLiteStore) UpdateRun(ctx context.Context, run *types.Run) error {
	metadata, _ := json.Marshal(run.Metadata)
	res, err := s.db.ExecContext(ctx,
		`UPDATE runs SET status = ?, error = ?, metadata = ?, updated_at = ? WHERE id = ?`,
		run.Status, run.Error, string(metadata), run.UpdatedAt, run.ID)
	if err != nil {
		return fmt.Errorf("update run: %w", err)
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return errRunNotFound
	}
	return nil
}

func (s *SQLiteStore) GetRun(ctx context.Context, runID string) (*types.Run, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, workflow_name, status, error, metadata, created_at, updated_at FROM runs WHERE id = ?`, runID)
	var run types.Run
	var metadata string
	var status string
	err := row.Scan(&run.ID, &run.WorkflowName, &status, &run.Error, &metadata, &run.CreatedAt, &run.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, errRunNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get run: %w", err)
	}
	run.Status = types.RunStatus(status)
	if metadata != "" {
		json.Unmarshal([]byte(metadata), &run.Metadata)
	}
	return &run, nil
}

func (s *SQLiteStore) CreateStep(ctx context.Context, step *types.Step) error {
	output, _ := json.Marshal(step.Output)
	metrics, _ := json.Marshal(step.Metrics)
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO steps (run_id, id, status, error, output, metrics, started_at, ended_at) 
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		step.RunID, step.ID, step.Status, step.Error, string(output), string(metrics), step.StartedAt, step.EndedAt)
	if err != nil {
		return fmt.Errorf("create step: %w", err)
	}
	return nil
}

func (s *SQLiteStore) UpdateStep(ctx context.Context, step *types.Step) error {
	output, _ := json.Marshal(step.Output)
	metrics, _ := json.Marshal(step.Metrics)
	res, err := s.db.ExecContext(ctx,
		`UPDATE steps SET status = ?, error = ?, output = ?, metrics = ?, started_at = ?, ended_at = ? 
		 WHERE run_id = ? AND id = ?`,
		step.Status, step.Error, string(output), string(metrics), step.StartedAt, step.EndedAt, step.RunID, step.ID)
	if err != nil {
		return fmt.Errorf("update step: %w", err)
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return errStepNotFound
	}
	return nil
}

func (s *SQLiteStore) GetStep(ctx context.Context, runID, stepID string) (*types.Step, error) {
	row := s.db.QueryRowContext(ctx, `SELECT run_id, id, status, error, output, metrics, started_at, ended_at FROM steps WHERE run_id = ? AND id = ?`, runID, stepID)
	var step types.Step
	var status, output, metrics string
	err := row.Scan(&step.RunID, &step.ID, &status, &step.Error, &output, &metrics, &step.StartedAt, &step.EndedAt)
	if err == sql.ErrNoRows {
		return nil, errStepNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get step: %w", err)
	}
	step.Status = types.StepStatus(status)
	if output != "" {
		json.Unmarshal([]byte(output), &step.Output)
	}
	if metrics != "" {
		json.Unmarshal([]byte(metrics), &step.Metrics)
	}
	return &step, nil
}

func (s *SQLiteStore) ListSteps(ctx context.Context, runID string) ([]*types.Step, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT run_id, id, status, error, output, metrics, started_at, ended_at FROM steps WHERE run_id = ?`, runID)
	if err != nil {
		return nil, fmt.Errorf("list steps query: %w", err)
	}
	defer rows.Close()

	var steps []*types.Step
	for rows.Next() {
		var step types.Step
		var status, output, metrics string
		if err := rows.Scan(&step.RunID, &step.ID, &status, &step.Error, &output, &metrics, &step.StartedAt, &step.EndedAt); err != nil {
			return nil, fmt.Errorf("list steps scan: %w", err)
		}
		step.Status = types.StepStatus(status)
		if output != "" {
			json.Unmarshal([]byte(output), &step.Output)
		}
		if metrics != "" {
			json.Unmarshal([]byte(metrics), &step.Metrics)
		}
		steps = append(steps, &step)
	}
	return steps, nil
}

func (s *SQLiteStore) GetEnvelope(ctx context.Context, runID string) (*types.Envelope, error) {
	run, err := s.GetRun(ctx, runID)
	if err != nil {
		return nil, err
	}

	steps, err := s.ListSteps(ctx, runID)
	if err != nil {
		return nil, err
	}

	env := &types.Envelope{
		RunID:    runID,
		Workflow: run.WorkflowName,
		Status:   string(run.Status),
		Error:    run.Error,
	}

	for _, step := range steps {
		if step.Status == types.StepStatusRunning || step.Status == types.StepStatusPending {
			continue
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
				Output:    step.Output,
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
