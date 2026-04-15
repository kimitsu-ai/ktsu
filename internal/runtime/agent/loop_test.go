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

func TestLoop_invalidJSONOutputExhausesTurns(t *testing.T) {
	// Gateway always returns plain text; loop should exhaust max turns.
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"content":        "I cannot produce JSON right now.",
			"model_resolved": "anthropic/claude-sonnet-4-6",
			"tokens_in":      50,
			"tokens_out":     10,
			"cost_usd":       0.0001,
		})
	}))
	defer gw.Close()

	req := baseReq()
	req.MaxTurns = 3

	loop := agent.NewLoop(gw.URL, mcpClient())
	payload := loop.Run(context.Background(), req)

	if payload.Status != "failed" {
		t.Fatalf("expected status failed, got %s", payload.Status)
	}
	if !strings.Contains(payload.Error, "max_turns_exceeded") {
		t.Errorf("expected max_turns_exceeded error, got: %s", payload.Error)
	}
	if !strings.Contains(payload.RawOutput, "I cannot produce JSON") {
		t.Errorf("expected raw_output to contain last invalid LLM response, got: %s", payload.RawOutput)
	}
}

func TestLoop_jsonRetrySucceeds(t *testing.T) {
	// First response is plain text; second response is valid JSON.
	var capturedBodies []map[string]any
	idx := 0
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		capturedBodies = append(capturedBodies, body)

		w.Header().Set("Content-Type", "application/json")
		content := "I found the answer is 42."
		if idx > 0 {
			content = `{"answer":42}`
		}
		idx++
		json.NewEncoder(w).Encode(map[string]any{
			"content":        content,
			"model_resolved": "anthropic/claude-sonnet-4-6",
			"tokens_in":      50,
			"tokens_out":     10,
			"cost_usd":       0.0001,
		})
	}))
	defer gw.Close()

	loop := agent.NewLoop(gw.URL, mcpClient())
	payload := loop.Run(context.Background(), baseReq())

	if payload.Status != "ok" {
		t.Fatalf("expected ok after retry, got %s: %s", payload.Status, payload.Error)
	}
	if payload.Output["answer"] != float64(42) {
		t.Errorf("unexpected output: %v", payload.Output)
	}
	if len(capturedBodies) != 2 {
		t.Errorf("expected 2 gateway calls, got %d", len(capturedBodies))
	}
	// Second call should include the correction message asking for valid JSON.
	msgs, _ := capturedBodies[1]["messages"].([]any)
	lastMsg, _ := msgs[len(msgs)-1].(map[string]any)
	if !strings.Contains(lastMsg["content"].(string), "not valid JSON") {
		t.Errorf("expected correction message in retry, got: %v", lastMsg["content"])
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

	// Tools must be omitted on the forced-conclusion turn so the LLM cannot make tool calls.
	tools, _ := capturedBodies[0]["tools"].([]any)
	if len(tools) != 0 {
		t.Errorf("expected no tools on forced-conclusion turn, got %d", len(tools))
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

func TestLoop_freeJSONCorrectionAfterMaxTurns(t *testing.T) {
	// Agent uses all turns on tool calls, then the forced-conclusion turn
	// produces invalid JSON. The free correction turn should rescue it.
	mcpSrv := fakeMCPServer(t, "kv-get", `"val"`)
	defer mcpSrv.Close()

	callCount := 0
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		switch callCount {
		case 1: // Turn 1: tool call
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
		case 2: // Turn 2 (max_turns): forced conclusion, but bad JSON
			json.NewEncoder(w).Encode(map[string]any{
				"content":        "Here is the result: {\"status\":\"ok\"}",
				"model_resolved": "anthropic/claude-sonnet-4-6",
				"tokens_in":      50,
				"tokens_out":     10,
				"cost_usd":       0.0005,
			})
		case 3: // Free correction turn: valid JSON
			json.NewEncoder(w).Encode(map[string]any{
				"content":        `{"status":"ok"}`,
				"model_resolved": "anthropic/claude-sonnet-4-6",
				"tokens_in":      30,
				"tokens_out":     8,
				"cost_usd":       0.0003,
			})
		default:
			t.Errorf("unexpected gateway call %d", callCount)
		}
	}))
	defer gw.Close()

	req := baseReq()
	req.MaxTurns = 2
	req.ToolServers = []agent.ToolServerSpec{
		{Name: "kv", URL: mcpSrv.URL, Allowlist: []string{"kv-get"}},
	}

	loop := agent.NewLoop(gw.URL, mcpClient())
	payload := loop.Run(context.Background(), req)

	if payload.Status != "ok" {
		t.Fatalf("expected ok after free correction, got %s: %s", payload.Status, payload.Error)
	}
	if payload.Output["status"] != "ok" {
		t.Errorf("unexpected output: %v", payload.Output)
	}
	if callCount != 3 {
		t.Errorf("expected 3 gateway calls (tool turn + forced conclusion + free correction), got %d", callCount)
	}
	if payload.Metrics.LLMCalls != 3 {
		t.Errorf("expected 3 LLM calls, got %d", payload.Metrics.LLMCalls)
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

func TestLoop_markdownCodeFenceStripped(t *testing.T) {
	gw := fakeGateway(t, []map[string]any{
		{
			"content":        "```json\n{\"result\":\"ok\"}\n```",
			"model_resolved": "anthropic/claude-sonnet-4-6",
			"tokens_in":      30,
			"tokens_out":     10,
			"cost_usd":       0.0001,
		},
	})
	defer gw.Close()

	loop := agent.NewLoop(gw.URL, mcpClient())
	payload := loop.Run(context.Background(), baseReq())

	if payload.Status != "ok" {
		t.Fatalf("expected ok, got %s: %s", payload.Status, payload.Error)
	}
	if payload.Output["result"] != "ok" {
		t.Errorf("unexpected output: %v", payload.Output)
	}
}

func TestLoop_codeFenceWithoutLanguageTag(t *testing.T) {
	gw := fakeGateway(t, []map[string]any{
		{
			"content":        "```\n{\"result\":\"ok\"}\n```",
			"model_resolved": "anthropic/claude-sonnet-4-6",
			"tokens_in":      30,
			"tokens_out":     10,
			"cost_usd":       0.0001,
		},
	})
	defer gw.Close()

	loop := agent.NewLoop(gw.URL, mcpClient())
	payload := loop.Run(context.Background(), baseReq())

	if payload.Status != "ok" {
		t.Fatalf("expected ok, got %s: %s", payload.Status, payload.Error)
	}
	if payload.Output["result"] != "ok" {
		t.Errorf("unexpected output: %v", payload.Output)
	}
}

func TestLoop_outputSchemaInjectedInSystemPrompt(t *testing.T) {
	var capturedSystemPrompt string
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if msgs, ok := body["messages"].([]any); ok && len(msgs) > 0 {
			if first, ok := msgs[0].(map[string]any); ok {
				capturedSystemPrompt, _ = first["content"].(string)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"content":        `{"answer":"yes"}`,
			"model_resolved": "anthropic/claude-sonnet-4-6",
			"tokens_in":      10,
			"tokens_out":     5,
			"cost_usd":       0.0,
		})
	}))
	defer gw.Close()

	req := baseReq()
	req.OutputSchema = map[string]any{
		"type":     "object",
		"required": []string{"answer"},
		"properties": map[string]any{
			"answer": map[string]any{"type": "string"},
		},
	}

	loop := agent.NewLoop(gw.URL, mcpClient())
	payload := loop.Run(context.Background(), req)

	if payload.Status != "ok" {
		t.Fatalf("expected ok, got %s: %s", payload.Status, payload.Error)
	}
	if !strings.Contains(capturedSystemPrompt, "JSON schema") {
		t.Errorf("expected system prompt to contain schema, got: %s", capturedSystemPrompt)
	}
	if !strings.Contains(capturedSystemPrompt, `"answer"`) {
		t.Errorf("expected system prompt to contain schema field 'answer', got: %s", capturedSystemPrompt)
	}
}

