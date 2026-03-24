package agent_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kimitsu-ai/ktsu/internal/runtime/agent"
	agentmcp "github.com/kimitsu-ai/ktsu/internal/runtime/agent/mcp"
)

func mcpClient() *agentmcp.Client {
	return agentmcp.New(http.DefaultClient)
}

// fakeGateway returns a handler that serves a scripted sequence of responses.
// Each call to the handler pops the next response from the slice.
func fakeGateway(t *testing.T, responses []map[string]any) *httptest.Server {
	t.Helper()
	idx := 0
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if idx >= len(responses) {
			t.Errorf("gateway called more times than expected (call %d)", idx+1)
			http.Error(w, "unexpected call", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(responses[idx])
		idx++
	}))
}

func baseReq() agent.InvokeRequest {
	return agent.InvokeRequest{
		RunID:     "run_test",
		StepID:    "step_test",
		AgentName: "test-agent",
		System:    "You are a test agent.",
		MaxTurns:  5,
		Model:     agent.ModelSpec{Group: "standard", MaxTokens: 512},
		Input:     map[string]any{"message": "hello"},
	}
}

func TestLoop_noTools(t *testing.T) {
	gw := fakeGateway(t, []map[string]any{
		{
			"content":        `{"category":"billing"}`,
			"model_resolved": "anthropic/claude-sonnet-4-6",
			"tokens_in":      100,
			"tokens_out":     20,
			"cost_usd":       0.001,
		},
	})
	defer gw.Close()

	loop := agent.NewLoop(gw.URL, mcpClient())
	payload := loop.Run(context.Background(), baseReq())

	if payload.Status != "ok" {
		t.Fatalf("expected status ok, got %s (error: %s)", payload.Status, payload.Error)
	}
	if payload.Output["category"] != "billing" {
		t.Errorf("unexpected output: %v", payload.Output)
	}
	if payload.Metrics.TokensIn != 100 {
		t.Errorf("expected tokens_in 100, got %d", payload.Metrics.TokensIn)
	}
	if payload.Metrics.ModelResolved != "anthropic/claude-sonnet-4-6" {
		t.Errorf("unexpected model_resolved: %s", payload.Metrics.ModelResolved)
	}
	if payload.Metrics.DurationMS < 0 {
		t.Error("expected non-negative duration_ms")
	}
}

func TestLoop_gatewayError(t *testing.T) {
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(map[string]any{
			"error":     "unknown_group",
			"message":   "group 'standard' not found",
			"retryable": false,
		})
	}))
	defer gw.Close()

	loop := agent.NewLoop(gw.URL, mcpClient())
	payload := loop.Run(context.Background(), baseReq())

	if payload.Status != "failed" {
		t.Fatalf("expected status failed, got %s", payload.Status)
	}
	if !strings.Contains(payload.Error, "unknown_group") {
		t.Errorf("expected error to mention unknown_group, got: %s", payload.Error)
	}
}

func TestLoop_invalidJSONOutput(t *testing.T) {
	gw := fakeGateway(t, []map[string]any{
		{
			"content":        "This is plain text, not JSON.",
			"model_resolved": "anthropic/claude-sonnet-4-6",
			"tokens_in":      50,
			"tokens_out":     10,
			"cost_usd":       0.0001,
		},
	})
	defer gw.Close()

	loop := agent.NewLoop(gw.URL, mcpClient())
	payload := loop.Run(context.Background(), baseReq())

	if payload.Status != "failed" {
		t.Fatalf("expected status failed, got %s", payload.Status)
	}
	if !strings.Contains(payload.Error, "not valid JSON") {
		t.Errorf("expected JSON parse error, got: %s", payload.Error)
	}
}

