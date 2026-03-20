package transform

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jmespath/go-jmespath"
	mcp "github.com/kimitsu-ai/ktsu/pkg/mcp"
)

type TransformServer struct{}

func New() *TransformServer { return &TransformServer{} }

func (s *TransformServer) Name() string { return "ktsu/transform" }

func (s *TransformServer) Tools() []mcp.Tool {
	return []mcp.Tool{
		{Name: "transform_jmespath", Description: "Apply a JMESPath expression to a JSON value", InputSchema: mcp.ToolInputSchema{Type: "object", Properties: map[string]interface{}{"expression": map[string]interface{}{"type": "string"}, "value": map[string]interface{}{"type": "object"}}, Required: []string{"expression", "value"}}},
		{Name: "transform_map", Description: "Apply a set of JMESPath mappings to produce a new object", InputSchema: mcp.ToolInputSchema{Type: "object", Properties: map[string]interface{}{"mappings": map[string]interface{}{"type": "object"}, "value": map[string]interface{}{"type": "object"}}, Required: []string{"mappings", "value"}}},
		{Name: "transform_filter", Description: "Filter an array using a JMESPath condition", InputSchema: mcp.ToolInputSchema{Type: "object", Properties: map[string]interface{}{"condition": map[string]interface{}{"type": "string"}, "items": map[string]interface{}{"type": "array"}}, Required: []string{"condition", "items"}}},
	}
}

func (s *TransformServer) Call(_ context.Context, name string, input json.RawMessage) (json.RawMessage, error) {
	switch name {
	case "transform_jmespath":
		var args struct {
			Expression string `json:"expression"`
			Value      any    `json:"value"`
		}
		json.Unmarshal(input, &args)
		result, err := jmespath.Search(args.Expression, args.Value)
		if err != nil {
			return nil, fmt.Errorf("jmespath error: %w", err)
		}
		return json.Marshal(map[string]any{"result": result})

	case "transform_map":
		var args struct {
			Mappings map[string]string `json:"mappings"`
			Value    any               `json:"value"`
		}
		json.Unmarshal(input, &args)
		out := make(map[string]any, len(args.Mappings))
		for outKey, expr := range args.Mappings {
			result, err := jmespath.Search(expr, args.Value)
			if err != nil {
				return nil, fmt.Errorf("jmespath error for mapping %q: %w", outKey, err)
			}
			out[outKey] = result
		}
		return json.Marshal(map[string]any{"result": out})

	case "transform_filter":
		var args struct {
			Condition string `json:"condition"`
			Items     []any  `json:"items"`
		}
		json.Unmarshal(input, &args)
		expr := fmt.Sprintf("[?%s]", args.Condition)
		result, err := jmespath.Search(expr, args.Items)
		if err != nil {
			return nil, fmt.Errorf("jmespath filter error: %w", err)
		}
		return json.Marshal(map[string]any{"result": result})

	default:
		return nil, fmt.Errorf("unknown tool %q", name)
	}
}
