package log

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	mcp "github.com/kimitsu-ai/ktsu/pkg/mcp"
)

type entry struct {
	RunID   string `json:"run_id"`
	Level   string `json:"level"`
	Message string `json:"message"`
}

type LogServer struct {
	mu      sync.RWMutex
	entries []entry
}

func New() *LogServer { return &LogServer{} }

func (s *LogServer) Name() string { return "ktsu/log" }

func (s *LogServer) Tools() []mcp.Tool {
	return []mcp.Tool{
		{Name: "log_write", Description: "Write a log entry", InputSchema: mcp.ToolInputSchema{Type: "object", Properties: map[string]interface{}{"level": map[string]interface{}{"type": "string"}, "message": map[string]interface{}{"type": "string"}}, Required: []string{"message"}}},
		{Name: "log_read", Description: "Read log entries by run ID", InputSchema: mcp.ToolInputSchema{Type: "object", Properties: map[string]interface{}{"run_id": map[string]interface{}{"type": "string"}, "limit": map[string]interface{}{"type": "integer"}}, Required: []string{"run_id"}}},
		{Name: "log_tail", Description: "Tail the most recent log entries", InputSchema: mcp.ToolInputSchema{Type: "object", Properties: map[string]interface{}{"n": map[string]interface{}{"type": "integer"}}}},
	}
}

func (s *LogServer) Call(_ context.Context, name string, input json.RawMessage) (json.RawMessage, error) {
	switch name {
	case "log_write":
		var args map[string]string
		json.Unmarshal(input, &args)
		level := args["level"]
		if level == "" {
			level = "info"
		}
		s.mu.Lock()
		s.entries = append(s.entries, entry{RunID: args["run_id"], Level: level, Message: args["message"]})
		s.mu.Unlock()
		return json.Marshal(map[string]bool{"ok": true})

	case "log_read":
		var args map[string]string
		json.Unmarshal(input, &args)
		runID := args["run_id"]
		s.mu.RLock()
		var result []entry
		for _, e := range s.entries {
			if e.RunID == runID {
				result = append(result, e)
			}
		}
		s.mu.RUnlock()
		if result == nil {
			result = []entry{}
		}
		return json.Marshal(map[string][]entry{"entries": result})

	case "log_tail":
		var args map[string]int
		json.Unmarshal(input, &args)
		n := args["n"]
		s.mu.RLock()
		all := s.entries
		s.mu.RUnlock()
		if n <= 0 || n > len(all) {
			n = len(all)
		}
		tail := all[len(all)-n:]
		return json.Marshal(map[string][]entry{"entries": tail})

	default:
		return nil, fmt.Errorf("unknown tool %q", name)
	}
}
