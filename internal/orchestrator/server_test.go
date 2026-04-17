package orchestrator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kimitsu-ai/ktsu/internal/config"
	"github.com/kimitsu-ai/ktsu/internal/orchestrator/runner"
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

// newInvokeAuthServer creates a minimal server for testing handleInvoke auth.
func newInvokeAuthServer(t *testing.T, workflowYAML string) *server {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "myflow.workflow.yaml"), []byte(workflowYAML), 0600); err != nil {
		t.Fatalf("write workflow: %v", err)
	}
	o := &Orchestrator{cfg: Config{WorkflowDir: dir}}
	st := state.NewMemStore()
	return &server{
		o:                o,
		store:            st,
		runner:           runner.New(st),
		pendingCallbacks: make(map[stepCallbackKey]chan agent.CallbackPayload),
	}
}

func invokeRequest(t *testing.T, header, value string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/invoke/myflow", strings.NewReader("{}"))
	req.SetPathValue("workflow", "myflow")
	if header != "" {
		req.Header.Set(header, value)
	}
	return req
}

const rawAuthWorkflow = `
kind: workflow
name: myflow
version: "1.0.0"
visibility: root
invoke:
  auth:
    header: X-Telegram-Bot-Api-Secret-Token
    scheme: raw
    secret: "` + "`supersecret`" + `"
pipeline:
  - id: noop
    webhook:
      url: https://example.com
`

const bearerAuthWorkflow = `
kind: workflow
name: myflow
version: "1.0.0"
visibility: root
invoke:
  auth:
    header: Authorization
    scheme: bearer
    secret: "` + "`mytoken`" + `"
pipeline:
  - id: noop
    webhook:
      url: https://example.com
`

func TestHandleInvoke_auth_raw_missingHeader(t *testing.T) {
	s := newInvokeAuthServer(t, rawAuthWorkflow)
	rr := httptest.NewRecorder()
	s.handleInvoke(rr, invokeRequest(t, "", ""))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rr.Code)
	}
}

func TestHandleInvoke_auth_raw_wrongToken(t *testing.T) {
	s := newInvokeAuthServer(t, rawAuthWorkflow)
	rr := httptest.NewRecorder()
	s.handleInvoke(rr, invokeRequest(t, "X-Telegram-Bot-Api-Secret-Token", "wrongtoken"))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rr.Code)
	}
}

func TestHandleInvoke_auth_raw_correctToken(t *testing.T) {
	s := newInvokeAuthServer(t, rawAuthWorkflow)
	rr := httptest.NewRecorder()
	s.handleInvoke(rr, invokeRequest(t, "X-Telegram-Bot-Api-Secret-Token", "supersecret"))
	if rr.Code == http.StatusUnauthorized {
		t.Fatalf("got unexpected 401 with correct token")
	}
}

func TestHandleInvoke_auth_bearer_missingPrefix(t *testing.T) {
	s := newInvokeAuthServer(t, bearerAuthWorkflow)
	rr := httptest.NewRecorder()
	s.handleInvoke(rr, invokeRequest(t, "Authorization", "mytoken"))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rr.Code)
	}
}

func TestHandleInvoke_auth_bearer_correctToken(t *testing.T) {
	s := newInvokeAuthServer(t, bearerAuthWorkflow)
	rr := httptest.NewRecorder()
	s.handleInvoke(rr, invokeRequest(t, "Authorization", "Bearer mytoken"))
	if rr.Code == http.StatusUnauthorized {
		t.Fatalf("got unexpected 401 with correct bearer token")
	}
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

func TestHandleStepComplete_reflectCallsPassThrough(t *testing.T) {
	s := newHandlerServer()
	key := stepCallbackKey{"run-r", "step-r"}
	ch := make(chan agent.CallbackPayload, 1)
	s.pendingCallbacks[key] = ch

	payload := agent.CallbackPayload{
		RunID:  "run-r",
		StepID: "step-r",
		Status: "ok",
		Output: map[string]any{"result": "reflected"},
		Metrics: agent.Metrics{
			LLMCalls:     3,
			ReflectCalls: 1,
		},
	}

	rr := postComplete(t, s, "run-r", "step-r", payload)
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rr.Code)
	}

	received := <-ch
	if received.Metrics.ReflectCalls != 1 {
		t.Errorf("want ReflectCalls=1 in channel payload, got %d", received.Metrics.ReflectCalls)
	}
	if received.Metrics.LLMCalls != 3 {
		t.Errorf("want LLMCalls=3, got %d", received.Metrics.LLMCalls)
	}
}

