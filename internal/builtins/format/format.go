package format

import (
	"context"
	"encoding/json"

	builtins "github.com/kimitsu-ai/ktsu/internal/builtins"
	mcp "github.com/kimitsu-ai/ktsu/pkg/mcp"
)

type FormatServer struct{}

func New() *FormatServer { return &FormatServer{} }

func (s *FormatServer) Name() string { return "ktsu/format" }

func (s *FormatServer) Tools() []mcp.Tool {
	return []mcp.Tool{
		{
			Name:        "format_json",
			Description: "Format a value as pretty-printed JSON",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"value": map[string]interface{}{"type": "string"},
					"indent": map[string]interface{}{"type": "integer"},
				},
				Required: []string{"value"},
			},
		},
		{
			Name:        "format_yaml",
			Description: "Format a value as YAML",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"value": map[string]interface{}{"type": "string"},
				},
				Required: []string{"value"},
			},
		},
		{
			Name:        "format_template",
			Description: "Render a Go text/template with provided data",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"template": map[string]interface{}{"type": "string"},
					"data":     map[string]interface{}{"type": "object"},
				},
				Required: []string{"template", "data"},
			},
		},
	}
}

func (s *FormatServer) Call(ctx context.Context, name string, input json.RawMessage) (json.RawMessage, error) {
	return nil, builtins.ErrNotImplemented
}
