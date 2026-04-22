package mcp_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

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
	tools, err := c.DiscoverTools(context.Background(), srv.URL, "", "", "", []string{"kv-get", "kv-set"})
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
	tools, err := c.DiscoverTools(context.Background(), "http://unreachable", "", "", "", []string{})
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
	_, err := c.DiscoverTools(context.Background(), srv.URL, "", "", "", []string{"kv-get"})
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
	result, err := c.CallTool(context.Background(), srv.URL, "", "", "", "kv-get", map[string]any{"key": "user:123"})
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

func TestInitialize_sendsConfigInParams(t *testing.T) {
	var receivedConfig map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if params, ok := req["params"].(map[string]any); ok {
			receivedConfig, _ = params["config"].(map[string]any)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result":  map[string]any{},
		})
	}))
	defer srv.Close()

	c := newClient()
	err := c.Initialize(context.Background(), srv.URL, "", "", "", map[string]any{"namespace": "user-123"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if receivedConfig == nil {
		t.Fatal("expected config to be sent in initialize params")
	}
	if receivedConfig["namespace"] != "user-123" {
		t.Errorf("got config %v, want namespace=user-123", receivedConfig)
	}
}

func TestInitialize_withBearerAuth(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{}})
	}))
	defer srv.Close()

	c := newClient()
	err := c.Initialize(context.Background(), srv.URL, "", "Authorization", "Bearer my-token", map[string]any{"key": "val"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotAuth != "Bearer my-token" {
		t.Errorf("got Authorization %q want %q", gotAuth, "Bearer my-token")
	}
}

func TestDiscoverTools_withCustomHeader(t *testing.T) {
	var gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Api-Key")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result": map[string]any{"tools": []map[string]any{
				{"name": "search", "description": "search", "inputSchema": map[string]any{}},
			}},
		})
	}))
	defer srv.Close()

	c := newClient()
	_, err := c.DiscoverTools(context.Background(), srv.URL, "", "X-Api-Key", "secret-key", []string{"search"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotHeader != "secret-key" {
		t.Errorf("got X-Api-Key header %q, want %q", gotHeader, "secret-key")
	}
}

func TestCallTool_withRawAuth(t *testing.T) {
	var gotHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("X-Token")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result":  map[string]any{"content": []map[string]any{{"type": "text", "text": "ok"}}},
		})
	}))
	defer srv.Close()

	c := newClient()
	_, err := c.CallTool(context.Background(), srv.URL, "", "X-Token", "rawvalue", "kv-get", map[string]any{"key": "k"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotHeader != "rawvalue" {
		t.Errorf("got X-Token header %q, want %q", gotHeader, "rawvalue")
	}
}

func TestRpc_sendsMCPProtocolVersionHeader(t *testing.T) {
	var gotVersion string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotVersion = r.Header.Get("MCP-Protocol-Version")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": 1, "result": map[string]any{}})
	}))
	defer srv.Close()

	c := newClient()
	err := c.Initialize(context.Background(), srv.URL, "", "", "", map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotVersion != "2024-11-05" {
		t.Errorf("got MCP-Protocol-Version %q, want %q", gotVersion, "2024-11-05")
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
	_, err := c.CallTool(context.Background(), srv.URL, "", "", "", "missing-tool", nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestRpc_withSSEHandshake(t *testing.T) {
	var sseCalled, messageCalled bool
	var capturedBody map[string]any

	// The "bridge" server that handles the actual RPC
	bridgeSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		messageCalled = true
		json.NewDecoder(r.Body).Decode(&capturedBody)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      1,
			"result": map[string]any{
				"content": []map[string]any{
					{"type": "text", "text": "pong"},
				},
			},
		})
	}))
	defer bridgeSrv.Close()

	// The base server that handles /sse discovery
	baseSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/sse" {
			sseCalled = true
			w.Header().Set("Content-Type", "text/event-stream")
			fmt.Fprintf(w, "event: endpoint\ndata: %s\n\n", bridgeSrv.URL)
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer baseSrv.Close()

	c := newClient()
	result, err := c.CallTool(context.Background(), baseSrv.URL, "", "", "", "ping", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !sseCalled {
		t.Error("expected /sse handshake but it was not called")
	}
	if !messageCalled {
		t.Error("expected bridge URL to be called but it was not")
	}
	if len(result.Content) == 0 || result.Content[0].Text != "pong" {
		t.Errorf("got result %+v, want text 'pong'", result)
	}
}
func TestRpc_withAsyncSSEResponse(t *testing.T) {
	var sseCalled, messageCalled bool
	pushChan := make(chan bool, 1)

	// The base server that handles /sse discovery AND sends the async response
	baseSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/sse" {
			sseCalled = true
			w.Header().Set("Content-Type", "text/event-stream")
			// Flush headers immediately
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			// Send the endpoint event
			fmt.Fprintf(w, "event: endpoint\ndata: /message?sessionId=123\n\n")
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}

			// Wait for the POST request to arrive before sending the tool response
			select {
			case <-pushChan:
				// Send a mock response with ID 1 (Discovery starts at 1)
				fmt.Fprintf(w, "data: {\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{\"tools\":[{\"name\":\"test-tool\",\"description\":\"desc\",\"inputSchema\":{}}]}}\n\n")
				if f, ok := w.(http.Flusher); ok {
					f.Flush()
				}
			case <-time.After(5 * time.Second):
				// timeout
			}

			// Keep alive
			for {
				select {
				case <-r.Context().Done():
					return
				case <-time.After(1 * time.Second):
					if _, err := fmt.Fprint(w, ":ping\n\n"); err != nil {
						return
					}
				}
			}
		}
		if r.URL.Path == "/message" {
			messageCalled = true
			w.WriteHeader(http.StatusAccepted) // 202 Accepted
			pushChan <- true
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer baseSrv.Close()

	c := newClient()
	defer c.Close()
	tools, err := c.DiscoverTools(context.Background(), baseSrv.URL, "", "", "", []string{"test-tool"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !sseCalled {
		t.Error("expected /sse handshake but it was not called")
	}
	if !messageCalled {
		t.Error("expected bridge URL to be called but it was not")
	}
	if len(tools) != 1 || tools[0].Name != "test-tool" {
		t.Errorf("got tools %+v, want 'test-tool'", tools)
	}
}

func TestRpc_withRetryOnExpiration(t *testing.T) {
	var sseCount, messageCount atomic.Int64

	// The mock server starts returning 400 for a session after it's been used.
	baseSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/sse" {
			count := sseCount.Add(1)
			w.Header().Set("Content-Type", "text/event-stream")
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			// Send a unique bridge URL each time
			fmt.Fprintf(w, "event: endpoint\ndata: /message/%d\n\n", count)
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			// Keep alive
			for {
				select {
				case <-r.Context().Done():
					return
				case <-time.After(100 * time.Millisecond):
					fmt.Fprint(w, ":ping\n\n")
					if f, ok := w.(http.Flusher); ok {
						f.Flush()
					}
				}
			}
		}

		if strings.HasPrefix(r.URL.Path, "/message/") {
			count := messageCount.Add(1)
			path := r.URL.Path
			// If it's the FIRST bridge URL (/message/1), return 400 for any subsequent calls
			// This simulates a session that worked once but then expired.
			if path == "/message/1" && count > 1 {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusOK)
			// Return a mock result immediately for simplicity in this retry test
			fmt.Fprintln(w, `{"jsonrpc":"2.0","result":{"tools":[{"name":"test-tool","description":"desc","inputSchema":{}}]}}`)
			return
		}
	}))
	defer baseSrv.Close()

	c := mcp.New(http.DefaultClient)
	defer c.Close()

	// 1. First call should trigger SSE 1 and Message 1
	ctx1, cancel1 := context.WithTimeout(context.Background(), 1*time.Second)
	_, err := c.DiscoverTools(ctx1, baseSrv.URL, "", "", "", []string{"test-tool"})
	cancel1()
	if err != nil {
		t.Errorf("first call failed: %v", err)
	}

	if sseCount.Load() != 1 {
		t.Errorf("expected 1 handshake, got %d", sseCount.Load())
	}
	if messageCount.Load() != 1 {
		t.Errorf("expected 1 tool call, got %d", messageCount.Load())
	}

	// 2. Second call. We simulate the server returning 400 for /message/1.
	// The client should catch the 400, clear cache, and retry with /message/2.
	ctx2, cancel2 := context.WithTimeout(context.Background(), 1*time.Second)
	_, err = c.DiscoverTools(ctx2, baseSrv.URL, "", "", "", []string{"test-tool"})
	cancel2()
	if err != nil {
		t.Errorf("second call (retry) failed: %v", err)
	}

	if sseCount.Load() != 2 {
		t.Errorf("expected 2 handshakes after retry, got %d", sseCount.Load())
	}
	// 1 (first run) + 1 (failed retry) + 1 (successful second run) = 3
	if messageCount.Load() != 3 {
		t.Errorf("expected 3 total message calls (1 success, 1 failure, 1 retry success), got %d", messageCount.Load())
	}
}

func TestDiscoverTools_wildcard(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/sse", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, "event: endpoint\ndata: /message\n\n")
	})
	mux.HandleFunc("/message", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, `{"jsonrpc":"2.0","result":{"tools":[
			{"name":"test_get","description":"desc","inputSchema":{}},
			{"name":"test_set","description":"desc","inputSchema":{}},
			{"name":"other_tool","description":"desc","inputSchema":{}}
		]}}`)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := mcp.New(http.DefaultClient)
	defer c.Close()

	// Pattern kv_* should match kv_get
	tools, err := c.DiscoverTools(context.Background(), srv.URL, "", "", "", []string{"test_*"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(tools) != 2 {
		t.Errorf("expected 2 tools (test_get, test_set), got %d: %+v", len(tools), tools)
	}
	
	// Double check exact match still works
	tools, err = c.DiscoverTools(context.Background(), srv.URL, "", "", "", []string{"other_tool"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "other_tool" {
		t.Errorf("expected exactly 'other_tool', got: %+v", tools)
	}
}
