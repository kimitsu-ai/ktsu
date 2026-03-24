package openai_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kimitsu-ai/ktsu/internal/gateway/providers"
	"github.com/kimitsu-ai/ktsu/internal/gateway/providers/openai"
)

func defaultRequest() providers.InvokeRequest {
	temp := 0.2
	return providers.InvokeRequest{
		RunID:  "r1",
		StepID: "s1",
		Model:  "gpt-4o-mini",
		Messages: []providers.Message{
			{Role: "system", Content: "You are helpful."},
			{Role: "user", Content: "Hello"},
		},
		MaxTokens:   100,
		Temperature: &temp,
	}
}

func okResponse() map[string]interface{} {
	return map[string]interface{}{
		"choices": []map[string]interface{}{
			{"message": map[string]string{"content": "Hi there"}},
		},
		"usage": map[string]int{"prompt_tokens": 312, "completion_tokens": 87},
	}
}

func TestOpenAIProvider_success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(okResponse())
	}))
	defer srv.Close()

	resp, err := openai.New(srv.URL, "test-key").Invoke(context.Background(), defaultRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "Hi there" {
		t.Errorf("content: want 'Hi there', got %q", resp.Content)
	}
	if resp.TokensIn != 312 || resp.TokensOut != 87 {
		t.Errorf("tokens: want in=312 out=87, got in=%d out=%d", resp.TokensIn, resp.TokensOut)
	}
}

func TestOpenAIProvider_sends_model_and_params(t *testing.T) {
	var body map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(okResponse())
	}))
	defer srv.Close()

	openai.New(srv.URL, "key").Invoke(context.Background(), defaultRequest())
	if body["model"] != "gpt-4o-mini" {
		t.Errorf("model: want gpt-4o-mini, got %v", body["model"])
	}
	if body["max_tokens"].(float64) != 100 {
		t.Errorf("max_tokens: want 100, got %v", body["max_tokens"])
	}
	if body["temperature"].(float64) != 0.2 {
		t.Errorf("temperature: want 0.2, got %v", body["temperature"])
	}
}

func TestOpenAIProvider_sends_auth_header(t *testing.T) {
	var authHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(okResponse())
	}))
	defer srv.Close()

	openai.New(srv.URL, "sk-test").Invoke(context.Background(), defaultRequest())
	if authHeader != "Bearer sk-test" {
		t.Errorf("auth: want 'Bearer sk-test', got %q", authHeader)
	}
}

func TestOpenAIProvider_rate_limit_retryable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]string{"code": "rate_limit_exceeded", "message": "slow down"},
		})
	}))
	defer srv.Close()

	_, err := openai.New(srv.URL, "key").Invoke(context.Background(), defaultRequest())
	var gwErr *providers.GatewayError
	if !errors.As(err, &gwErr) {
		t.Fatalf("expected GatewayError, got %T: %v", err, err)
	}
	if gwErr.Type != "provider_error" || !gwErr.Retryable {
		t.Errorf("want retryable provider_error, got type=%q retryable=%v", gwErr.Type, gwErr.Retryable)
	}
}

func TestOpenAIProvider_billing_hard_limit_budget_exceeded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]string{"code": "billing_hard_limit_reached", "message": "limit hit"},
		})
	}))
	defer srv.Close()

	_, err := openai.New(srv.URL, "key").Invoke(context.Background(), defaultRequest())
	var gwErr *providers.GatewayError
	if !errors.As(err, &gwErr) {
		t.Fatalf("expected GatewayError")
	}
	if gwErr.Type != "budget_exceeded" || gwErr.Retryable {
		t.Errorf("want non-retryable budget_exceeded, got type=%q retryable=%v", gwErr.Type, gwErr.Retryable)
	}
}

func TestOpenAIProvider_insufficient_quota_budget_exceeded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]string{"code": "insufficient_quota", "message": "quota"},
		})
	}))
	defer srv.Close()

	_, err := openai.New(srv.URL, "key").Invoke(context.Background(), defaultRequest())
	var gwErr *providers.GatewayError
	if !errors.As(err, &gwErr) {
		t.Fatalf("expected GatewayError")
	}
	if gwErr.Type != "budget_exceeded" {
		t.Errorf("want budget_exceeded, got %q", gwErr.Type)
	}
}

func TestOpenAIProvider_5xx_retryable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
	}))
	defer srv.Close()

	_, err := openai.New(srv.URL, "key").Invoke(context.Background(), defaultRequest())
	var gwErr *providers.GatewayError
	if !errors.As(err, &gwErr) {
		t.Fatalf("expected GatewayError")
	}
	if gwErr.Type != "provider_error" || !gwErr.Retryable {
		t.Errorf("want retryable provider_error")
	}
}

func TestOpenAIProvider_tool_call_response(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{
					"message": map[string]interface{}{
						"content": nil,
						"tool_calls": []map[string]interface{}{
							{
								"id":   "call_abc",
								"type": "function",
								"function": map[string]string{
									"name":      "kv-get",
									"arguments": `{"key":"foo"}`,
								},
							},
						},
					},
				},
			},
			"usage": map[string]int{"prompt_tokens": 10, "completion_tokens": 5},
		})
	}))
	defer srv.Close()

	resp, err := openai.New(srv.URL, "key").Invoke(context.Background(), defaultRequest())
	if err != nil {
		t.Fatalf("unexpected error for tool call response: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("want 1 tool call, got %d", len(resp.ToolCalls))
	}
	tc := resp.ToolCalls[0]
	if tc.ID != "call_abc" || tc.Name != "kv-get" {
		t.Errorf("tool call: want id=call_abc name=kv-get, got id=%q name=%q", tc.ID, tc.Name)
	}
	if tc.Arguments["key"] != "foo" {
		t.Errorf("arguments: want key=foo, got %v", tc.Arguments)
	}
}

func TestOpenAIProvider_4xx_not_retryable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]string{"code": "invalid_request", "message": "bad"},
		})
	}))
	defer srv.Close()

	_, err := openai.New(srv.URL, "key").Invoke(context.Background(), defaultRequest())
	var gwErr *providers.GatewayError
	if !errors.As(err, &gwErr) {
		t.Fatalf("expected GatewayError")
	}
	if gwErr.Type != "provider_error" || gwErr.Retryable {
		t.Errorf("want non-retryable provider_error")
	}
}
