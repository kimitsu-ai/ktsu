package orchestrator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/kimitsu-ai/ktsu/internal/config"
	"github.com/kimitsu-ai/ktsu/internal/orchestrator/state"
	"github.com/kimitsu-ai/ktsu/internal/runtime/agent"
	"github.com/kimitsu-ai/ktsu/pkg/types"
)

// newHandlerServer creates a minimal server suitable for testing handleStepComplete.
func newHandlerServer() *server {
	return &server{
		pendingCallbacks: make(map[stepCallbackKey]chan agent.CallbackPayload),
	}
}

// newHandlerServerWithStore creates a server with a MemStore for testing handleGetRun.
func newHandlerServerWithStore() *server {
	return &server{
		store:            state.NewMemStore(),
		pendingCallbacks: make(map[stepCallbackKey]chan agent.CallbackPayload),
	}
}

func postComplete(t *testing.T, s *server, runID, stepID string, payload any) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/runs/"+runID+"/steps/"+stepID+"/complete", bytes.NewReader(body))
	req.SetPathValue("run_id", runID)
	req.SetPathValue("step_id", stepID)
	rr := httptest.NewRecorder()
	s.handleStepComplete(rr, req)
	return rr
}

// TestHandleStepComplete_returns200 verifies a valid callback with a registered channel returns 200.
func TestHandleStepComplete_returns200(t *testing.T) {
	s := newHandlerServer()
	key := stepCallbackKey{"run-1", "step-1"}
	ch := make(chan agent.CallbackPayload, 1)
	s.pendingCallbacks[key] = ch

	rr := postComplete(t, s, "run-1", "step-1", agent.CallbackPayload{
		RunID:  "run-1",
		StepID: "step-1",
		Status: "ok",
		Output: map[string]any{"result": "hello"},
	})

	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rr.Code)
	}
}

// TestHandleStepComplete_invalidJSON verifies malformed body returns 400.
func TestHandleStepComplete_invalidJSON(t *testing.T) {
	s := newHandlerServer()
	req := httptest.NewRequest(http.MethodPost, "/runs/r/steps/s/complete", bytes.NewBufferString("{bad json"))
	req.SetPathValue("run_id", "r")
	req.SetPathValue("step_id", "s")
	rr := httptest.NewRecorder()
	s.handleStepComplete(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("want 400, got %d", rr.Code)
	}
}

// TestHandleStepComplete_callbackPayloadDecoding verifies all payload fields (including metrics)
// are decoded correctly and delivered on the channel.
func TestHandleStepComplete_callbackPayloadDecoding(t *testing.T) {
	s := newHandlerServer()
	key := stepCallbackKey{"run-2", "step-2"}
	ch := make(chan agent.CallbackPayload, 1)
	s.pendingCallbacks[key] = ch

	sent := agent.CallbackPayload{
		RunID:  "run-2",
		StepID: "step-2",
		Status: "ok",
		Output: map[string]any{"answer": "42"},
		Metrics: agent.Metrics{
			ModelResolved: "anthropic/claude-opus-4-6",
			TokensIn:      100,
			TokensOut:     200,
			CostUSD:       0.005,
			DurationMS:    1234,
			ToolCalls:     3,
		},
	}

	postComplete(t, s, "run-2", "step-2", sent)

	received := <-ch
	if received.Metrics.ModelResolved != "anthropic/claude-opus-4-6" {
		t.Errorf("ModelResolved: got %q", received.Metrics.ModelResolved)
	}
	if received.Metrics.TokensIn != 100 {
		t.Errorf("TokensIn: got %d", received.Metrics.TokensIn)
	}
	if received.Metrics.CostUSD != 0.005 {
		t.Errorf("CostUSD: got %f", received.Metrics.CostUSD)
	}
	if received.Metrics.ToolCalls != 3 {
		t.Errorf("ToolCalls: got %d", received.Metrics.ToolCalls)
	}
}

