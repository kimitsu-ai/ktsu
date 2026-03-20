package blob

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	mcp "github.com/kimitsu-ai/ktsu/pkg/mcp"
)

type BlobServer struct {
	mu    sync.RWMutex
	store map[string]string
}

func New() *BlobServer { return &BlobServer{store: make(map[string]string)} }

func (s *BlobServer) Name() string { return "ktsu/blob" }

func (s *BlobServer) Tools() []mcp.Tool {
	return []mcp.Tool{
		{Name: "blob_get", Description: "Get a blob by key", InputSchema: mcp.ToolInputSchema{Type: "object", Properties: map[string]interface{}{"key": map[string]interface{}{"type": "string"}}, Required: []string{"key"}}},
		{Name: "blob_put", Description: "Store a blob by key", InputSchema: mcp.ToolInputSchema{Type: "object", Properties: map[string]interface{}{"key": map[string]interface{}{"type": "string"}, "data": map[string]interface{}{"type": "string"}}, Required: []string{"key", "data"}}},
		{Name: "blob_delete", Description: "Delete a blob by key", InputSchema: mcp.ToolInputSchema{Type: "object", Properties: map[string]interface{}{"key": map[string]interface{}{"type": "string"}}, Required: []string{"key"}}},
		{Name: "blob_list", Description: "List blobs by prefix", InputSchema: mcp.ToolInputSchema{Type: "object", Properties: map[string]interface{}{"prefix": map[string]interface{}{"type": "string"}}}},
	}
}

func (s *BlobServer) Call(_ context.Context, name string, input json.RawMessage) (json.RawMessage, error) {
	var args map[string]string
	json.Unmarshal(input, &args)

	switch name {
	case "blob_get":
		s.mu.RLock()
		v, ok := s.store[args["key"]]
		s.mu.RUnlock()
		if !ok {
			return nil, fmt.Errorf("key %q not found", args["key"])
		}
		return json.Marshal(map[string]string{"data": v})

	case "blob_put":
		s.mu.Lock()
		s.store[args["key"]] = args["data"]
		s.mu.Unlock()
		return json.Marshal(map[string]bool{"ok": true})

	case "blob_delete":
		s.mu.Lock()
		delete(s.store, args["key"])
		s.mu.Unlock()
		return json.Marshal(map[string]bool{"ok": true})

	case "blob_list":
		prefix := args["prefix"]
		s.mu.RLock()
		var keys []string
		for k := range s.store {
			if strings.HasPrefix(k, prefix) {
				keys = append(keys, k)
			}
		}
		s.mu.RUnlock()
		if keys == nil {
			keys = []string{}
		}
		return json.Marshal(map[string][]string{"keys": keys})

	default:
		return nil, fmt.Errorf("unknown tool %q", name)
	}
}
