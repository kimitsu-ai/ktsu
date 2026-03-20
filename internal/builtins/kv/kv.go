package kv

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	mcp "github.com/kimitsu-ai/ktsu/pkg/mcp"
)

type KVServer struct {
	mu    sync.RWMutex
	store map[string]string
}

func New() *KVServer { return &KVServer{store: make(map[string]string)} }

func (s *KVServer) Name() string { return "ktsu/kv" }

func (s *KVServer) Tools() []mcp.Tool {
	return []mcp.Tool{
		{
			Name:        "kv_get",
			Description: "Get a value by key",
			InputSchema: mcp.ToolInputSchema{
				Type:       "object",
				Properties: map[string]interface{}{"key": map[string]interface{}{"type": "string"}},
				Required:   []string{"key"},
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
				Type:       "object",
				Properties: map[string]interface{}{"key": map[string]interface{}{"type": "string"}},
				Required:   []string{"key"},
			},
		},
	}
}

func (s *KVServer) Call(_ context.Context, name string, input json.RawMessage) (json.RawMessage, error) {
	var args map[string]string
	if err := json.Unmarshal(input, &args); err != nil {
		return nil, fmt.Errorf("invalid input: %w", err)
	}

	switch name {
	case "kv_get":
		s.mu.RLock()
		v, ok := s.store[args["key"]]
		s.mu.RUnlock()
		if !ok {
			return nil, fmt.Errorf("key %q not found", args["key"])
		}
		return json.Marshal(map[string]string{"value": v})

	case "kv_set":
		s.mu.Lock()
		s.store[args["key"]] = args["value"]
		s.mu.Unlock()
		return json.Marshal(map[string]bool{"ok": true})

	case "kv_delete":
		s.mu.Lock()
		delete(s.store, args["key"])
		s.mu.Unlock()
		return json.Marshal(map[string]bool{"ok": true})

	default:
		return nil, fmt.Errorf("unknown tool %q", name)
	}
}