// TestHandleStepComplete_reservedFieldsStripped verifies ktsu_* fields are stripped from output
// before delivery on the channel.
func TestHandleStepComplete_reservedFieldsStripped(t *testing.T) {
	s := newHandlerServer()
	key := stepCallbackKey{"run-3", "step-3"}
	ch := make(chan agent.CallbackPayload, 1)
	s.pendingCallbacks[key] = ch

	postComplete(t, s, "run-3", "step-3", agent.CallbackPayload{
		RunID:  "run-3",
		StepID: "step-3",
		Status: "ok",
		Output: map[string]any{
			"result":          "hello",
			"ktsu_confidence": 0.9,
		},
	})

	received := <-ch
	if _, ok := received.Output["ktsu_confidence"]; ok {
		t.Error("ktsu_confidence should have been stripped from output")
	}
	if received.Output["result"] != "hello" {
		t.Errorf("result field should be preserved, got %v", received.Output["result"])
	}
}

// TestHandleStepComplete_injectionAttemptFails verifies ktsu_injection_attempt: true causes
// status: failed and clears output.
func TestHandleStepComplete_injectionAttemptFails(t *testing.T) {
	s := newHandlerServer()
	key := stepCallbackKey{"run-4", "step-4"}
	ch := make(chan agent.CallbackPayload, 1)
	s.pendingCallbacks[key] = ch

	postComplete(t, s, "run-4", "step-4", agent.CallbackPayload{
		RunID:  "run-4",
		StepID: "step-4",
		Status: "ok",
		Output: map[string]any{
			"result":                "payload",
			"ktsu_injection_attempt": true,
		},
	})

	received := <-ch
	if received.Status != "failed" {
		t.Errorf("want status=failed, got %q", received.Status)
	}
	if received.Output != nil {
		t.Errorf("want nil output after injection attempt, got %v", received.Output)
	}
	if received.Error == "" {
		t.Error("want non-empty error message")
	}
}

// TestHandleStepComplete_failedStatusPassedThrough verifies a pre-failed callback is delivered
// without modification.
func TestHandleStepComplete_failedStatusPassedThrough(t *testing.T) {
	s := newHandlerServer()
	key := stepCallbackKey{"run-5", "step-5"}
	ch := make(chan agent.CallbackPayload, 1)
	s.pendingCallbacks[key] = ch

	postComplete(t, s, "run-5", "step-5", agent.CallbackPayload{
		RunID:  "run-5",
		StepID: "step-5",
		Status: "failed",
		Error:  "upstream model error",
	})

	received := <-ch
	if received.Status != "failed" {
		t.Errorf("want status=failed, got %q", received.Status)
	}
	if received.Error != "upstream model error" {
		t.Errorf("want original error, got %q", received.Error)
	}
}

// TestHandleStepComplete_noPendingWaiter verifies no panic occurs when no channel is registered,
// and that the handler still returns 200.
func TestHandleStepComplete_noPendingWaiter(t *testing.T) {
	s := newHandlerServer()
	// No channel registered for this key.
	rr := postComplete(t, s, "run-6", "step-6", agent.CallbackPayload{
		RunID:  "run-6",
		StepID: "step-6",
		Status: "ok",
		Output: map[string]any{"x": 1},
	})

	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rr.Code)
	}
}

// TestHandleStepComplete_skipReasonSetsSkippedStatus verifies ktsu_skip_reason causes
// status: skipped.
func TestHandleStepComplete_skipReasonSetsSkippedStatus(t *testing.T) {
	s := newHandlerServer()
	key := stepCallbackKey{"run-7", "step-7"}
	ch := make(chan agent.CallbackPayload, 1)
	s.pendingCallbacks[key] = ch

	postComplete(t, s, "run-7", "step-7", agent.CallbackPayload{
		RunID:  "run-7",
		StepID: "step-7",
		Status: "ok",
		Output: map[string]any{
			"result":          "value",
			"ktsu_skip_reason": "already processed",
		},
	})

	received := <-ch
	if received.Status != "skipped" {
		t.Errorf("want status=skipped, got %q", received.Status)
	}
}