func TestLoop_noOutputSchemaNoSchemaInPrompt(t *testing.T) {
	var capturedSystemPrompt string
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if msgs, ok := body["messages"].([]any); ok && len(msgs) > 0 {
			if first, ok := msgs[0].(map[string]any); ok {
				capturedSystemPrompt, _ = first["content"].(string)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"content":        `{"answer":"yes"}`,
			"model_resolved": "anthropic/claude-sonnet-4-6",
			"tokens_in":      10,
			"tokens_out":     5,
			"cost_usd":       0.0,
		})
	}))
	defer gw.Close()

	loop := agent.NewLoop(gw.URL, mcpClient())
	payload := loop.Run(context.Background(), baseReq())

	if payload.Status != "ok" {
		t.Fatalf("expected ok, got %s: %s", payload.Status, payload.Error)
	}
	if strings.Contains(capturedSystemPrompt, "JSON schema") {
		t.Errorf("expected no schema in system prompt when OutputSchema is nil, got: %s", capturedSystemPrompt)
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

func TestLoop_messagesAlwaysSetOnSuccess(t *testing.T) {
	gw := fakeGateway(t, []map[string]any{
		{
			"content":        `{"result":"ok"}`,
			"model_resolved": "anthropic/claude-sonnet-4-6",
			"tokens_in":      10,
			"tokens_out":     5,
			"cost_usd":       0.0,
		},
	})
	defer gw.Close()

	loop := agent.NewLoop(gw.URL, mcpClient())
	payload := loop.Run(context.Background(), baseReq())

	if payload.Status != "ok" {
		t.Fatalf("unexpected failure: %s", payload.Error)
	}
	if len(payload.Messages) == 0 {
		t.Error("expected messages to be set in payload")
	}
}

func TestLoop_pendingApprovalTriggered(t *testing.T) {
	mcpSrv := fakeMCPServer(t, "delete-user", `"deleted"`)
	defer mcpSrv.Close()

	// Gateway returns a tool call for delete-user.
	gw := fakeGateway(t, []map[string]any{
		{
			"content":        "",
			"model_resolved": "anthropic/claude-sonnet-4-6",
			"tokens_in":      100,
			"tokens_out":     20,
			"cost_usd":       0.001,
			"tool_calls": []map[string]any{
				{"id": "tc1", "name": "delete-user", "arguments": map[string]any{"id": "42"}},
			},
		},
	})
	defer gw.Close()

	req := baseReq()
	req.ToolServers = []agent.ToolServerSpec{
		{
			Name:      "user-svc",
			URL:       mcpSrv.URL,
			Allowlist: []string{"delete-user"},
			ApprovalRules: []agent.ToolApprovalRule{
				{Pattern: "delete-*", OnReject: "fail", TimeoutMS: 30000, TimeoutBehavior: "reject"},
			},
		},
	}

	loop := agent.NewLoop(gw.URL, mcpClient())
	payload := loop.Run(context.Background(), req)

	if payload.Status != "pending_approval" {
		t.Fatalf("expected pending_approval, got %s (error: %s)", payload.Status, payload.Error)
	}
	if payload.PendingApproval == nil {
		t.Fatal("expected PendingApproval to be set")
	}
	if payload.PendingApproval.ToolName != "delete-user" {
		t.Errorf("expected tool_name=delete-user, got %s", payload.PendingApproval.ToolName)
	}
	if payload.PendingApproval.ToolUseID != "tc1" {
		t.Errorf("expected tool_use_id=tc1, got %s", payload.PendingApproval.ToolUseID)
	}
	if payload.PendingApproval.OnReject != "fail" {
		t.Errorf("expected on_reject=fail, got %s", payload.PendingApproval.OnReject)
	}
	if payload.OriginalRequest == nil {
		t.Error("expected OriginalRequest to be set on pending_approval")
	}
	if len(payload.Messages) == 0 {
		t.Error("expected messages to be set for resume")
	}
	// The last message should be the assistant tool_use block.
	lastMsg := payload.Messages[len(payload.Messages)-1]
	if lastMsg.Role != "assistant" {
		t.Errorf("expected last message to be assistant tool_use, got role=%s", lastMsg.Role)
	}
}

func TestLoop_resumeWithPreApprovedToolCall(t *testing.T) {
	mcpSrv := fakeMCPServer(t, "delete-user", `"deleted"`)
	defer mcpSrv.Close()

	// On resume, gateway is only called once (after the pre-approved tool executes).
	gw := fakeGateway(t, []map[string]any{
		{
			"content":        `{"status":"done"}`,
			"model_resolved": "anthropic/claude-sonnet-4-6",
			"tokens_in":      80,
			"tokens_out":     15,
			"cost_usd":       0.0008,
		},
	})
	defer gw.Close()

	// Build the checkpoint messages as the orchestrator would after approval.
	// They include the system prompt, user input, and the assistant tool_use block.
	checkpointMessages := []agent.Message{
		{Role: "system", Content: "You are a test agent."},
		{Role: "user", Content: `{"message":"hello"}`},
		{Role: "assistant", Content: `[{"type":"tool_use","id":"tc1","name":"delete-user","input":{"id":"42"}}]`},
	}

	req := baseReq()
	req.IsResume = true
	req.Messages = checkpointMessages
	req.ApprovedToolCalls = []string{"tc1"}
	req.ToolServers = []agent.ToolServerSpec{
		{
			Name:      "user-svc",
			URL:       mcpSrv.URL,
			Allowlist: []string{"delete-user"},
			// No approval rules — on resume the tool was already approved.
		},
	}

	loop := agent.NewLoop(gw.URL, mcpClient())
	payload := loop.Run(context.Background(), req)

	if payload.Status != "ok" {
		t.Fatalf("expected ok after resume, got %s: %s", payload.Status, payload.Error)
	}
	if payload.Output["status"] != "done" {
		t.Errorf("unexpected output: %v", payload.Output)
	}
	if payload.Metrics.ToolCalls != 1 {
		t.Errorf("expected 1 tool call (pre-approved), got %d", payload.Metrics.ToolCalls)
	}
	if !payload.IsResume {
		t.Error("expected IsResume to be true in payload")
	}
}
func TestLoop_parallelToolCalls(t *testing.T) {
	// Turn 1: two tool calls for two different servers.
	// Turn 2: final answer.
	// Assert both tool_result blocks appear in turn 2's messages in original order.
	mcp1 := fakeMCPServer(t, "tool-a", `"result-a"`)
	mcp2 := fakeMCPServer(t, "tool-b", `"result-b"`)
	defer mcp1.Close()
	defer mcp2.Close()

	var capturedTurn2Messages []any
	callIdx := 0
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if callIdx == 0 {
			callIdx++
			json.NewEncoder(w).Encode(map[string]any{
				"content":        "",
				"model_resolved": "anthropic/claude-sonnet-4-6",
				"tokens_in":      100,
				"tokens_out":     20,
				"cost_usd":       0.001,
				"tool_calls": []map[string]any{
					{"id": "tc-a", "name": "tool-a", "arguments": map[string]any{"k": "v"}},
					{"id": "tc-b", "name": "tool-b", "arguments": map[string]any{"k": "v"}},
				},
			})
			return
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		capturedTurn2Messages, _ = body["messages"].([]any)
		json.NewEncoder(w).Encode(map[string]any{
			"content":        `{"done":true}`,
			"model_resolved": "anthropic/claude-sonnet-4-6",
			"tokens_in":      50,
			"tokens_out":     10,
			"cost_usd":       0.0005,
		})
	}))
	defer gw.Close()

	req := baseReq()
	req.ToolServers = []agent.ToolServerSpec{
		{Name: "srv1", URL: mcp1.URL, Allowlist: []string{"tool-a"}},
		{Name: "srv2", URL: mcp2.URL, Allowlist: []string{"tool-b"}},
	}

	loop := agent.NewLoop(gw.URL, mcpClient())
	payload := loop.Run(context.Background(), req)

	if payload.Status != agent.StatusOK {
		t.Fatalf("expected ok, got %s: %s", payload.Status, payload.Error)
	}
	if payload.Metrics.ToolCalls != 2 {
		t.Errorf("expected 2 tool calls, got %d", payload.Metrics.ToolCalls)
	}

	// messages: [system, user, assistant(tool_use-a), user(tool_result-a), assistant(tool_use-b), user(tool_result-b)]
	if len(capturedTurn2Messages) < 6 {
		t.Fatalf("expected at least 6 messages on turn 2, got %d", len(capturedTurn2Messages))
	}
	// Verify tool results appear in the original call order (tc-a before tc-b).
	resultA, _ := capturedTurn2Messages[3].(map[string]any)
	resultAContent, _ := resultA["content"].(string)
	if !strings.Contains(resultAContent, "tc-a") {
		t.Errorf("expected tool_result for tc-a at index 3, got: %v", resultAContent)
	}
	resultB, _ := capturedTurn2Messages[5].(map[string]any)
	resultBContent, _ := resultB["content"].(string)
	if !strings.Contains(resultBContent, "tc-b") {
		t.Errorf("expected tool_result for tc-b at index 5, got: %v", resultBContent)
	}
}

