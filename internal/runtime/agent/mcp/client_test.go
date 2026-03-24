package mcp_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kimitsu-ai/ktsu/internal/runtime/agent/mcp"
)

func newClient() *mcp.Client {
	return mcp.New(http.DefaultClient)
}

func rpcResponse(t *testing.T, result any) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result":  result,
		})
	}
}

func TestDiscoverTools_filtersAllowlist(t *testing.T) {
	srv := httptest.NewServer(rpcResponse(t, map[string]any{
		"tools": []map[string]any{
			{"name": "kv-get", "description": "get a value", "inputSchema": map[string]any{}},
			{"name": "kv-set", "description": "set a value", "inputSchema": map[string]any{}},
			{"name": "kv-delete", "description": "delete a value", "inputSchema": map[string]any{}},
		},
	}))
	defer srv.Close()

	c := newClient()
	tools, err := c.DiscoverTools(context.Background(), srv.URL, []string{"kv-get", "kv-set"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(tools))
	}
	if tools[0].Name != "kv-get" || tools[1].Name != "kv-set" {
		t.Errorf("unexpected tool names: %v, %v", tools[0].Name, tools[1].Name)
	}
}

func TestDiscoverTools_emptyAllowlist(t *testing.T) {
	// No HTTP call should be made; returns empty slice immediately.
	c := newClient()
	tools, err := c.DiscoverTools(context.Background(), "http://unreachable", []string{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tools) != 0 {
		t.Fatalf("expected 0 tools, got %d", len(tools))
	}
}

func TestDiscoverTools_serverError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := newClient()
	_, err := c.DiscoverTools(context.Background(), srv.URL, []string{"kv-get"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestCallTool_success(t *testing.T) {
	var capturedBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&capturedBody)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result": map[string]any{
				"content": []map[string]any{
					{"type": "text", "text": `{"value":"active"}`},
				},
			},
		})
	}))
	defer srv.Close()

	c := newClient()
	result, err := c.CallTool(context.Background(), srv.URL, "kv-get", map[string]any{"key": "user:123"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content block, got %d", len(result.Content))
	}
	if result.Content[0].Text != `{"value":"active"}` {
		t.Errorf("unexpected content: %s", result.Content[0].Text)
	}

	// Verify the correct JSON-RPC request was sent.
	params, _ := capturedBody["params"].(map[string]any)
	if params["name"] != "kv-get" {
		t.Errorf("expected tool name kv-get, got %v", params["name"])
	}
	args, _ := params["arguments"].(map[string]any)
	if args["key"] != "user:123" {
		t.Errorf("expected key user:123, got %v", args["key"])
	}
}

func TestCallTool_rpcError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"error":   map[string]any{"code": -32600, "message": "tool not found"},
		})
	}))
	defer srv.Close()

	c := newClient()
	_, err := c.CallTool(context.Background(), srv.URL, "missing-tool", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
