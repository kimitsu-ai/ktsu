package kv

import (
	"context"
	"encoding/json"

	builtins "github.com/your-org/sdd-services/internal/builtins"
	mcp "github.com/your-org/sdd-services/pkg/mcp"
)

type KVServer struct{}

func New() *KVServer { return &KVServer{} }

func (s *KVServer) Name() string { return "rss/kv" }

func (s *KVServer) Tools() []mcp.Tool {
	return []mcp.Tool{
		{
			Name:        "kv_get",
			Description: "Get a value by key",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"key": map[string]interface{}{"type": "string"},
				},
				Required: []string{"key"},
			},
		},
		{
			Name:        "kv_set",
			Description: "Set a value by key",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"key":   map[string]interface{}{"type": "string"},
					"value": map[string]interface{}{"type": "string"},
				},
				Required: []string{"key", "value"},
			},
		},
		{
			Name:        "kv_delete",
			Description: "Delete a value by key",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"key": map[string]interface{}{"type": "string"},
				},
				Required: []string{"key"},
			},
		},
	}
}

func (s *KVServer) Call(ctx context.Context, name string, input json.RawMessage) (json.RawMessage, error) {
	return nil, builtins.ErrNotImplemented
}