func TestLoop_parallelToolDiscovery(t *testing.T) {
	// Two MCP servers each advertising one tool.
	// Both tools must appear in the gateway request's tools list.
	mcp1 := fakeMCPServer(t, "tool-alpha", `"result-alpha"`)
	mcp2 := fakeMCPServer(t, "tool-beta", `"result-beta"`)
	defer mcp1.Close()
	defer mcp2.Close()

	var capturedTools []any
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		if capturedTools == nil {
			capturedTools, _ = body["tools"].([]any)
		}
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
	req.ToolServers = []agent.ToolServerSpec{
		{Name: "srv1", URL: mcp1.URL, Allowlist: []string{"tool-alpha"}},
		{Name: "srv2", URL: mcp2.URL, Allowlist: []string{"tool-beta"}},
	}

	loop := agent.NewLoop(gw.URL, mcpClient())
	payload := loop.Run(context.Background(), req)

	if payload.Status != agent.StatusOK {
		t.Fatalf("expected ok, got %s: %s", payload.Status, payload.Error)
	}
	toolNames := make(map[string]bool)
	for _, tool := range capturedTools {
		if m, ok := tool.(map[string]any); ok {
			toolNames[m["name"].(string)] = true
		}
	}
	if !toolNames["tool-alpha"] {
		t.Error("expected tool-alpha in gateway request")
	}
	if !toolNames["tool-beta"] {
		t.Error("expected tool-beta in gateway request")
	}
}

