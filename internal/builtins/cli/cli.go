package cli

import (
	"context"
	"encoding/json"

	builtins "github.com/your-org/sdd-services/internal/builtins"
	mcp "github.com/your-org/sdd-services/pkg/mcp"
)

type CLIServer struct{}

func New() *CLIServer { return &CLIServer{} }

func (s *CLIServer) Name() string { return "rss/cli" }

func (s *CLIServer) Tools() []mcp.Tool {
	return []mcp.Tool{
		{
			Name:        "cli_exec",
			Description: "Execute a shell command and return its output",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"command":    map[string]interface{}{"type": "string"},
					"timeout_ms": map[string]interface{}{"type": "integer"},
				},
				Required: []string{"command"},
			},
		},
		{
			Name:        "cli_spawn",
			Description: "Spawn a long-running process and return its process ID",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"command": map[string]interface{}{"type": "string"},
					"args":    map[string]interface{}{"type": "array"},
				},
				Required: []string{"command"},
			},
		},
	}
}

func (s *CLIServer) Call(ctx context.Context, name string, input json.RawMessage) (json.RawMessage, error) {
	return nil, builtins.ErrNotImplemented
}
