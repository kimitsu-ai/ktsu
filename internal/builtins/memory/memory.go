package memory

import (
	"context"
	"encoding/json"

	builtins "github.com/kimitsu-ai/ktsu/internal/builtins"
	mcp "github.com/kimitsu-ai/ktsu/pkg/mcp"
)

type MemoryServer struct{}

func New() *MemoryServer { return &MemoryServer{} }

func (s *MemoryServer) Name() string { return "ktsu/memory" }

func (s *MemoryServer) Tools() []mcp.Tool {
	return []mcp.Tool{
		{
			Name:        "memory_store",
			Description: "Store a memory entry with optional metadata",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"content":  map[string]interface{}{"type": "string"},
					"metadata": map[string]interface{}{"type": "object"},
				},
				Required: []string{"content"},
			},
		},
		{
			Name:        "memory_retrieve",
			Description: "Retrieve a memory entry by ID",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"id": map[string]interface{}{"type": "string"},
				},
				Required: []string{"id"},
			},
		},
		{
			Name:        "memory_search",
			Description: "Search memory entries by query",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"query": map[string]interface{}{"type": "string"},
					"limit": map[string]interface{}{"type": "integer"},
				},
				Required: []string{"query"},
			},
		},
		{
			Name:        "memory_forget",
			Description: "Delete a memory entry by ID",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"id": map[string]interface{}{"type": "string"},
				},
				Required: []string{"id"},
			},
		},
	}
}

func (s *MemoryServer) Call(ctx context.Context, name string, input json.RawMessage) (json.RawMessage, error) {
	return nil, builtins.ErrNotImplemented
}
