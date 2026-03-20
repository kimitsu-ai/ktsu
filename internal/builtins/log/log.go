package log

import (
	"context"
	"encoding/json"

	builtins "github.com/kimitsu-ai/ktsu/internal/builtins"
	mcp "github.com/kimitsu-ai/ktsu/pkg/mcp"
)

type LogServer struct{}

func New() *LogServer { return &LogServer{} }

func (s *LogServer) Name() string { return "ktsu/log" }

func (s *LogServer) Tools() []mcp.Tool {
	return []mcp.Tool{
		{
			Name:        "log_write",
			Description: "Write a log entry",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"level":   map[string]interface{}{"type": "string"},
					"message": map[string]interface{}{"type": "string"},
				},
				Required: []string{"message"},
			},
		},
		{
			Name:        "log_read",
			Description: "Read log entries by run ID",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"run_id": map[string]interface{}{"type": "string"},
					"limit":  map[string]interface{}{"type": "integer"},
				},
				Required: []string{"run_id"},
			},
		},
		{
			Name:        "log_tail",
			Description: "Tail the most recent log entries",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"n": map[string]interface{}{"type": "integer"},
				},
			},
		},
	}
}

func (s *LogServer) Call(ctx context.Context, name string, input json.RawMessage) (json.RawMessage, error) {
	return nil, builtins.ErrNotImplemented
}
