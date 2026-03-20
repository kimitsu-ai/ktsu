package transform

import (
	"context"
	"encoding/json"

	builtins "github.com/your-org/sdd-services/internal/builtins"
	mcp "github.com/your-org/sdd-services/pkg/mcp"
)

type TransformServer struct{}

func New() *TransformServer { return &TransformServer{} }

func (s *TransformServer) Name() string { return "rss/transform" }

func (s *TransformServer) Tools() []mcp.Tool {
	return []mcp.Tool{
		{
			Name:        "transform_jmespath",
			Description: "Apply a JMESPath expression to a JSON value",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"expression": map[string]interface{}{"type": "string"},
					"value":      map[string]interface{}{"type": "object"},
				},
				Required: []string{"expression", "value"},
			},
		},
		{
			Name:        "transform_map",
			Description: "Apply a set of JMESPath mappings to produce a new object",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"mappings": map[string]interface{}{"type": "object"},
					"value":    map[string]interface{}{"type": "object"},
				},
				Required: []string{"mappings", "value"},
			},
		},
		{
			Name:        "transform_filter",
			Description: "Filter an array using a JMESPath condition",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"condition": map[string]interface{}{"type": "string"},
					"items":     map[string]interface{}{"type": "array"},
				},
				Required: []string{"condition", "items"},
			},
		},
	}
}

func (s *TransformServer) Call(ctx context.Context, name string, input json.RawMessage) (json.RawMessage, error) {
	return nil, builtins.ErrNotImplemented
}
