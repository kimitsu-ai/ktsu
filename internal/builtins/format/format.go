package format

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"text/template"

	mcp "github.com/kimitsu-ai/ktsu/pkg/mcp"
	"gopkg.in/yaml.v3"
)

type FormatServer struct{}

func New() *FormatServer { return &FormatServer{} }

func (s *FormatServer) Name() string { return "ktsu/format" }

func (s *FormatServer) Tools() []mcp.Tool {
	return []mcp.Tool{
		{Name: "format_json", Description: "Format a value as pretty-printed JSON", InputSchema: mcp.ToolInputSchema{Type: "object", Properties: map[string]interface{}{"value": map[string]interface{}{"type": "string"}, "indent": map[string]interface{}{"type": "integer"}}, Required: []string{"value"}}},
		{Name: "format_yaml", Description: "Format a value as YAML", InputSchema: mcp.ToolInputSchema{Type: "object", Properties: map[string]interface{}{"value": map[string]interface{}{"type": "string"}}, Required: []string{"value"}}},
		{Name: "format_template", Description: "Render a Go text/template with provided data", InputSchema: mcp.ToolInputSchema{Type: "object", Properties: map[string]interface{}{"template": map[string]interface{}{"type": "string"}, "data": map[string]interface{}{"type": "object"}}, Required: []string{"template", "data"}}},
	}
}

func (s *FormatServer) Call(_ context.Context, name string, input json.RawMessage) (json.RawMessage, error) {
	switch name {
	case "format_json":
		var args map[string]string
		json.Unmarshal(input, &args)
		var v any
		if err := json.Unmarshal([]byte(args["value"]), &v); err != nil {
			return nil, fmt.Errorf("invalid JSON: %w", err)
		}
		pretty, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return nil, err
		}
		return json.Marshal(map[string]string{"output": string(pretty)})

	case "format_yaml":
		var args map[string]string
		json.Unmarshal(input, &args)
		var v any
		if err := json.Unmarshal([]byte(args["value"]), &v); err != nil {
			return nil, fmt.Errorf("invalid JSON: %w", err)
		}
		out, err := yaml.Marshal(v)
		if err != nil {
			return nil, err
		}
		return json.Marshal(map[string]string{"output": string(out)})

	case "format_template":
		var args struct {
			Template string         `json:"template"`
			Data     map[string]any `json:"data"`
		}
		json.Unmarshal(input, &args)
		tmpl, err := template.New("").Parse(args.Template)
		if err != nil {
			return nil, fmt.Errorf("invalid template: %w", err)
		}
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, args.Data); err != nil {
			return nil, fmt.Errorf("template execution failed: %w", err)
		}
		return json.Marshal(map[string]string{"output": buf.String()})

	default:
		return nil, fmt.Errorf("unknown tool %q", name)
	}
}