func TestHandleInvoke_secondWorkspace(t *testing.T) {
	ws1 := t.TempDir()
	ws2 := t.TempDir()
	if err := os.MkdirAll(filepath.Join(ws1, "workflows"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(ws2, "workflows"), 0o755); err != nil {
		t.Fatal(err)
	}

	wfYAML := `kind: workflow
name: ws2-flow
version: "1.0.0"
pipeline:
  - id: s1
    transform:
      inputs: [{from: input}]
      ops: [{set: {output: "ok"}}]
`
	if err := os.WriteFile(filepath.Join(ws2, "workflows", "ws2-flow.workflow.yaml"), []byte(wfYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	o := New(Config{
		WorkflowDir: filepath.Join(ws1, "workflows"),
		ProjectDir:  ws1,
		Workspaces: []Workspace{
			{ProjectDir: ws2},
		},
	})
	srv, err := newServer(o)
	if err != nil {
		t.Fatalf("newServer: %v", err)
	}

	req := httptest.NewRequest("POST", "/invoke/ws2-flow", strings.NewReader("{}"))
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleInvoke_lockAutoLoad(t *testing.T) {
	// Primary workspace (no workflows)
	primary := t.TempDir()
	if err := os.MkdirAll(filepath.Join(primary, "workflows"), 0o755); err != nil {
		t.Fatal(err)
	}

	// Hub-installed workspace
	hubWs := t.TempDir()
	if err := os.MkdirAll(filepath.Join(hubWs, "workflows"), 0o755); err != nil {
		t.Fatal(err)
	}
	wfYAML := `kind: workflow
name: hub-flow
version: "1.0.0"
pipeline: []
`
	if err := os.WriteFile(filepath.Join(hubWs, "workflows", "hub-flow.workflow.yaml"), []byte(wfYAML), 0o644); err != nil {
		t.Fatal(err)
	}

	// Write ktsuhub.lock.yaml in primary
	lockContent := fmt.Sprintf(`entries:
  - name: testorg/hub-flow
    source: github.com/testorg/hub-flow
    sha: abc123
    cache: %s
    mutable: false
`, hubWs)
	if err := os.WriteFile(filepath.Join(primary, "ktsuhub.lock.yaml"), []byte(lockContent), 0o644); err != nil {
		t.Fatal(err)
	}

	o := New(Config{
		WorkflowDir: filepath.Join(primary, "workflows"),
		ProjectDir:  primary,
		// NoHubLock: false (default) — should auto-load
	})
	srv, err := newServer(o)
	if err != nil {
		t.Fatalf("newServer: %v", err)
	}

	req := httptest.NewRequest("POST", "/invoke/hub-flow", strings.NewReader("{}"))
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusAccepted {
		t.Fatalf("expected 202 (hub-flow found via lock), got %d: %s", w.Code, w.Body.String())
	}
}

func TestHandleInvoke_noHubLock(t *testing.T) {
	primary := t.TempDir()
	if err := os.MkdirAll(filepath.Join(primary, "workflows"), 0o755); err != nil {
		t.Fatal(err)
	}

	hubWs := t.TempDir()
	if err := os.MkdirAll(filepath.Join(hubWs, "workflows"), 0o755); err != nil {
		t.Fatal(err)
	}
	wfYAML := `kind: workflow
name: hub-flow
version: "1.0.0"
pipeline: []
`
	os.WriteFile(filepath.Join(hubWs, "workflows", "hub-flow.workflow.yaml"), []byte(wfYAML), 0o644)

	lockContent := fmt.Sprintf(`entries:
  - name: testorg/hub-flow
    source: github.com/testorg/hub-flow
    sha: abc123
    cache: %s
    mutable: false
`, hubWs)
	os.WriteFile(filepath.Join(primary, "ktsuhub.lock.yaml"), []byte(lockContent), 0o644)

	o := New(Config{
		WorkflowDir: filepath.Join(primary, "workflows"),
		ProjectDir:  primary,
		NoHubLock:   true, // should NOT load the lock
	})
	srv, err := newServer(o)
	if err != nil {
		t.Fatalf("newServer: %v", err)
	}

	req := httptest.NewRequest("POST", "/invoke/hub-flow", strings.NewReader("{}"))
	w := httptest.NewRecorder()
	srv.mux.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 (lock suppressed), got %d: %s", w.Code, w.Body.String())
	}
}

func TestExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home directory available")
	}

	tests := []struct {
		input string
		want  string
	}{
		{"~/foo/bar", filepath.Join(home, "foo/bar")},
		{"/absolute/path", "/absolute/path"},
		{"relative/path", "relative/path"},
		{"", ""},
		// bare ~ (no trailing slash) — should NOT be expanded
		{"~", "~"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := expandHome(tt.input)
			if got != tt.want {
				t.Errorf("expandHome(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
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

func TestHandleInvoke_subWorkflowVisibility_returns404(t *testing.T) {
	dir := t.TempDir()
	wfContent := "kind: workflow\nname: test-sub\nversion: \"1.0.0\"\nvisibility: sub-workflow\npipeline: []\n"
	if err := os.WriteFile(filepath.Join(dir, "test-sub.workflow.yaml"), []byte(wfContent), 0644); err != nil {
		t.Fatalf("write workflow: %v", err)
	}
	o := &Orchestrator{cfg: Config{WorkflowDir: dir, StoreType: "memory"}}
	srv, err := newServer(o)
	if err != nil {
		t.Fatalf("newServer: %v", err)
	}
	req := httptest.NewRequest("POST", "/invoke/test-sub", bytes.NewBufferString("{}"))
	req.SetPathValue("workflow", "test-sub")
	w := httptest.NewRecorder()
	srv.handleInvoke(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404 for sub-workflow invocation, got %d", w.Code)
	}
}

func TestHandleInvoke_noVisibilityField_allowed(t *testing.T) {
	dir := t.TempDir()
	wfContent := "kind: workflow\nname: test-default\nversion: \"1.0.0\"\npipeline: []\n"
	if err := os.WriteFile(filepath.Join(dir, "test-default.workflow.yaml"), []byte(wfContent), 0644); err != nil {
		t.Fatalf("write workflow: %v", err)
	}
	o := &Orchestrator{cfg: Config{WorkflowDir: dir, StoreType: "memory"}}
	srv, err := newServer(o)
	if err != nil {
		t.Fatalf("newServer: %v", err)
	}
	req := httptest.NewRequest("POST", "/invoke/test-default", bytes.NewBufferString("{}"))
	req.SetPathValue("workflow", "test-default")
	w := httptest.NewRecorder()
	srv.handleInvoke(w, req)
	if w.Code == http.StatusNotFound {
		t.Errorf("expected non-404 for workflow with no visibility field, got 404")
	}
}

func TestHandleInvoke_rootVisibility_allowed(t *testing.T) {
	dir := t.TempDir()
	wfContent := "kind: workflow\nname: test-root\nversion: \"1.0.0\"\nvisibility: root\npipeline: []\n"
	if err := os.WriteFile(filepath.Join(dir, "test-root.workflow.yaml"), []byte(wfContent), 0644); err != nil {
		t.Fatalf("write workflow: %v", err)
	}
	o := &Orchestrator{cfg: Config{WorkflowDir: dir, StoreType: "memory"}}
	srv, err := newServer(o)
	if err != nil {
		t.Fatalf("newServer: %v", err)
	}
	req := httptest.NewRequest("POST", "/invoke/test-root", bytes.NewBufferString("{}"))
	req.SetPathValue("workflow", "test-root")
	w := httptest.NewRecorder()
	srv.handleInvoke(w, req)
	if w.Code == http.StatusNotFound {
		t.Errorf("expected non-404 for root workflow, got 404")
	}
}

func newHandlerServerWithStoreAndRuntime(t *testing.T, runtimeURL string) *server {
	t.Helper()
	return &server{
		o: &Orchestrator{cfg: Config{
			RuntimeURL:  runtimeURL,
			WorkflowDir: t.TempDir(),
		}},
		store:            state.NewMemStore(),
		pendingCallbacks: make(map[stepCallbackKey]chan agent.CallbackPayload),
	}
}

func TestHandleStepComplete_pendingApprovalNotSignaled(t *testing.T) {
	s := newHandlerServerWithStore()
	ctx := context.Background()

	// Pre-create run and step
	_ = s.store.CreateRun(ctx, &types.Run{ID: "run-pa", WorkflowName: "wf", Status: types.RunStatusRunning})
	_ = s.store.CreateStep(ctx, &types.Step{ID: "step-pa", RunID: "run-pa", Status: types.StepStatusRunning})

	key := stepCallbackKey{"run-pa", "step-pa"}
	ch := make(chan agent.CallbackPayload, 1)
	s.pendingCallbacks[key] = ch

	rr := postComplete(t, s, "run-pa", "step-pa", agent.CallbackPayload{
		RunID:  "run-pa",
		StepID: "step-pa",
		Status: "pending_approval",
		Metrics: agent.Metrics{TokensIn: 100, CostUSD: 0.001},
		PendingApproval: &agent.PendingApproval{
			ToolName:  "delete-user",
			ToolUseID: "tc1",
			OnReject:  "fail",
		},
		OriginalRequest: json.RawMessage(`{"run_id":"run-pa","step_id":"step-pa"}`),
	})

	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rr.Code)
	}

	// Channel must NOT be signaled.
	select {
	case got := <-ch:
		t.Errorf("channel must not be signaled for pending_approval, got status=%q", got.Status)
	default:
	}

	// Approval must be in store.
	approval, err := s.store.GetApproval(ctx, "run-pa", "step-pa")
	if err != nil {
		t.Fatalf("GetApproval: %v", err)
	}
	if approval.Status != types.ApprovalStatusPending {
		t.Errorf("want pending approval, got %q", approval.Status)
	}
	if approval.ToolName != "delete-user" {
		t.Errorf("want tool_name=delete-user, got %q", approval.ToolName)
	}
	if approval.PartialMetrics.TokensIn != 100 {
		t.Errorf("want partial_metrics.tokens_in=100, got %d", approval.PartialMetrics.TokensIn)
	}

	// Step status must be pending_approval.
	step, err := s.store.GetStep(ctx, "run-pa", "step-pa")
	if err != nil {
		t.Fatalf("GetStep: %v", err)
	}
	if step.Status != types.StepStatusPendingApproval {
		t.Errorf("want step status pending_approval, got %q", step.Status)
	}
}

func TestHandleDecideApproval_Approved_DispatchesToRuntime(t *testing.T) {
	runtimeReceived := make(chan agent.InvokeRequest, 1)
	runtime := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req agent.InvokeRequest
		json.NewDecoder(r.Body).Decode(&req)
		runtimeReceived <- req
		w.WriteHeader(http.StatusAccepted)
	}))
	defer runtime.Close()

	s := newHandlerServerWithStoreAndRuntime(t, runtime.URL)
	ctx := context.Background()

	_ = s.store.CreateRun(ctx, &types.Run{ID: "run-dec", WorkflowName: "wf", Status: types.RunStatusRunning})
	_ = s.store.CreateStep(ctx, &types.Step{
		ID:       "step-dec",
		RunID:    "run-dec",
		Status:   types.StepStatusPendingApproval,
		Messages: json.RawMessage(`[{"role":"user","content":"{}"}]`),
	})
	originalReq := agent.InvokeRequest{RunID: "run-dec", StepID: "step-dec", AgentName: "test-agent"}
	originalJSON, _ := json.Marshal(originalReq)
	_ = s.store.CreateApproval(ctx, &types.Approval{
		RunID:           "run-dec",
		StepID:          "step-dec",
		ToolName:        "delete-user",
		ToolUseID:       "tc1",
		OnReject:        "fail",
		Status:          types.ApprovalStatusPending,
		CreatedAt:       time.Now(),
		OriginalRequest: json.RawMessage(originalJSON),
	})

	body, _ := json.Marshal(map[string]string{"decision": "approved"})
	req := httptest.NewRequest(http.MethodPost, "/runs/run-dec/steps/step-dec/approval/decide", bytes.NewReader(body))
	req.SetPathValue("run_id", "run-dec")
	req.SetPathValue("step_id", "step-dec")
	rr := httptest.NewRecorder()
	s.handleDecideApproval(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
	}

	select {
	case invoked := <-runtimeReceived:
		if !invoked.IsResume {
			t.Error("expected IsResume=true in runtime invoke")
		}
		if len(invoked.ApprovedToolCalls) == 0 || invoked.ApprovedToolCalls[0] != "tc1" {
			t.Errorf("expected ApprovedToolCalls=[tc1], got %v", invoked.ApprovedToolCalls)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for runtime to receive invoke")
	}

	// Approval status must be updated to approved.
	approval, _ := s.store.GetApproval(ctx, "run-dec", "step-dec")
	if approval.Status != types.ApprovalStatusApproved {
		t.Errorf("want approved, got %q", approval.Status)
	}
}

func TestHandleDecideApproval_Rejected_FailSignalsChannel(t *testing.T) {
	s := newHandlerServerWithStoreAndRuntime(t, "")
	ctx := context.Background()

	_ = s.store.CreateRun(ctx, &types.Run{ID: "run-rej", WorkflowName: "wf", Status: types.RunStatusRunning})
	_ = s.store.CreateStep(ctx, &types.Step{ID: "step-rej", RunID: "run-rej", Status: types.StepStatusPendingApproval})
	_ = s.store.CreateApproval(ctx, &types.Approval{
		RunID:     "run-rej",
		StepID:    "step-rej",
		ToolName:  "delete-user",
		ToolUseID: "tc2",
		OnReject:  "fail",
		Status:    types.ApprovalStatusPending,
		CreatedAt: time.Now(),
		OriginalRequest: json.RawMessage(`{}`),
		PartialMetrics: types.StepMetrics{TokensIn: 50},
	})

	// Register pending channel
	key := stepCallbackKey{"run-rej", "step-rej"}
	ch := make(chan agent.CallbackPayload, 1)
	s.pendingCallbacks[key] = ch

	body, _ := json.Marshal(map[string]string{"decision": "rejected"})
	req := httptest.NewRequest(http.MethodPost, "/runs/run-rej/steps/step-rej/approval/decide", bytes.NewReader(body))
	req.SetPathValue("run_id", "run-rej")
	req.SetPathValue("step_id", "step-rej")
	rr := httptest.NewRecorder()
	s.handleDecideApproval(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rr.Code)
	}

	select {
	case got := <-ch:
		if got.Status != "failed" {
			t.Errorf("want status=failed, got %q", got.Status)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for channel signal")
	}
}

func TestHandleStepComplete_ResumeAccumulatesMetrics(t *testing.T) {
	s := newHandlerServerWithStore()
	ctx := context.Background()

	_ = s.store.CreateRun(ctx, &types.Run{ID: "run-res", WorkflowName: "wf", Status: types.RunStatusRunning})
	_ = s.store.CreateStep(ctx, &types.Step{ID: "step-res", RunID: "run-res", Status: types.StepStatusPendingApproval})
	_ = s.store.CreateApproval(ctx, &types.Approval{
		RunID:  "run-res",
		StepID: "step-res",
		Status: types.ApprovalStatusApproved,
		PartialMetrics: types.StepMetrics{
			TokensIn:  100,
			TokensOut: 50,
			CostUSD:   0.001,
			LLMCalls:  1,
			ToolCalls: 0,
		},
	})

	key := stepCallbackKey{"run-res", "step-res"}
	ch := make(chan agent.CallbackPayload, 1)
	s.pendingCallbacks[key] = ch

	// Resume callback with metrics from the second leg.
	rr := postComplete(t, s, "run-res", "step-res", agent.CallbackPayload{
		RunID:    "run-res",
		StepID:   "step-res",
		Status:   "ok",
		IsResume: true,
		Output:   map[string]any{"result": "done"},
		Metrics: agent.Metrics{
			TokensIn:  80,
			TokensOut: 30,
			CostUSD:   0.0008,
			LLMCalls:  1,
			ToolCalls: 1,
		},
	})

	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rr.Code)
	}

	got := <-ch
	if got.Metrics.TokensIn != 180 {
		t.Errorf("want accumulated tokens_in=180, got %d", got.Metrics.TokensIn)
	}
	if got.Metrics.CostUSD != 0.0018 {
		t.Errorf("want accumulated cost_usd=0.0018, got %f", got.Metrics.CostUSD)
	}
	if got.Metrics.LLMCalls != 2 {
		t.Errorf("want accumulated llm_calls=2, got %d", got.Metrics.LLMCalls)
	}
	if got.Metrics.ToolCalls != 1 {
		t.Errorf("want accumulated tool_calls=1, got %d", got.Metrics.ToolCalls)
	}
}

