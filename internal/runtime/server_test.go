package runtime

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/kimitsu-ai/ktsu/internal/runtime/agent"
)

func fakeGatewayHandler(output string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"content":        output,
			"model_resolved": "test/model",
			"tokens_in":      10,
			"tokens_out":     5,
			"cost_usd":       0.0,
		})
	}
}

func TestHandleInvoke_returns202(t *testing.T) {
	gw := httptest.NewServer(fakeGatewayHandler(`{"ok":true}`))
	defer gw.Close()

	r := New(Config{LLMGatewayURL: gw.URL})
	srv := httptest.NewServer(r.srv.mux)
	defer srv.Close()

	callbackReceived := make(chan agent.CallbackPayload, 1)
	callbackSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var p agent.CallbackPayload
		json.NewDecoder(req.Body).Decode(&p)
		callbackReceived <- p
		w.WriteHeader(http.StatusOK)
	}))
	defer callbackSrv.Close()

	body, _ := json.Marshal(map[string]any{
		"run_id": "run_test", "step_id": "step_test",
		"agent_name": "test-agent", "system": "You are a test agent.",
		"max_turns":    5,
		"model":        map[string]any{"group": "standard", "max_tokens": 512},
		"input":        map[string]any{"message": "hello"},
		"callback_url": callbackSrv.URL,
	})
	resp, err := http.Post(srv.URL+"/invoke", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /invoke: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", resp.StatusCode)
	}

	select {
	case payload := <-callbackReceived:
		if payload.Status != "ok" {
			t.Errorf("expected ok, got %s (error: %s)", payload.Status, payload.Error)
		}
		if payload.RunID != "run_test" {
			t.Errorf("expected run_id run_test, got %s", payload.RunID)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for callback")
	}
}

func TestHandleInvoke_missingFields(t *testing.T) {
	r := New(Config{LLMGatewayURL: "http://unused"})
	srv := httptest.NewServer(r.srv.mux)
	defer srv.Close()

	body, _ := json.Marshal(map[string]any{
		"run_id":  "run_test",
		"step_id": "step_test",
		// missing callback_url
	})
	resp, err := http.Post(srv.URL+"/invoke", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST /invoke: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", resp.StatusCode)
	}
}

func TestHandleInvoke_activeMapCleanedUp(t *testing.T) {
	gw := httptest.NewServer(fakeGatewayHandler(`{"ok":true}`))
	defer gw.Close()

	r := New(Config{LLMGatewayURL: gw.URL})
	srv := httptest.NewServer(r.srv.mux)
	defer srv.Close()

	done := make(chan struct{})
	callbackSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(http.StatusOK)
		close(done)
	}))
	defer callbackSrv.Close()

	body, _ := json.Marshal(map[string]any{
		"run_id": "run_test", "step_id": "step_test",
		"system": "s", "max_turns": 1,
		"model":        map[string]any{"group": "g", "max_tokens": 100},
		"input":        map[string]any{},
		"callback_url": callbackSrv.URL,
	})
	resp, err := http.Post(srv.URL+"/invoke", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	resp.Body.Close()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for callback")
	}

	// After callback, active map should be empty.
	var count int
	r.srv.activeInvocations.Range(func(_, _ any) bool { count++; return true })
	if count != 0 {
		t.Errorf("expected empty active map, got %d entries", count)
	}
}
