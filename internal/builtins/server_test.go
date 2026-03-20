package builtins

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	mcp "github.com/kimitsu-ai/ktsu/pkg/mcp"
)

// fakeServer is a test double for BuiltinServer.
type fakeServer struct {
	name  string
	tools []mcp.Tool
	// callFn lets individual tests control what Call returns.
	callFn func(name string, input json.RawMessage) (json.RawMessage, error)
}

func (f fakeServer) Name() string { return f.name }
func (f fakeServer) Tools() []mcp.Tool { return f.tools }
func (f fakeServer) Call(_ context.Context, name string, input json.RawMessage) (json.RawMessage, error) {
	if f.callFn != nil {
		return f.callFn(name, input)
	}
	return nil, ErrNotImplemented
}

// --- GET /health ---

func TestBuiltinHandler_healthReturns200(t *testing.T) {
	srv := httptest.NewServer(NewBuiltinHandler(fakeServer{name: "ktsu/test"}))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

// --- POST /tools/list ---

func TestBuiltinHandler_toolsListReturnsTools(t *testing.T) {
	tools := []mcp.Tool{
		{Name: "kv_get", Description: "Get a value"},
		{Name: "kv_set", Description: "Set a value"},
	}
	srv := httptest.NewServer(NewBuiltinHandler(fakeServer{name: "ktsu/kv", tools: tools}))
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/tools/list", "application/json", nil)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	var result mcp.ListToolsResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("could not decode response: %v", err)
	}
	if len(result.Tools) != 2 {
		t.Fatalf("expected 2 tools, got %d", len(result.Tools))
	}
	if result.Tools[0].Name != "kv_get" || result.Tools[1].Name != "kv_set" {
		t.Errorf("unexpected tools: %v", result.Tools)
	}
}

func TestBuiltinHandler_toolsListWithBadJSONReturns400(t *testing.T) {
	srv := httptest.NewServer(NewBuiltinHandler(fakeServer{name: "ktsu/kv"}))
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/tools/list", "application/json", bytes.NewBufferString("{bad json"))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

// --- POST /tools/call ---

func TestBuiltinHandler_toolsCallReturnsOutput(t *testing.T) {
	fake := fakeServer{
		name: "ktsu/kv",
		callFn: func(name string, _ json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`"hello"`), nil
		},
	}
	srv := httptest.NewServer(NewBuiltinHandler(fake))
	defer srv.Close()

	body, _ := json.Marshal(mcp.CallToolRequest{Name: "kv_get", Arguments: json.RawMessage(`{"key":"x"}`)})
	resp, err := http.Post(srv.URL+"/tools/call", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	var result mcp.CallToolResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("could not decode response: %v", err)
	}
	if result.IsError {
		t.Errorf("expected IsError=false, got true")
	}
	if len(result.Content) == 0 || result.Content[0].Text != `"hello"` {
		t.Errorf("unexpected content: %v", result.Content)
	}
}

func TestBuiltinHandler_toolsCallToolErrorSetsIsError(t *testing.T) {
	fake := fakeServer{
		name: "ktsu/kv",
		callFn: func(name string, _ json.RawMessage) (json.RawMessage, error) {
			return nil, ErrNotImplemented
		},
	}
	srv := httptest.NewServer(NewBuiltinHandler(fake))
	defer srv.Close()

	body, _ := json.Marshal(mcp.CallToolRequest{Name: "kv_get"})
	resp, err := http.Post(srv.URL+"/tools/call", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	var result mcp.CallToolResult
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("could not decode response: %v", err)
	}
	if !result.IsError {
		t.Errorf("expected IsError=true")
	}
	if len(result.Content) == 0 || result.Content[0].Text != ErrNotImplemented.Error() {
		t.Errorf("expected error text %q, got %v", ErrNotImplemented.Error(), result.Content)
	}
}

func TestBuiltinHandler_toolsCallWithBadJSONReturns400(t *testing.T) {
	srv := httptest.NewServer(NewBuiltinHandler(fakeServer{name: "ktsu/kv"}))
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/tools/call", "application/json", bytes.NewBufferString("{bad"))
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

func TestBuiltinHandler_healthIncludesServerName(t *testing.T) {
	srv := httptest.NewServer(NewBuiltinHandler(fakeServer{name: "ktsu/test"}))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("could not decode response: %v", err)
	}
	if body["server"] != "ktsu/test" {
		t.Errorf("expected server %q, got %q", "ktsu/test", body["server"])
	}
	if body["status"] != "ok" {
		t.Errorf("expected status %q, got %q", "ok", body["status"])
	}
}
