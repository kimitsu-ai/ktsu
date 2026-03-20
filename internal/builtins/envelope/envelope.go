package envelope

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	mcp "github.com/kimitsu-ai/ktsu/pkg/mcp"
)

type EnvelopeServer struct {
	mu   sync.RWMutex
	data map[string]any
}

func New() *EnvelopeServer { return &EnvelopeServer{data: make(map[string]any)} }

func (s *EnvelopeServer) Name() string { return "ktsu/envelope" }

func (s *EnvelopeServer) Tools() []mcp.Tool {
	return []mcp.Tool{
		{Name: "envelope_get", Description: "Get a field from the current run envelope", InputSchema: mcp.ToolInputSchema{Type: "object", Properties: map[string]interface{}{"path": map[string]interface{}{"type": "string"}}, Required: []string{"path"}}},
		{Name: "envelope_set", Description: "Set a field in the current run envelope", InputSchema: mcp.ToolInputSchema{Type: "object", Properties: map[string]interface{}{"path": map[string]interface{}{"type": "string"}, "value": map[string]interface{}{"type": "string"}}, Required: []string{"path", "value"}}},
		{Name: "envelope_append", Description: "Append a value to an array field in the current run envelope", InputSchema: mcp.ToolInputSchema{Type: "object", Properties: map[string]interface{}{"path": map[string]interface{}{"type": "string"}, "value": map[string]interface{}{"type": "string"}}, Required: []string{"path", "value"}}},
	}
}

func (s *EnvelopeServer) Call(_ context.Context, name string, input json.RawMessage) (json.RawMessage, error) {
	var args map[string]string
	json.Unmarshal(input, &args)

	switch name {
	case "envelope_get":
		s.mu.RLock()
		v, ok := s.data[args["path"]]
		s.mu.RUnlock()
		if !ok {
			return nil, fmt.Errorf("path %q not found in envelope", args["path"])
		}
		return json.Marshal(map[string]any{"value": v})

	case "envelope_set":
		s.mu.Lock()
		s.data[args["path"]] = args["value"]
		s.mu.Unlock()
		return json.Marshal(map[string]bool{"ok": true})

	case "envelope_append":
		s.mu.Lock()
		existing, _ := s.data[args["path"]].([]string)
		s.data[args["path"]] = append(existing, args["value"])
		s.mu.Unlock()
		return json.Marshal(map[string]bool{"ok": true})

	default:
		return nil, fmt.Errorf("unknown tool %q", name)
	}
}