func TestLoop_forcedConclusionMessage(t *testing.T) {
	var capturedBodies []map[string]any
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		capturedBodies = append(capturedBodies, body)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"content":        `{"done":true}`,
			"model_resolved": "anthropic/claude-sonnet-4-6",
			"tokens_in":      10,
			"tokens_out":     5,
			"cost_usd":       0.0,
		})
	}))
	defer gw.Close()

	req := baseReq()
	req.MaxTurns = 1 // forces conclusion message on the first (and only) turn

	loop := agent.NewLoop(gw.URL, mcpClient())
	payload := loop.Run(context.Background(), req)

	if payload.Status != "ok" {
		t.Fatalf("expected ok, got %s: %s", payload.Status, payload.Error)
	}

	// The messages sent on the final turn should include the forced-conclusion message.
	if len(capturedBodies) != 1 {
		t.Fatalf("expected 1 gateway call, got %d", len(capturedBodies))
	}
	msgs, _ := capturedBodies[0]["messages"].([]any)
	lastMsg, _ := msgs[len(msgs)-1].(map[string]any)
	if !strings.Contains(lastMsg["content"].(string), "maximum number of tool calls") {
		t.Errorf("expected forced-conclusion message, got: %v", lastMsg["content"])
	}
}

func TestLoop_ktsuFieldsPassedThrough(t *testing.T) {
	gw := fakeGateway(t, []map[string]any{
		{
			"content":        `{"category":"billing","ktsu_confidence":0.92,"ktsu_flags":["low_risk"]}`,
			"model_resolved": "anthropic/claude-sonnet-4-6",
			"tokens_in":      80,
			"tokens_out":     15,
			"cost_usd":       0.0005,
		},
	})
	defer gw.Close()

	loop := agent.NewLoop(gw.URL, mcpClient())
	payload := loop.Run(context.Background(), baseReq())

	if payload.Status != "ok" {
		t.Fatalf("unexpected failure: %s", payload.Error)
	}
	// ktsu_* fields must be present in output (runtime is transparent to them)
	if _, ok := payload.Output["ktsu_confidence"]; !ok {
		t.Error("expected ktsu_confidence in output")
	}
	if _, ok := payload.Output["ktsu_flags"]; !ok {
		t.Error("expected ktsu_flags in output")
	}
	// Non-reserved field also present
	if payload.Output["category"] != "billing" {
		t.Errorf("unexpected category: %v", payload.Output["category"])
	}
}

// fakeMCPServer returns an MCP server that advertises toolName via tools/list
// and responds to tools/call with a single text content block.
func fakeMCPServer(t *testing.T, toolName, resultText string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		method, _ := req["method"].(string)
		switch method {
		case "tools/list":
			json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      1,
				"result": map[string]any{
					"tools": []map[string]any{
						{"name": toolName, "description": "test tool", "inputSchema": map[string]any{}},
					},
				},
			})
		default: // tools/call
			json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0",
				"id":      1,
				"result": map[string]any{
					"content": []map[string]any{
						{"type": "text", "text": resultText},
					},
				},
			})
		}
	}))
}

func TestLoop_withToolCalls(t *testing.T) {
	var capturedTurn2Body map[string]any
	mcp := fakeMCPServer(t, "kv-get", `"active"`)
	defer mcp.Close()

	// Turn 1 returns a tool call; turn 2 returns the final answer.
	idx := 0
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if idx == 0 {
			idx++
			json.NewEncoder(w).Encode(map[string]any{
				"content":        "",
				"model_resolved": "anthropic/claude-sonnet-4-6",
				"tokens_in":      100,
				"tokens_out":     20,
				"cost_usd":       0.001,
				"tool_calls": []map[string]any{
					{"id": "tc1", "name": "kv-get", "arguments": map[string]any{"key": "x"}},
				},
			})
			return
		}
		json.NewDecoder(r.Body).Decode(&capturedTurn2Body)
		json.NewEncoder(w).Encode(map[string]any{
			"content":        `{"result":"found"}`,
			"model_resolved": "anthropic/claude-sonnet-4-6",
			"tokens_in":      50,
			"tokens_out":     10,
			"cost_usd":       0.0005,
		})
	}))
	defer gw.Close()

	req := baseReq()
	req.ToolServers = []agent.ToolServerSpec{
		{Name: "kv", URL: mcp.URL, Allowlist: []string{"kv-get"}},
	}

	loop := agent.NewLoop(gw.URL, mcpClient())
	payload := loop.Run(context.Background(), req)

	if payload.Status != "ok" {
		t.Fatalf("expected ok, got %s: %s", payload.Status, payload.Error)
	}
	if payload.Output["result"] != "found" {
		t.Errorf("unexpected output: %v", payload.Output)
	}
	if payload.Metrics.ToolCalls != 1 {
		t.Errorf("expected 1 tool call, got %d", payload.Metrics.ToolCalls)
	}
	if payload.Metrics.TokensIn != 150 {
		t.Errorf("expected 150 cumulative tokens_in, got %d", payload.Metrics.TokensIn)
	}

	// Turn 2 messages should include the tool_use and tool_result blocks.
	msgs, _ := capturedTurn2Body["messages"].([]any)
	if len(msgs) < 4 {
		t.Fatalf("expected at least 4 messages on turn 2, got %d", len(msgs))
	}
	// messages[2] is the assistant tool_use, messages[3] is the user tool_result.
	assistantMsg, _ := msgs[2].(map[string]any)
	userMsg, _ := msgs[3].(map[string]any)
	if assistantMsg["role"] != "assistant" {
		t.Errorf("expected assistant role, got %v", assistantMsg["role"])
	}
	if userMsg["role"] != "user" {
		t.Errorf("expected user role for tool_result, got %v", userMsg["role"])
	}
}

