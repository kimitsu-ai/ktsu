package orchestrator

import (
	"context"
	"fmt"
	"log"

	"github.com/kimitsu-ai/ktsu/internal/config"
	"github.com/kimitsu-ai/ktsu/internal/orchestrator/state"
)

// Workspace pairs a project root with its workflow directory.
// WorkflowDir defaults to ProjectDir/workflows if empty.
type Workspace struct {
	ProjectDir  string
	WorkflowDir string
}

type Config struct {
	EnvPath     string
	Env         *config.EnvConfig
	WorkflowDir string          // default: "./workflows"
	Host        string          // bind interface, "" = all
	Port        int             // default 5050
	RuntimeURL  string          // URL of the Agent Runtime (e.g. "http://runtime:5051")
	GatewayURL  string          // URL of the LLM Gateway (e.g. "http://gateway:5052")
	OwnURL      string          // this orchestrator's own URL, for constructing callback_url
	ProjectDir  string          // root dir for resolving agent/server paths (default: ".")
	APIKey      string          // optional bearer token; empty = auth disabled
	StoreType   state.StoreType // "memory" (default), "sqlite"
	StoreDSN    string          // database path for sqlite (default: "ktsu.db")
	Logger      *log.Logger
	Workspaces  []Workspace // additional workspaces (from --workspace or ktsuhub.lock.yaml)
	NoHubLock   bool        // skip ktsuhub.lock.yaml auto-load
}

type Orchestrator struct {
	cfg    Config
	logger *log.Logger
}

func New(cfg Config) *Orchestrator {
	return &Orchestrator{cfg: cfg, logger: cfg.Logger}
}

func (o *Orchestrator) Start(ctx context.Context) error {
	srv, err := newServer(o)
	if err != nil {
		return err
	}
	return srv.serve(ctx)
}

func (o *Orchestrator) String() string {
	return fmt.Sprintf("Orchestrator(env=%s)", o.cfg.EnvPath)
}
