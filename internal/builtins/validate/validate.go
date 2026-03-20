package validate

import (
	"context"
	"encoding/json"
	"fmt"

	mcp "github.com/kimitsu-ai/ktsu/pkg/mcp"
)

type ValidateServer struct{}

func New() *ValidateServer { return &ValidateServer{} }

func (s *ValidateServer) Name() string { return "ktsu/validate" }

func (s *ValidateServer) Tools() []mcp.Tool {
	return []mcp.Tool{
		{Name: "validate_schema", Description: "Validate a value against a JSON Schema", InputSchema: mcp.ToolInputSchema{Type: "object", Properties: map[string]interface{}{"value": map[string]interface{}{"type": "object"}, "schema": map[string]interface{}{"type": "object"}}, Required: []string{"value", "schema"}}},
		{Name: "validate_json", Description: "Check that a string is valid JSON", InputSchema: mcp.ToolInputSchema{Type: "object", Properties: map[string]interface{}{"value": map[string]interface{}{"type": "string"}}, Required: []string{"value"}}},
	}
}

func (s *ValidateServer) Call(_ context.Context, name string, input json.RawMessage) (json.RawMessage, error) {
	switch name {
	case "validate_json":
		var args map[string]string
		json.Unmarshal(input, &args)
		var v any
		if err := json.Unmarshal([]byte(args["value"]), &v); err != nil {
			return json.Marshal(map[string]any{"valid": false, "error": err.Error()})
		}
		return json.Marshal(map[string]bool{"valid": true})

	case "validate_schema":
		var args struct {
			Value  map[string]any `json:"value"`
			Schema map[string]any `json:"schema"`
		}
		json.Unmarshal(input, &args)

		if required, ok := args.Schema["required"].([]any); ok {
			for _, r := range required {
				field, _ := r.(string)
				if _, exists := args.Value[field]; !exists {
					msg := fmt.Sprintf("missing required field %q", field)
					return json.Marshal(map[string]any{"valid": false, "error": msg})
				}
			}
		}
		return json.Marshal(map[string]bool{"valid": true})

	default:
		return nil, fmt.Errorf("unknown tool %q", name)
	}
}
