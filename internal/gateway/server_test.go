package gateway_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kimitsu-ai/ktsu/internal/config"
	gw "github.com/kimitsu-ai/ktsu/internal/gateway"
	"github.com/kimitsu-ai/ktsu/internal/gateway/providers"
)

// fakeDispatcher lets tests control dispatch responses.
type fakeDispatcher struct {
	resp providers.InvokeResponse
	err  error
}

func (f *fakeDispatcher) Dispatch(_ context.Context, _ gw.DispatchRequest) (providers.InvokeResponse, error) {
	return f.resp, f.err
}

func gatewayWithDispatcher(d gw.Dispatchable) *gw.Gateway {
	cfg := gw.Config{
		GatewayConfig: &config.GatewayConfig{},
		Port:          0,
	}
	return gw.NewWithDispatcher(cfg, d)
}

func invokeBody(group string) []byte {
	b, _ := json.Marshal(map[string]interface{}{
		"run_id":     "r1",
		"step_id":    "s1",
		"group":      group,
		"messages":   []map[string]string{{"role": "user", "content": "hi"}},
		"max_tokens": 100,
	})
	return b
}

func TestServer_invoke_success(t *testing.T) {
	d := &fakeDispatcher{resp: providers.InvokeResponse{
		Content:       "hello",
		ModelResolved: "openai/gpt-4o-mini",
		TokensIn:      10,
		TokensOut:     5,
		CostUSD:       0.001,
	}}
	g := gatewayWithDispatcher(d)
	srv := httptest.NewServer(g.Handler())
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/invoke", "application/json", bytes.NewReader(invokeBody("fast")))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	var body providers.InvokeResponse
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if body.Content != "hello" {
		t.Errorf("content: want 'hello', got %q", body.Content)
	}
	if body.CostUSD != 0.001 {
		t.Errorf("cost: want 0.001, got %f", body.CostUSD)
	}
}

func TestServer_invoke_unknown_group_returns_400(t *testing.T) {
	d := &fakeDispatcher{err: &providers.GatewayError{Type: "unknown_group", Message: "not found", Retryable: false}}
	g := gatewayWithDispatcher(d)
	srv := httptest.NewServer(g.Handler())
	defer srv.Close()

	resp, _ := http.Post(srv.URL+"/invoke", "application/json", bytes.NewReader(invokeBody("bad")))
	if resp.StatusCode != 400 {
		t.Errorf("want 400, got %d", resp.StatusCode)
	}
	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if body["error"] != "unknown_group" {
		t.Errorf("error field: want unknown_group, got %v", body["error"])
	}
	if body["retryable"] != false {
		t.Errorf("retryable should be false")
	}
}

func TestServer_invoke_budget_exceeded_returns_402(t *testing.T) {
	d := &fakeDispatcher{err: &providers.GatewayError{Type: "budget_exceeded", Message: "limit hit", Retryable: false}}
	g := gatewayWithDispatcher(d)
	srv := httptest.NewServer(g.Handler())
	defer srv.Close()

	resp, _ := http.Post(srv.URL+"/invoke", "application/json", bytes.NewReader(invokeBody("fast")))
	if resp.StatusCode != 402 {
		t.Errorf("want 402, got %d", resp.StatusCode)
	}
}

func TestServer_invoke_no_models_available_returns_503(t *testing.T) {
	d := &fakeDispatcher{err: &providers.GatewayError{Type: "no_models_available", Message: "empty", Retryable: false}}
	g := gatewayWithDispatcher(d)
	srv := httptest.NewServer(g.Handler())
	defer srv.Close()

	resp, _ := http.Post(srv.URL+"/invoke", "application/json", bytes.NewReader(invokeBody("fast")))
	if resp.StatusCode != 503 {
		t.Errorf("want 503, got %d", resp.StatusCode)
	}
}

func TestServer_invoke_provider_error_retryable_returns_502(t *testing.T) {
	d := &fakeDispatcher{err: &providers.GatewayError{Type: "provider_error", Message: "upstream failed", Retryable: true}}
	g := gatewayWithDispatcher(d)
	srv := httptest.NewServer(g.Handler())
	defer srv.Close()

	resp, _ := http.Post(srv.URL+"/invoke", "application/json", bytes.NewReader(invokeBody("fast")))
	if resp.StatusCode != 502 {
		t.Errorf("want 502, got %d", resp.StatusCode)
	}
	var body map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response body: %v", err)
	}
	if body["retryable"] != true {
		t.Errorf("retryable should be true")
	}
}

func TestServer_health(t *testing.T) {
	g := gatewayWithDispatcher(&fakeDispatcher{})
	srv := httptest.NewServer(g.Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("want 200, got %d", resp.StatusCode)
	}
}

func TestServer_invoke_bad_json_returns_400(t *testing.T) {
	g := gatewayWithDispatcher(&fakeDispatcher{})
	srv := httptest.NewServer(g.Handler())
	defer srv.Close()

	resp, _ := http.Post(srv.URL+"/invoke", "application/json", bytes.NewReader([]byte("not json")))
	if resp.StatusCode != 400 {
		t.Errorf("want 400, got %d", resp.StatusCode)
	}
}

