package anthropic_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kimitsu-ai/ktsu/internal/gateway/providers"
	"github.com/kimitsu-ai/ktsu/internal/gateway/providers/anthropic"
)

func defaultRequest() providers.InvokeRequest {
	temp := 0.7
	return providers.InvokeRequest{
		Model: "claude-opus-4-6",
		Messages: []providers.Message{
			{Role: "system", Content: "You are helpful."},
			{Role: "user", Content: "Hello"},
		},
		MaxTokens:   100,
		Temperature: &temp,
	}
}

func TestAnthropicProvider_success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"content": []map[string]string{{"type": "text", "text": "Hi there"}},
			"usage":   map[string]int{"input_tokens": 10, "output_tokens": 5},
		})
	}))
	defer srv.Close()

	resp, err := anthropic.New(srv.URL, "test-key").Invoke(context.Background(), defaultRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "Hi there" {
		t.Errorf("content: want 'Hi there', got %q", resp.Content)
	}
	if resp.TokensIn != 10 || resp.TokensOut != 5 {
		t.Errorf("tokens: want in=10 out=5, got in=%d out=%d", resp.TokensIn, resp.TokensOut)
	}
}

func TestAnthropicProvider_extracts_system_message(t *testing.T) {
	var body map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"content": []map[string]string{{"type": "text", "text": "ok"}},
			"usage":   map[string]int{"input_tokens": 1, "output_tokens": 1},
		})
	}))
	defer srv.Close()

	anthropic.New(srv.URL, "key").Invoke(context.Background(), defaultRequest())

	// system should be a top-level field, not in messages
	if body["system"] != "You are helpful." {
		t.Errorf("system field: want 'You are helpful.', got %v", body["system"])
	}
	msgs := body["messages"].([]interface{})
	for _, m := range msgs {
		msg := m.(map[string]interface{})
		if msg["role"] == "system" {
			t.Error("system message should not appear in messages array")
		}
	}
}

func TestAnthropicProvider_sends_required_headers(t *testing.T) {
	var version, apiKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		version = r.Header.Get("anthropic-version")
		apiKey = r.Header.Get("x-api-key")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"content": []map[string]string{{"type": "text", "text": "ok"}},
			"usage":   map[string]int{"input_tokens": 1, "output_tokens": 1},
		})
	}))
	defer srv.Close()

	anthropic.New(srv.URL, "sk-ant-test").Invoke(context.Background(), defaultRequest())
	if version != "2023-06-01" {
		t.Errorf("anthropic-version: want 2023-06-01, got %q", version)
	}
	if apiKey != "sk-ant-test" {
		t.Errorf("x-api-key: want sk-ant-test, got %q", apiKey)
	}
}

func TestAnthropicProvider_credit_exhaustion_is_budget_exceeded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"type": "error",
			"error": map[string]string{
				"type":    "permission_error",
				"message": "credit balance is too low to access the API",
			},
		})
	}))
	defer srv.Close()

	_, err := anthropic.New(srv.URL, "key").Invoke(context.Background(), defaultRequest())
	var gwErr *providers.GatewayError
	if !errors.As(err, &gwErr) {
		t.Fatalf("expected GatewayError, got %T", err)
	}
	if gwErr.Type != "budget_exceeded" || gwErr.Retryable {
		t.Errorf("want non-retryable budget_exceeded, got type=%q retryable=%v", gwErr.Type, gwErr.Retryable)
	}
}

func TestAnthropicProvider_tool_use_response(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"content": []map[string]interface{}{
				{
					"type":  "tool_use",
					"id":    "toolu_123",
					"name":  "kv-get",
					"input": map[string]interface{}{"key": "foo"},
				},
			},
			"usage": map[string]int{"input_tokens": 5, "output_tokens": 3},
		})
	}))
	defer srv.Close()

	resp, err := anthropic.New(srv.URL, "key").Invoke(context.Background(), defaultRequest())
	if err != nil {
		t.Fatalf("unexpected error for tool_use response: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("want 1 tool call, got %d", len(resp.ToolCalls))
	}
	tc := resp.ToolCalls[0]
	if tc.ID != "toolu_123" || tc.Name != "kv-get" {
		t.Errorf("tool call: want id=toolu_123 name=kv-get, got id=%q name=%q", tc.ID, tc.Name)
	}
	if tc.Arguments["key"] != "foo" {
		t.Errorf("arguments: want key=foo, got %v", tc.Arguments)
	}
	if resp.Content != "" {
		t.Errorf("content: want empty for tool-only response, got %q", resp.Content)
	}
}

func TestAnthropicProvider_empty_content_returns_error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"content": []map[string]interface{}{},
			"usage":   map[string]int{"input_tokens": 5, "output_tokens": 3},
		})
	}))
	defer srv.Close()

	_, err := anthropic.New(srv.URL, "key").Invoke(context.Background(), defaultRequest())
	if err == nil {
		t.Fatal("expected error for empty content array, got nil")
	}
}

func TestAnthropicProvider_529_overload_retryable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(529)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"type": "error",
			"error": map[string]string{
				"type":    "overloaded_error",
				"message": "overloaded",
			},
		})
	}))
	defer srv.Close()

	_, err := anthropic.New(srv.URL, "key").Invoke(context.Background(), defaultRequest())
	var gwErr *providers.GatewayError
	if !errors.As(err, &gwErr) {
		t.Fatalf("expected GatewayError")
	}
	if gwErr.Type != "provider_error" || !gwErr.Retryable {
		t.Errorf("want retryable provider_error for 529")
	}
}
