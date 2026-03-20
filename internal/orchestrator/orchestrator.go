package orchestrator

import (
	"context"
	"fmt"

	"github.com/kimitsu-ai/ktsu/internal/config"
)

type Config struct {
	EnvPath string
	Env     *config.EnvConfig
}

type Orchestrator struct {
	cfg Config
}

func New(cfg Config) *Orchestrator {
	return &Orchestrator{cfg: cfg}
}

func (o *Orchestrator) Start(ctx context.Context) error {
	srv := newServer(o)
	return srv.serve(ctx)
}

func (o *Orchestrator) String() string {
	return fmt.Sprintf("Orchestrator(env=%s)", o.cfg.EnvPath)
}
