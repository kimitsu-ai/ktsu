package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	mcp "github.com/kimitsu-ai/ktsu/pkg/mcp"
)

type memEntry struct {
	ID      string `json:"id"`
	Content string `json:"content"`
}

type MemoryServer struct {
	mu      sync.RWMutex
	entries map[string]memEntry
	seq     int
}

func New() *MemoryServer { return &MemoryServer{entries: make(map[string]memEntry)} }

func (s *MemoryServer) Name() string { return "ktsu/memory" }

func (s *MemoryServer) Tools() []mcp.Tool {
	return []mcp.Tool{
		{Name: "memory_store", Description: "Store a memory entry with optional metadata", InputSchema: mcp.ToolInputSchema{Type: "object", Properties: map[string]interface{}{"content": map[string]interface{}{"type": "string"}, "metadata": map[string]interface{}{"type": "object"}}, Required: []string{"content"}}},
		{Name: "memory_retrieve", Description: "Retrieve a memory entry by ID", InputSchema: mcp.ToolInputSchema{Type: "object", Properties: map[string]interface{}{"id": map[string]interface{}{"type": "string"}}, Required: []string{"id"}}},
		{Name: "memory_search", Description: "Search memory entries by query", InputSchema: mcp.ToolInputSchema{Type: "object", Properties: map[string]interface{}{"query": map[string]interface{}{"type": "string"}, "limit": map[string]interface{}{"type": "integer"}}, Required: []string{"query"}}},
		{Name: "memory_forget", Description: "Delete a memory entry by ID", InputSchema: mcp.ToolInputSchema{Type: "object", Properties: map[string]interface{}{"id": map[string]interface{}{"type": "string"}}, Required: []string{"id"}}},
	}
}

func (s *MemoryServer) Call(_ context.Context, name string, input json.RawMessage) (json.RawMessage, error) {
	switch name {
	case "memory_store":
		var args map[string]string
		json.Unmarshal(input, &args)
		s.mu.Lock()
		s.seq++
		id := fmt.Sprintf("mem_%d", s.seq)
		s.entries[id] = memEntry{ID: id, Content: args["content"]}
		s.mu.Unlock()
		return json.Marshal(map[string]string{"id": id})

	case "memory_retrieve":
		var args map[string]string
		json.Unmarshal(input, &args)
		s.mu.RLock()
		e, ok := s.entries[args["id"]]
		s.mu.RUnlock()
		if !ok {
			return nil, fmt.Errorf("memory entry %q not found", args["id"])
		}
		return json.Marshal(map[string]string{"id": e.ID, "content": e.Content})

	case "memory_search":
		var args map[string]string
		json.Unmarshal(input, &args)
		query := args["query"]
		s.mu.RLock()
		var results []map[string]string
		for _, e := range s.entries {
			if strings.Contains(e.Content, query) {
				results = append(results, map[string]string{"id": e.ID, "content": e.Content})
			}
		}
		s.mu.RUnlock()
		if results == nil {
			results = []map[string]string{}
		}
		return json.Marshal(map[string][]map[string]string{"results": results})

	case "memory_forget":
		var args map[string]string
		json.Unmarshal(input, &args)
		s.mu.Lock()
		delete(s.entries, args["id"])
		s.mu.Unlock()
		return json.Marshal(map[string]bool{"ok": true})

	default:
		return nil, fmt.Errorf("unknown tool %q", name)
	}
}
