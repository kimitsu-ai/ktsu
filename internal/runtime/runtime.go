package runtime

import (
	"context"
	"fmt"
	"net/http"

	"github.com/kimitsu-ai/ktsu/internal/runtime/agent"
	"github.com/kimitsu-ai/ktsu/internal/runtime/agent/mcp"
)

// Config holds the runtime's startup configuration.
type Config struct {
	OrchestratorURL string
	LLMGatewayURL   string
	Host            string // bind interface, "" = all
	Port            int    // default 8082
}

// Runtime is the agent execution service.
type Runtime struct {
	cfg  Config
	srv  *server
}

// New creates a Runtime from config.
func New(cfg Config) *Runtime {
	mcpClient := mcp.New(http.DefaultClient)
	loop := agent.NewLoop(cfg.LLMGatewayURL, mcpClient)
	r := &Runtime{cfg: cfg}
	r.srv = newServer(r, loop)
	return r
}

// Start runs the runtime HTTP server and heartbeat, blocking until ctx is cancelled.
func (r *Runtime) Start(ctx context.Context) error {
	go r.heartbeatLoop(ctx)
	return r.srv.serve(ctx)
}

func (r *Runtime) String() string {
	return fmt.Sprintf("Runtime(orchestrator=%s, gateway=%s)", r.cfg.OrchestratorURL, r.cfg.LLMGatewayURL)
}