func TestHandleListRuns(t *testing.T) {
	ctx := context.Background()
	s := newHandlerServerWithStore()

	now := time.Now()
	runs := []*types.Run{
		{ID: "run-1", WorkflowName: "wf-a", Status: types.RunStatusComplete, CreatedAt: now.Add(-2 * time.Minute), UpdatedAt: now},
		{ID: "run-2", WorkflowName: "wf-b", Status: types.RunStatusFailed, CreatedAt: now.Add(-1 * time.Minute), UpdatedAt: now},
	}
	for _, r := range runs {
		if err := s.store.CreateRun(ctx, r); err != nil {
			t.Fatalf("CreateRun: %v", err)
		}
	}

	t.Run("no filter returns all runs", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/runs", nil)
		rr := httptest.NewRecorder()
		s.handleListRuns(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body)
		}
		var result struct {
			Runs []types.Run `json:"runs"`
		}
		if err := json.NewDecoder(rr.Body).Decode(&result); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if len(result.Runs) != 2 {
			t.Errorf("want 2 runs, got %d", len(result.Runs))
		}
	})

	t.Run("filter by workflow", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/runs?workflow=wf-a", nil)
		rr := httptest.NewRecorder()
		s.handleListRuns(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("want 200, got %d", rr.Code)
		}
		var result struct {
			Runs []types.Run `json:"runs"`
		}
		json.NewDecoder(rr.Body).Decode(&result)
		if len(result.Runs) != 1 || result.Runs[0].ID != "run-1" {
			t.Errorf("want run-1 only, got %v", result.Runs)
		}
	})

	t.Run("filter by status", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/runs?status=failed", nil)
		rr := httptest.NewRecorder()
		s.handleListRuns(rr, req)

		var result struct {
			Runs []types.Run `json:"runs"`
		}
		json.NewDecoder(rr.Body).Decode(&result)
		if len(result.Runs) != 1 || result.Runs[0].ID != "run-2" {
			t.Errorf("want run-2 only, got %v", result.Runs)
		}
	})
}

