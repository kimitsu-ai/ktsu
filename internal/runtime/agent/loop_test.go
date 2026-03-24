package agent_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kimitsu-ai/ktsu/internal/runtime/agent"
	"github.com/kimitsu-ai/ktsu/internal/runtime/agent/mcp"
)

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

	loop := agent.NewLoop(gw.URL, mcp.New(http.DefaultClient))
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

	loop := agent.NewLoop(gw.URL, mcp.New(http.DefaultClient))
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

	loop := agent.NewLoop(gw.URL, mcp.New(http.DefaultClient))
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

	loop := agent.NewLoop(gw.URL, mcp.New(http.DefaultClient))
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

	loop := agent.NewLoop(gw.URL, mcp.New(http.DefaultClient))
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

	loop := agent.NewLoop(gw.URL, mcp.New(http.DefaultClient))
	payload := loop.Run(context.Background(), req)

	if payload.Status != "failed" {
		t.Fatalf("expected failed, got %s", payload.Status)
	}
}