func TestLoop_modelResolvedSetOnFreeCorrection(t *testing.T) {
	// max_turns=1: forced conclusion produces invalid JSON.
	// Free correction (2nd call): valid JSON with model_resolved set.
	// Assert ModelResolved is populated from the recovery call.
	callCount := 0
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		if callCount == 1 {
			json.NewEncoder(w).Encode(map[string]any{
				"content":        "not valid json at all",
				"model_resolved": "anthropic/claude-sonnet-4-6",
				"tokens_in":      50,
				"tokens_out":     10,
				"cost_usd":       0.001,
			})
		} else {
			json.NewEncoder(w).Encode(map[string]any{
				"content":        `{"result":"ok"}`,
				"model_resolved": "anthropic/claude-haiku-4-5",
				"tokens_in":      20,
				"tokens_out":     5,
				"cost_usd":       0.0002,
			})
		}
	}))
	defer gw.Close()

	req := baseReq()
	req.MaxTurns = 1

	loop := agent.NewLoop(gw.URL, mcpClient())
	payload := loop.Run(context.Background(), req)

	if payload.Status != agent.StatusOK {
		t.Fatalf("expected ok, got %s: %s", payload.Status, payload.Error)
	}
	if payload.Metrics.ModelResolved != "anthropic/claude-sonnet-4-6" {
		t.Errorf("expected model_resolved from first call, got: %q", payload.Metrics.ModelResolved)
	}
	if payload.Metrics.LLMCalls != 2 {
		t.Errorf("expected 2 LLM calls, got %d", payload.Metrics.LLMCalls)
	}
}

