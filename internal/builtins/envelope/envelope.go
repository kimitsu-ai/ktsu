package envelope

import (
	"context"
	"encoding/json"

	builtins "github.com/your-org/sdd-services/internal/builtins"
	mcp "github.com/your-org/sdd-services/pkg/mcp"
)

type EnvelopeServer struct{}

func New() *EnvelopeServer { return &EnvelopeServer{} }

func (s *EnvelopeServer) Name() string { return "rss/envelope" }

func (s *EnvelopeServer) Tools() []mcp.Tool {
	return []mcp.Tool{
		{
			Name:        "envelope_get",
			Description: "Get a field from the current run envelope",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"path": map[string]interface{}{"type": "string"},
				},
				Required: []string{"path"},
			},
		},
		{
			Name:        "envelope_set",
			Description: "Set a field in the current run envelope",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"path":  map[string]interface{}{"type": "string"},
					"value": map[string]interface{}{"type": "string"},
				},
				Required: []string{"path", "value"},
			},
		},
		{
			Name:        "envelope_append",
			Description: "Append a value to an array field in the current run envelope",
			InputSchema: mcp.ToolInputSchema{
				Type: "object",
				Properties: map[string]interface{}{
					"path":  map[string]interface{}{"type": "string"},
					"value": map[string]interface{}{"type": "string"},
				},
				Required: []string{"path", "value"},
			},
		},
	}
}

func (s *EnvelopeServer) Call(ctx context.Context, name string, input json.RawMessage) (json.RawMessage, error) {
	return nil, builtins.ErrNotImplemented
}
