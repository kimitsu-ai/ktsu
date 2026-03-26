package state

import (
	"context"

	"github.com/kimitsu-ai/ktsu/pkg/types"
)

// Store is the persistence interface for run and step state.
type Store interface {
	CreateRun(ctx context.Context, run *types.Run) error
	UpdateRun(ctx context.Context, run *types.Run) error
	GetRun(ctx context.Context, runID string) (*types.Run, error)
	CreateStep(ctx context.Context, step *types.Step) error
	UpdateStep(ctx context.Context, step *types.Step) error
	GetStep(ctx context.Context, runID, stepID string) (*types.Step, error)
	ListSteps(ctx context.Context, runID string) ([]*types.Step, error)
	GetEnvelope(ctx context.Context, runID string) (*types.Envelope, error)
}

// SQLiteStore is a SQLite-backed implementation of Store.
type SQLiteStore struct {
	dsn string
}

func NewSQLiteStore(dsn string) (*SQLiteStore, error) {
	return &SQLiteStore{dsn: dsn}, nil
}

func (s *SQLiteStore) CreateRun(ctx context.Context, run *types.Run) error {
	return ErrNotImplemented
}

func (s *SQLiteStore) UpdateRun(ctx context.Context, run *types.Run) error {
	return ErrNotImplemented
}

func (s *SQLiteStore) GetRun(ctx context.Context, runID string) (*types.Run, error) {
	return nil, ErrNotImplemented
}

func (s *SQLiteStore) CreateStep(ctx context.Context, step *types.Step) error {
	return ErrNotImplemented
}

func (s *SQLiteStore) UpdateStep(ctx context.Context, step *types.Step) error {
	return ErrNotImplemented
}

func (s *SQLiteStore) GetStep(ctx context.Context, runID, stepID string) (*types.Step, error) {
	return nil, ErrNotImplemented
}

func (s *SQLiteStore) ListSteps(ctx context.Context, runID string) ([]*types.Step, error) {
	return nil, ErrNotImplemented
}

func (s *SQLiteStore) GetEnvelope(ctx context.Context, runID string) (*types.Envelope, error) {
	return nil, ErrNotImplemented
}

// ErrNotImplemented is returned by store stubs
var ErrNotImplemented = storeError("not implemented")

type storeError string

func (e storeError) Error() string { return string(e) }