func TestLoop_reflect_basicTurn(t *testing.T) {
	// First gateway call: initial reasoning → draft output.
	// Second gateway call: reflect turn → revised output.
	responses := []map[string]any{
		{
			"content":        `{"category":"billing","ktsu_confidence":0.5}`,
			"model_resolved": "anthropic/claude-sonnet-4-6",
			"tokens_in":      100,
			"tokens_out":     20,
			"cost_usd":       0.001,
		},
		{
			"content":        `{"category":"technical","ktsu_confidence":0.9}`,
			"model_resolved": "anthropic/claude-sonnet-4-6",
			"tokens_in":      80,
			"tokens_out":     15,
			"cost_usd":       0.0008,
		},
	}
	var capturedBodies []map[string]any
	idx := 0
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		capturedBodies = append(capturedBodies, body)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(responses[idx])
		idx++
	}))
	defer gw.Close()

	req := baseReq()
	req.Reflect = "Review your answer. If it seems wrong, revise it."
	req.OutputSchema = map[string]any{
		"properties": map[string]any{
			"category":        map[string]any{"type": "string"},
			"ktsu_confidence": map[string]any{"type": "number"},
		},
	}

	loop := agent.NewLoop(gw.URL, mcpClient())
	payload := loop.Run(context.Background(), req)

	if payload.Status != "ok" {
		t.Fatalf("expected ok, got %s: %s", payload.Status, payload.Error)
	}
	// Reflected output replaces draft
	if payload.Output["category"] != "technical" {
		t.Errorf("expected reflected category technical, got %v", payload.Output["category"])
	}
	if payload.Metrics.ReflectCalls != 1 {
		t.Errorf("expected 1 reflect call, got %d", payload.Metrics.ReflectCalls)
	}
	if payload.Metrics.LLMCalls != 2 {
		t.Errorf("expected 2 total LLM calls, got %d", payload.Metrics.LLMCalls)
	}
	if len(capturedBodies) != 2 {
		t.Fatalf("expected 2 gateway calls, got %d", len(capturedBodies))
	}
	// Reflect turn uses reflect prompt as system message
	reflectMsgs, _ := capturedBodies[1]["messages"].([]any)
	firstMsg, _ := reflectMsgs[0].(map[string]any)
	if firstMsg["role"] != "system" || firstMsg["content"] != req.Reflect {
		t.Errorf("reflect turn system message wrong: %v", firstMsg)
	}
}