func TestLoop_maxTurnsExceeded(t *testing.T) {
	// Gateway always returns a tool call, forcing the loop to exhaust max turns.
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"content":        "",
			"model_resolved": "anthropic/claude-sonnet-4-6",
			"tokens_in":      10,
			"tokens_out":     5,
			"cost_usd":       0.0001,
			"tool_calls": []map[string]any{
				{"id": "tc1", "name": "kv-get", "arguments": map[string]any{"key": "x"}},
			},
		})
	}))
	defer gw.Close()

	mcpSrv := fakeMCPServer(t, "kv-get", `"val"`)
	defer mcpSrv.Close()

	req := baseReq()
	req.MaxTurns = 3
	req.ToolServers = []agent.ToolServerSpec{
		{Name: "kv", URL: mcpSrv.URL, Allowlist: []string{"kv-get"}},
	}

	loop := agent.NewLoop(gw.URL, mcpClient())
	payload := loop.Run(context.Background(), req)

	if payload.Status != "failed" {
		t.Fatalf("expected failed, got %s", payload.Status)
	}
	if !strings.Contains(payload.Error, "max_turns_exceeded") {
		t.Errorf("expected max_turns_exceeded error, got: %s", payload.Error)
	}
}

func TestLoop_toolNotPermitted(t *testing.T) {
	// Gateway returns a tool call for a tool not in any server's allowlist.
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"content":        "",
			"model_resolved": "anthropic/claude-sonnet-4-6",
			"tokens_in":      10,
			"tokens_out":     5,
			"tool_calls": []map[string]any{
				{"id": "tc1", "name": "secret-tool", "arguments": nil},
			},
		})
	}))
	defer gw.Close()

	req := baseReq()
	// No tool servers configured → toolByName is empty.

	loop := agent.NewLoop(gw.URL, mcpClient())
	payload := loop.Run(context.Background(), req)

	if payload.Status != "failed" {
		t.Fatalf("expected failed, got %s", payload.Status)
	}
	if !strings.Contains(payload.Error, "tool_not_permitted") {
		t.Errorf("expected tool_not_permitted error, got: %s", payload.Error)
	}
}

func TestLoop_toolDiscoveryError(t *testing.T) {
	// MCP server that returns 500
	badMCP := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "broken", http.StatusInternalServerError)
	}))
	defer badMCP.Close()

	// Gateway should never be called
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("gateway should not have been called")
	}))
	defer gw.Close()

	req := baseReq()
	req.ToolServers = []agent.ToolServerSpec{
		{Name: "broken-server", URL: badMCP.URL, Allowlist: []string{"some-tool"}},
	}

	loop := agent.NewLoop(gw.URL, mcpClient())
	payload := loop.Run(context.Background(), req)

	if payload.Status != "failed" {
		t.Fatalf("expected failed, got %s", payload.Status)
	}
}
