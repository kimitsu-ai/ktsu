package orchestrator

import (
	"context"
	"fmt"

	"github.com/kimitsu-ai/ktsu/internal/config"
)

type Config struct {
	EnvPath     string
	Env         *config.EnvConfig
	WorkflowDir string // default: "./workflows"
	Host        string // bind interface, "" = all
	Port        int    // default 8080
	RuntimeURL  string // URL of the Agent Runtime (e.g. "http://runtime:8082")
	OwnURL      string // this orchestrator's own URL, for constructing callback_url
	ProjectDir  string // root dir for resolving agent/server paths (default: ".")
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