// TestRuntimeDispatcher_Dispatch_returns202AndDelivery verifies end-to-end: fake runtime returns
// 202, test delivers callback via channel, Dispatch returns the output.
func TestRuntimeDispatcher_Dispatch_returns202AndDelivery(t *testing.T) {
	var capturedBody agent.InvokeRequest
	// invokeReceived fires once the runtime handler has decoded the request body.
	// Because the dispatcher registers its channel before sending the HTTP request,
	// the channel is guaranteed to exist by the time this fires.
	invokeReceived := make(chan struct{})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
			t.Errorf("decode invoke request: %v", err)
		}
		w.WriteHeader(http.StatusAccepted)
		close(invokeReceived)
	}))
	defer srv.Close()

	pendingCallbacks := make(map[stepCallbackKey]chan agent.CallbackPayload)
	var mu sync.Mutex

	d := &runtimeDispatcher{
		runtimeURL:      srv.URL,
		ownURL:          "http://orchestrator",
		pendingMu:       &mu,
		pendingCallbacks: pendingCallbacks,
	}

	resultCh := make(chan map[string]any, 1)
	errCh := make(chan error, 1)
	go func() {
		out, _, err := d.Dispatch(context.Background(), "run-8", "step-8", nil, map[string]any{"input": "data"})
		resultCh <- out
		errCh <- err
	}()

	// Wait until the runtime received the invoke request (channel registered before HTTP send).
	<-invokeReceived

	mu.Lock()
	ch := pendingCallbacks[stepCallbackKey{"run-8", "step-8"}]
	mu.Unlock()

	ch <- agent.CallbackPayload{
		RunID:  "run-8",
		StepID: "step-8",
		Status: "ok",
		Output: map[string]any{"answer": "yes"},
	}

	output := <-resultCh
	if err := <-errCh; err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if output["answer"] != "yes" {
		t.Errorf("want answer=yes, got %v", output["answer"])
	}

	// Verify the invoke request sent to the runtime.
	if capturedBody.RunID != "run-8" {
		t.Errorf("RunID: got %q", capturedBody.RunID)
	}
	if capturedBody.CallbackURL != "http://orchestrator/runs/run-8/steps/step-8/complete" {
		t.Errorf("CallbackURL: got %q", capturedBody.CallbackURL)
	}
}

// TestRuntimeDispatcher_Dispatch_agentFailure verifies a failed callback propagates as an error.
func TestRuntimeDispatcher_Dispatch_agentFailure(t *testing.T) {
	invokeReceived := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		close(invokeReceived)
	}))
	defer srv.Close()

	pendingCallbacks := make(map[stepCallbackKey]chan agent.CallbackPayload)
	var mu sync.Mutex

	d := &runtimeDispatcher{
		runtimeURL:      srv.URL,
		ownURL:          "http://orchestrator",
		pendingMu:       &mu,
		pendingCallbacks: pendingCallbacks,
	}

	errCh := make(chan error, 1)
	go func() {
		_, _, err := d.Dispatch(context.Background(), "run-9", "step-9", nil, nil)
		errCh <- err
	}()

	<-invokeReceived
	mu.Lock()
	ch := pendingCallbacks[stepCallbackKey{"run-9", "step-9"}]
	mu.Unlock()

	ch <- agent.CallbackPayload{
		Status: "failed",
		Error:  "tool call timed out",
	}

	err := <-errCh
	if err == nil {
		t.Fatal("want error for failed agent, got nil")
	}
	if err.Error() != "agent step failed: tool call timed out" {
		t.Errorf("unexpected error message: %v", err)
	}
}

// TestHandleGetRun_returnsRunAndSteps verifies GET /runs/{run_id} returns the run status and steps.
func TestHandleGetRun_returnsRunAndSteps(t *testing.T) {
	s := newHandlerServerWithStore()
	ctx := context.Background()

	run := &types.Run{ID: "run-get", WorkflowName: "wf", Status: types.RunStatusRunning}
	if err := s.store.CreateRun(ctx, run); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}
	step := &types.Step{ID: "step-1", RunID: "run-get", Status: types.StepStatusComplete}
	if err := s.store.CreateStep(ctx, step); err != nil {
		t.Fatalf("CreateStep: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/runs/run-get", nil)
	req.SetPathValue("run_id", "run-get")
	rr := httptest.NewRecorder()
	s.handleGetRun(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rr.Code)
	}
	var body map[string]interface{}
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body["run"] == nil {
		t.Error("response missing 'run' field")
	}
	if body["steps"] == nil {
		t.Error("response missing 'steps' field")
	}
	steps, ok := body["steps"].([]interface{})
	if !ok || len(steps) != 1 {
		t.Errorf("want 1 step, got %v", body["steps"])
	}
}

