package state

import (
	"context"
	"fmt"

	"github.com/kimitsu-ai/ktsu/pkg/types"
)

// StoreType identifies the storage backend.
type StoreType string

const (
	StoreTypeMemory   StoreType = "memory"
	StoreTypeSQLite   StoreType = "sqlite"
	StoreTypePostgres StoreType = "postgres"
)

// StoreConfig holds the configuration for a state store.
type StoreConfig struct {
	Type StoreType
	DSN  string // Data Source Name (e.g. database file path for SQLite)
}

// ListRunsFilter constrains which runs ListRuns returns.
type ListRunsFilter struct {
	WorkflowName string
	Status       types.RunStatus
	Limit        int // defaults to 50 when zero
}

// Store is the persistence interface for run and step state.
type Store interface {
	CreateRun(ctx context.Context, run *types.Run) error
	UpdateRun(ctx context.Context, run *types.Run) error
	GetRun(ctx context.Context, runID string) (*types.Run, error)
	ListRuns(ctx context.Context, filter ListRunsFilter) ([]*types.Run, error)
	CreateStep(ctx context.Context, step *types.Step) error
	UpdateStep(ctx context.Context, step *types.Step) error
	GetStep(ctx context.Context, runID, stepID string) (*types.Step, error)
	ListSteps(ctx context.Context, runID string) ([]*types.Step, error)
	GetEnvelope(ctx context.Context, runID string) (*types.Envelope, error)
	CreateApproval(ctx context.Context, approval *types.Approval) error
	GetApproval(ctx context.Context, runID, stepID string) (*types.Approval, error)
	ListPendingApprovals(ctx context.Context) ([]*types.Approval, error)
	UpdateApproval(ctx context.Context, approval *types.Approval) error
}

// NewStore initializes a Store based on the provided configuration.
func NewStore(cfg StoreConfig) (Store, error) {
	switch cfg.Type {
	case StoreTypeMemory:
		return NewMemStore(), nil
	case StoreTypeSQLite:
		return NewSQLiteStore(cfg.DSN)
	case StoreTypePostgres:
		return nil, fmt.Errorf("postgres store not yet implemented")
	default:
		// Default to memory if not specified, but error on unknown types
		if cfg.Type == "" {
			return NewMemStore(), nil
		}
		return nil, fmt.Errorf("unknown store type: %s", cfg.Type)
	}
}

func (s *SQLiteStore) CreateApproval(ctx context.Context, approval *types.Approval) error {
	return ErrNotImplemented
}

func (s *SQLiteStore) GetApproval(ctx context.Context, runID, stepID string) (*types.Approval, error) {
	return nil, ErrNotImplemented
}

func (s *SQLiteStore) ListPendingApprovals(ctx context.Context) ([]*types.Approval, error) {
	return nil, ErrNotImplemented
}

func (s *SQLiteStore) UpdateApproval(ctx context.Context, approval *types.Approval) error {
	return ErrNotImplemented
}

// ErrNotImplemented is returned by store stubs
var ErrNotImplemented = storeError("not implemented")

type storeError string

func (e storeError) Error() string { return string(e) }