func TestHandleGetEnvelope_NewPath(t *testing.T) {
	ctx := context.Background()
	s := newHandlerServerWithStore()

	run := &types.Run{
		ID:           "run-env",
		WorkflowName: "wf-env",
		Status:       types.RunStatusComplete,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	if err := s.store.CreateRun(ctx, run); err != nil {
		t.Fatalf("CreateRun: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/runs/run-env/envelope", nil)
	req.SetPathValue("run_id", "run-env")
	rr := httptest.NewRecorder()
	s.handleGetEnvelope(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body)
	}
}

func TestHandleListApprovals_ReturnsPendingApprovals(t *testing.T) {
	s := newHandlerServerWithStore()
	ctx := context.Background()

	_ = s.store.CreateApproval(ctx, &types.Approval{
		RunID: "run-list", StepID: "step-1", ToolName: "delete-x",
		Status: types.ApprovalStatusPending, CreatedAt: time.Now(),
	})
	_ = s.store.CreateApproval(ctx, &types.Approval{
		RunID: "run-list", StepID: "step-2", ToolName: "update-y",
		Status: types.ApprovalStatusApproved, CreatedAt: time.Now(),
	})

	req := httptest.NewRequest(http.MethodGet, "/approvals", nil)
	rr := httptest.NewRecorder()
	s.handleListApprovals(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rr.Code)
	}

	var approvals []*types.Approval
	if err := json.NewDecoder(rr.Body).Decode(&approvals); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(approvals) != 1 {
		t.Errorf("want 1 pending approval, got %d", len(approvals))
	}
	if len(approvals) > 0 && approvals[0].ToolName != "delete-x" {
		t.Errorf("wrong approval returned: %q", approvals[0].ToolName)
	}
}