// TestHandleGetRun_notFound verifies GET /runs/{run_id} returns 404 for unknown run.
func TestHandleGetRun_notFound(t *testing.T) {
	s := newHandlerServerWithStore()

	req := httptest.NewRequest(http.MethodGet, "/runs/no-such-run", nil)
	req.SetPathValue("run_id", "no-such-run")
	rr := httptest.NewRecorder()
	s.handleGetRun(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("want 404, got %d", rr.Code)
	}
}

// TestRuntimeDispatcher_Dispatch_runtimeNon202 verifies a non-202 runtime response returns an
// error immediately without waiting for a callback.
func TestRuntimeDispatcher_Dispatch_runtimeNon202(t *testing.T) {
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer runtime.Close()

	pendingCallbacks := make(map[stepCallbackKey]chan agent.CallbackPayload)
	var mu sync.Mutex

	d := &runtimeDispatcher{
		runtimeURL:      runtime.URL,
		ownURL:          "http://orchestrator",
		pendingMu:       &mu,
		pendingCallbacks: pendingCallbacks,
	}

	_, _, err := d.Dispatch(context.Background(), "run-10", "step-10", nil, nil)
	if err == nil {
		t.Fatal("want error for non-202, got nil")
	}
}

// TestRuntimeDispatcher_Dispatch_missingOutputSchema verifies that an agent config without
// an output schema causes Dispatch to return an error without calling the runtime.
func TestRuntimeDispatcher_Dispatch_missingOutputSchema(t *testing.T) {
	// Write a minimal agent config with no output field.
	dir := t.TempDir()
	agentPath := filepath.Join(dir, "no-schema.agent.yaml")
	if err := os.WriteFile(agentPath, []byte("name: no-schema\nsystem: test\n"), 0o600); err != nil {
		t.Fatalf("write agent file: %v", err)
	}

	runtimeCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		runtimeCalled = true
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	pendingCallbacks := make(map[stepCallbackKey]chan agent.CallbackPayload)
	var mu sync.Mutex

	d := &runtimeDispatcher{
		runtimeURL:       srv.URL,
		ownURL:           "http://orchestrator",
		projectDir:       dir,
		pendingMu:        &mu,
		pendingCallbacks: pendingCallbacks,
	}

	step := &config.PipelineStep{ID: "fetch", Agent: "no-schema.agent.yaml"}
	_, _, err := d.Dispatch(context.Background(), "run-schema", "fetch", step, nil)
	if err == nil {
		t.Fatal("expected error for agent missing output schema, got nil")
	}
	if runtimeCalled {
		t.Error("runtime should not have been called when agent has no output schema")
	}
}

// TestRuntimeDispatcher_Dispatch_contextCancellation verifies context cancellation unblocks
// Dispatch with an error.
func TestRuntimeDispatcher_Dispatch_contextCancellation(t *testing.T) {
	invokeReceived := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		close(invokeReceived)
	}))
	defer srv.Close()

	pendingCallbacks := make(map[stepCallbackKey]chan agent.CallbackPayload)
	var mu sync.Mutex

	d := &runtimeDispatcher{
		runtimeURL:      srv.URL,
		ownURL:          "http://orchestrator",
		pendingMu:       &mu,
		pendingCallbacks: pendingCallbacks,
	}

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		_, _, err := d.Dispatch(ctx, "run-11", "step-11", nil, nil)
		errCh <- err
	}()

	// Wait until the runtime received the invoke (channel registered), then cancel.
	<-invokeReceived
	cancel()

	err := <-errCh
	if err == nil {
		t.Fatal("want error after context cancel, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("want error wrapping context.Canceled, got %v", err)
	}
}