func TestLoop_reflect_skippedOnHighConfidence(t *testing.T) {
	// Only one gateway call expected — reflect should be skipped.
	gw := fakeGateway(t, []map[string]any{
		{
			"content":        `{"category":"billing","ktsu_confidence":0.95}`,
			"model_resolved": "anthropic/claude-sonnet-4-6",
			"tokens_in":      100,
			"tokens_out":     20,
			"cost_usd":       0.001,
		},
	})
	defer gw.Close()

	req := baseReq()
	req.Reflect = "Review your answer."
	req.ConfidenceThreshold = 0.8
	req.OutputSchema = map[string]any{
		"properties": map[string]any{
			"category":        map[string]any{"type": "string"},
			"ktsu_confidence": map[string]any{"type": "number"},
		},
	}

	loop := agent.NewLoop(gw.URL, mcpClient())
	payload := loop.Run(context.Background(), req)

	if payload.Status != "ok" {
		t.Fatalf("expected ok, got %s: %s", payload.Status, payload.Error)
	}
	if payload.Metrics.ReflectCalls != 0 {
		t.Errorf("expected 0 reflect calls, got %d", payload.Metrics.ReflectCalls)
	}
}

func TestLoop_reflect_jsonCorrectionSucceeds(t *testing.T) {
	// Call 1: draft. Call 2: reflect → bad JSON. Call 3: correction → valid JSON.
	responses := []map[string]any{
		{"content": `{"category":"billing"}`, "model_resolved": "m", "tokens_in": 10, "tokens_out": 5, "cost_usd": 0.001},
		{"content": "I cannot produce JSON right now.", "model_resolved": "m", "tokens_in": 10, "tokens_out": 5, "cost_usd": 0.001},
		{"content": `{"category":"technical"}`, "model_resolved": "m", "tokens_in": 10, "tokens_out": 5, "cost_usd": 0.001},
	}
	gw := fakeGateway(t, responses)
	defer gw.Close()

	req := baseReq()
	req.Reflect = "Review your answer."

	loop := agent.NewLoop(gw.URL, mcpClient())
	payload := loop.Run(context.Background(), req)

	if payload.Status != "ok" {
		t.Fatalf("expected ok after reflect correction, got %s: %s", payload.Status, payload.Error)
	}
	if payload.Output["category"] != "technical" {
		t.Errorf("expected technical, got %v", payload.Output["category"])
	}
	if payload.Metrics.ReflectCalls != 2 {
		t.Errorf("expected 2 reflect calls (reflect + correction), got %d", payload.Metrics.ReflectCalls)
	}
}

