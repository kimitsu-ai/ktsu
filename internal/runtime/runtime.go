package runtime

import (
	"context"
	"fmt"
)

type Config struct {
	OrchestratorURL string
	LLMGatewayURL   string
	Port            int
}

type Runtime struct {
	cfg Config
}

func New(cfg Config) *Runtime {
	return &Runtime{cfg: cfg}
}

func (r *Runtime) Start(ctx context.Context) error {
	srv := newServer(r)
	return srv.serve(ctx)
}

func (r *Runtime) String() string {
	return fmt.Sprintf("Runtime(orchestrator=%s, gateway=%s)", r.cfg.OrchestratorURL, r.cfg.LLMGatewayURL)
}
