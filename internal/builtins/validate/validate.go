package validate

import (
	"context"
	"encoding/json"

	builtins "github.com/your-org/sdd-services/internal/builtins"
	mcp "github.com/your-org/sdd-services/pkg/mcp"
)

type ValidateServer struct{}

func New() *ValidateServer { return &ValidateServer{} }

func (s *ValidateServer) Name() string { return "rss/validate" }

func (s *ValidateServer) Tools() []mcp.Tool {
	return []mcp.Tool{
		{
			Name:        "validate_schema",
			Description: "Validate a value against a JSON Schema",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"value":  map[string]interface{}{"type": "object"},
					"schema": map[string]interface{}{"type": "object"},
				},
				Required: []string{"value", "schema"},
			},
		},
		{
			Name:        "validate_json",
			Description: "Check that a string is valid JSON",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"value": map[string]interface{}{"type": "string"},
				},
				Required: []string{"value"},
			},
		},
	}
}

func (s *ValidateServer) Call(ctx context.Context, name string, input json.RawMessage) (json.RawMessage, error) {
	return nil, builtins.ErrNotImplemented
}