func TestLoop_reflect_jsonCorrectionFails(t *testing.T) {
	// Call 1: draft. Call 2: reflect → bad JSON. Call 3: correction → still bad JSON.
	responses := []map[string]any{
		{"content": `{"category":"billing"}`, "model_resolved": "m", "tokens_in": 10, "tokens_out": 5, "cost_usd": 0.001},
		{"content": "Not JSON.", "model_resolved": "m", "tokens_in": 10, "tokens_out": 5, "cost_usd": 0.001},
		{"content": "Still not JSON.", "model_resolved": "m", "tokens_in": 10, "tokens_out": 5, "cost_usd": 0.001},
	}
	gw := fakeGateway(t, responses)
	defer gw.Close()

	req := baseReq()
	req.Reflect = "Review your answer."

	loop := agent.NewLoop(gw.URL, mcpClient())
	payload := loop.Run(context.Background(), req)

	if payload.Status != "failed" {
		t.Fatalf("expected failed, got %s", payload.Status)
	}
	if payload.Error != "reflect_output_invalid" {
		t.Errorf("expected reflect_output_invalid error, got %s", payload.Error)
	}
	if payload.Metrics.ReflectCalls != 2 {
		t.Errorf("expected 2 reflect calls, got %d", payload.Metrics.ReflectCalls)
	}
}

func TestLoop_reflect_fatalReservedFieldAborts(t *testing.T) {
	// Draft contains ktsu_injection_attempt: true — reflect must not run.
	gw := fakeGateway(t, []map[string]any{
		{
			"content":        `{"category":"billing","ktsu_injection_attempt":true}`,
			"model_resolved": "m",
			"tokens_in":      50,
			"tokens_out":     10,
			"cost_usd":       0.001,
		},
	})
	defer gw.Close()

	req := baseReq()
	req.Reflect = "Review your answer."

	loop := agent.NewLoop(gw.URL, mcpClient())
	payload := loop.Run(context.Background(), req)

	if payload.Status != "failed" {
		t.Fatalf("expected failed, got %s", payload.Status)
	}
	if payload.Error != "injection attempt detected" {
		t.Errorf("expected injection attempt error, got %s", payload.Error)
	}
	if payload.Metrics.ReflectCalls != 0 {
		t.Errorf("expected 0 reflect calls, got %d", payload.Metrics.ReflectCalls)
	}
}

func TestLoop_reflect_gatewayErrorOnReflect(t *testing.T) {
	// Call 1: draft succeeds. Call 2: reflect gateway fails (server closes).
	idx := 0
	gw := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if idx == 0 {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]any{
				"content":        `{"category":"billing"}`,
				"model_resolved": "m",
				"tokens_in":      10,
				"tokens_out":     5,
				"cost_usd":       0.001,
			})
		} else {
			// Simulate gateway failure on reflect call.
			hj, ok := w.(http.Hijacker)
			if ok {
				conn, _, _ := hj.Hijack()
				conn.Close()
			}
		}
		idx++
	}))
	defer gw.Close()

	req := baseReq()
	req.Reflect = "Review your answer."

	loop := agent.NewLoop(gw.URL, mcpClient())
	payload := loop.Run(context.Background(), req)

	if payload.Status != "failed" {
		t.Fatalf("expected failed on reflect gateway error, got %s", payload.Status)
	}
	// Should NOT be the reflect_output_invalid sentinel — should be a real error.
	if payload.Error == "reflect_output_invalid" {
		t.Errorf("expected real error, got the sentinel reflect_output_invalid")
	}
	if payload.Error == "" {
		t.Errorf("expected non-empty error")
	}
}
