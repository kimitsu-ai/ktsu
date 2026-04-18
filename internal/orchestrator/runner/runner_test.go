package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kimitsu-ai/ktsu/internal/config"
	"github.com/kimitsu-ai/ktsu/internal/orchestrator/state"
	"github.com/kimitsu-ai/ktsu/pkg/types"
)

// --- StepType constants ---

func TestStepTypeWorkflow_constant(t *testing.T) {
	if types.StepTypeWorkflow != "workflow" {
		t.Errorf("StepTypeWorkflow should be \"workflow\", got %q", types.StepTypeWorkflow)
	}
}

func makeWorkflow(steps ...config.PipelineStep) *config.WorkflowConfig {
	return &config.WorkflowConfig{
		Kind:     "workflow",
		Name:     "test-workflow",
		Pipeline: steps,
	}
}

// mockDispatcher returns preconfigured outputs keyed by step ID.
type mockDispatcher struct {
	outputs map[string]map[string]interface{}
}

func (m *mockDispatcher) Dispatch(_ context.Context, _, stepID string, _ *config.PipelineStep, _ map[string]interface{}) (map[string]interface{}, types.StepMetrics, error) {
	if out, ok := m.outputs[stepID]; ok {
		return out, types.StepMetrics{}, nil
	}
	return map[string]interface{}{"stubbed": true}, types.StepMetrics{}, nil
}

// capturingDispatcher records every Dispatch call for fanout inspection.
type capturingDispatcher struct {
	mu      sync.Mutex
	calls   []capturedCall
	outputs map[string]interface{} // returned for every call
}

type capturedCall struct {
	stepID string
	input  map[string]interface{}
}

func (c *capturingDispatcher) Dispatch(_ context.Context, _, stepID string, _ *config.PipelineStep, input map[string]interface{}) (map[string]interface{}, types.StepMetrics, error) {
	c.mu.Lock()
	c.calls = append(c.calls, capturedCall{stepID: stepID, input: input})
	c.mu.Unlock()
	out := map[string]interface{}{"processed": true}
	for k, v := range c.outputs {
		out[k] = v
	}
	return out, types.StepMetrics{TokensIn: 10, TokensOut: 5}, nil
}

// failingDispatcher fails dispatch calls whose stepID matches any entry in failStepIDs.
// All other calls return successOutput.
type failingDispatcher struct {
	failStepIDs   map[string]bool
	failErr       error
	successOutput map[string]interface{}
}

func (f *failingDispatcher) Dispatch(_ context.Context, _, stepID string, _ *config.PipelineStep, _ map[string]interface{}) (map[string]interface{}, types.StepMetrics, error) {
	if f.failStepIDs[stepID] {
		return nil, types.StepMetrics{TokensIn: 5, TokensOut: 2, CostUSD: 0.0001, LLMCalls: 1}, f.failErr
	}
	return f.successOutput, types.StepMetrics{TokensIn: 10, TokensOut: 5, CostUSD: 0.001, LLMCalls: 1}, nil
}

// TestRunner_envelopePayload verifies that the invoke payload is stored on the envelope record.
func TestRunner_envelopePayload(t *testing.T) {
	store := state.NewMemStore()
	r := NewWithDispatcher(store, &mockDispatcher{
		outputs: map[string]map[string]interface{}{
			"step1": {"ok": true},
		},
	})

	wf := makeWorkflow(config.PipelineStep{
		ID:    "step1",
		Agent: "agents/foo.agent.yaml",
	})

	payload := map[string]interface{}{"name": "world"}
	ctx := context.Background()
	if err := r.Execute(ctx, "test-workflow", "run-payload", wf, payload, nil); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	env, err := store.GetEnvelope(ctx, "run-payload")
	if err != nil {
		t.Fatalf("GetEnvelope: %v", err)
	}
	if env.Payload == nil {
		t.Fatal("expected envelope payload to be set")
	}
	if env.Payload["name"] != "world" {
		t.Errorf("expected payload.name=world, got %v", env.Payload["name"])
	}
}

// TestRunner_workflowParams verifies the workflow params are available to pipeline steps.
func TestRunner_workflowParams(t *testing.T) {
	store := state.NewMemStore()
	r := NewWithDispatcher(store, &mockDispatcher{
		outputs: map[string]map[string]interface{}{
			"step1": {"received": true},
		},
	})

	wf := makeWorkflow(config.PipelineStep{
		ID:    "step1",
		Agent: "agents/foo.agent.yaml",
	})

	ctx := context.Background()
	err := r.Execute(ctx, "test-workflow", "run-input", wf, map[string]interface{}{"message": "hello"}, nil)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	step, err := store.GetStep(ctx, "run-input", "step1")
	if err != nil {
		t.Fatalf("GetStep failed: %v", err)
	}
	if step.Status != types.StepStatusComplete {
		t.Errorf("expected step status complete, got %s", step.Status)
	}
}

// TestRunner_transformStep_merge verifies two step outputs are merged via transform.
func TestRunner_transformStep_merge(t *testing.T) {
	store := state.NewMemStore()
	r := NewWithDispatcher(store, &mockDispatcher{
		outputs: map[string]map[string]interface{}{
			"step1": {"a": float64(1)},
			"step2": {"b": float64(2)},
		},
	})

	wf := makeWorkflow(
		config.PipelineStep{
			ID:    "step1",
			Agent: "agents/a.agent.yaml",
		},
		config.PipelineStep{
			ID:    "step2",
			Agent: "agents/b.agent.yaml",
		},
		config.PipelineStep{
			ID:        "merge_step",
			DependsOn: []string{"step1", "step2"},
			Transform: &config.TransformSpec{
				Inputs: []config.TransformInput{
					{From: "step1"},
					{From: "step2"},
				},
				Ops: []map[string]interface{}{
					{"merge": []interface{}{"step1", "step2"}},
				},
			},
		},
	)

	ctx := context.Background()
	err := r.Execute(ctx, "test-workflow", "run-merge", wf, nil, nil)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	step, err := store.GetStep(ctx, "run-merge", "merge_step")
	if err != nil {
		t.Fatalf("GetStep failed: %v", err)
	}
	if step.Status != types.StepStatusComplete {
		t.Errorf("expected complete, got %s", step.Status)
	}
	result, ok := step.Output["result"]
	if !ok {
		t.Fatalf("missing result key in output: %v", step.Output)
	}
	resultMap, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("result is not a map: %T", result)
	}
	if _, hasA := resultMap["a"]; !hasA {
		t.Errorf("merged result missing 'a': %v", resultMap)
	}
	if _, hasB := resultMap["b"]; !hasB {
		t.Errorf("merged result missing 'b': %v", resultMap)
	}
}

// TestRunner_transformStep_filter verifies filter op on an array from a prior step output.
func TestRunner_transformStep_filter(t *testing.T) {
	items := []interface{}{
		map[string]interface{}{"status": "active", "name": "item1"},
		map[string]interface{}{"status": "inactive", "name": "item2"},
	}
	store := state.NewMemStore()
	r := NewWithDispatcher(store, &mockDispatcher{
		outputs: map[string]map[string]interface{}{
			"data_provider": {"items": items},
		},
	})

	wf := makeWorkflow(
		config.PipelineStep{
			ID:    "data_provider",
			Agent: "agents/data.agent.yaml",
		},
		config.PipelineStep{
			ID:        "filter_step",
			DependsOn: []string{"data_provider"},
			Transform: &config.TransformSpec{
				Inputs: []config.TransformInput{{From: "data_provider"}},
				Ops: []map[string]interface{}{
					{"map": map[string]interface{}{"expr": "items"}},
					{"flatten": nil},
					{"filter": map[string]interface{}{"expr": "status == 'active'"}},
				},
			},
		},
	)

	ctx := context.Background()
	err := r.Execute(ctx, "test-workflow", "run-filter", wf, nil, nil)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	step, err := store.GetStep(ctx, "run-filter", "filter_step")
	if err != nil {
		t.Fatalf("GetStep failed: %v", err)
	}
	if step.Status != types.StepStatusComplete {
		t.Errorf("expected complete, got %s", step.Status)
	}
	result, ok := step.Output["result"]
	if !ok {
		t.Fatalf("missing result in output: %v", step.Output)
	}
	resultSlice, ok := result.([]interface{})
	if !ok {
		t.Fatalf("result is not a slice: %T %v", result, result)
	}
	if len(resultSlice) != 1 {
		t.Errorf("expected 1 active item, got %d: %v", len(resultSlice), resultSlice)
	}
}

// TestRunner_transformStep_sort verifies sort op on array from a prior step output.
func TestRunner_transformStep_sort(t *testing.T) {
	items := []interface{}{
		map[string]interface{}{"name": "Charlie", "age": float64(30)},
		map[string]interface{}{"name": "Alice", "age": float64(25)},
		map[string]interface{}{"name": "Bob", "age": float64(28)},
	}
	store := state.NewMemStore()
	r := NewWithDispatcher(store, &mockDispatcher{
		outputs: map[string]map[string]interface{}{
			"data_provider": {"items": items},
		},
	})

	wf := makeWorkflow(
		config.PipelineStep{
			ID:    "data_provider",
			Agent: "agents/data.agent.yaml",
		},
		config.PipelineStep{
			ID:        "sort_step",
			DependsOn: []string{"data_provider"},
			Transform: &config.TransformSpec{
				Inputs: []config.TransformInput{{From: "data_provider"}},
				Ops: []map[string]interface{}{
					{"map": map[string]interface{}{"expr": "items"}},
					{"flatten": nil},
					{"sort": map[string]interface{}{"field": "age", "order": "asc"}},
				},
			},
		},
	)

	ctx := context.Background()
	err := r.Execute(ctx, "test-workflow", "run-sort", wf, nil, nil)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	step, err := store.GetStep(ctx, "run-sort", "sort_step")
	if err != nil {
		t.Fatalf("GetStep failed: %v", err)
	}
	if step.Status != types.StepStatusComplete {
		t.Errorf("expected complete, got %s", step.Status)
	}
	resultSlice := step.Output["result"].([]interface{})
	if len(resultSlice) != 3 {
		t.Fatalf("expected 3 items, got %d", len(resultSlice))
	}
	first := resultSlice[0].(map[string]interface{})
	if first["name"] != "Alice" {
		t.Errorf("expected Alice first, got %v", first["name"])
	}
}

// TestRunner_webhookStep_success verifies a webhook step POSTs to the URL and completes on 200.
func TestRunner_webhookStep_success(t *testing.T) {
	var receivedBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var err error
		receivedBody, err = io.ReadAll(req.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	store := state.NewMemStore()
	r := New(store)

	wf := makeWorkflow(config.PipelineStep{
		ID: "notify",
		Webhook: &config.WebhookSpec{
			URL:    srv.URL,
			Method: "POST",
			Body: map[string]interface{}{
				"message": "params.msg",
			},
		},
	})

	ctx := context.Background()
	err := r.Execute(ctx, "test-workflow", "run-webhook", wf, map[string]interface{}{"msg": "hello world"}, nil)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	step, err := store.GetStep(ctx, "run-webhook", "notify")
	if err != nil {
		t.Fatalf("GetStep failed: %v", err)
	}
	if step.Status != types.StepStatusComplete {
		t.Errorf("expected complete, got %s", step.Status)
	}
	if step.Output["sent"] != true {
		t.Errorf("expected sent=true, got %v", step.Output["sent"])
	}
	if len(receivedBody) == 0 {
		t.Error("server did not receive any body")
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(receivedBody, &payload); err != nil {
		t.Errorf("invalid JSON received: %v", err)
	}
}

// TestRunner_webhookStep_non2xx verifies a non-2xx response fails the step.
func TestRunner_webhookStep_non2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "bad request", http.StatusBadRequest)
	}))
	defer srv.Close()

	store := state.NewMemStore()
	r := New(store)

	wf := makeWorkflow(config.PipelineStep{
		ID: "notify",
		Webhook: &config.WebhookSpec{
			URL: srv.URL,
		},
	})

	ctx := context.Background()
	err := r.Execute(ctx, "test-workflow", "run-webhook-fail", wf, nil, nil)
	if err == nil {
		t.Fatal("expected Execute to fail on non-2xx webhook, but it succeeded")
	}

	run, _ := store.GetRun(ctx, "run-webhook-fail")
	if run.Status != types.RunStatusFailed {
		t.Errorf("expected run failed, got %s", run.Status)
	}
}

// TestRunner_webhookStep_condition_false verifies webhook step is skipped when condition is false.
func TestRunner_webhookStep_condition_false(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	store := state.NewMemStore()
	r := New(store)

	wf := makeWorkflow(config.PipelineStep{
		ID:        "notify",
		Condition: "nonexistent.field", // evaluates to nil → skip
		Webhook: &config.WebhookSpec{
			URL: srv.URL,
		},
	})

	ctx := context.Background()
	err := r.Execute(ctx, "test-workflow", "run-cond", wf, nil, nil)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	step, err := store.GetStep(ctx, "run-cond", "notify")
	if err != nil {
		t.Fatalf("GetStep failed: %v", err)
	}
	if step.Status != types.StepStatusSkipped {
		t.Errorf("expected skipped, got %s", step.Status)
	}
	if called {
		t.Error("webhook should not have been called when condition is false")
	}
}

// TestRunner_agentStep_stub verifies an agent step without a dispatcher returns stub output.
func TestRunner_agentStep_stub(t *testing.T) {
	store := state.NewMemStore()
	r := New(store)

	wf := makeWorkflow(config.PipelineStep{
		ID:    "agent_step",
		Agent: "agents/foo.agent.yaml@1.0.0",
	})

	ctx := context.Background()
	err := r.Execute(ctx, "test-workflow", "run-agent", wf, nil, nil)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	step, err := store.GetStep(ctx, "run-agent", "agent_step")
	if err != nil {
		t.Fatalf("GetStep failed: %v", err)
	}
	if step.Status != types.StepStatusComplete {
		t.Errorf("expected complete, got %s", step.Status)
	}
	if step.Output["stubbed"] != true {
		t.Errorf("expected stubbed=true, got %v", step.Output["stubbed"])
	}
}

// TestRunner_reservedField_injectionAttempt verifies run fails on injection attempt from agent.
func TestRunner_reservedField_injectionAttempt(t *testing.T) {
	store := state.NewMemStore()
	r := NewWithDispatcher(store, &mockDispatcher{
		outputs: map[string]map[string]interface{}{
			"step1": {"ktsu_injection_attempt": true, "data": "something"},
		},
	})

	wf := makeWorkflow(config.PipelineStep{
		ID:    "step1",
		Agent: "agents/foo.agent.yaml",
	})

	ctx := context.Background()
	err := r.Execute(ctx, "test-workflow", "run-injection", wf, nil, nil)
	if err == nil {
		t.Fatal("expected Execute to fail on injection attempt")
	}

	run, _ := store.GetRun(ctx, "run-injection")
	if run.Status != types.RunStatusFailed {
		t.Errorf("expected run failed, got %s", run.Status)
	}
}

// TestRunner_reservedField_needsHuman verifies run fails when ktsu_needs_human is true.
func TestRunner_reservedField_needsHuman(t *testing.T) {
	store := state.NewMemStore()
	r := NewWithDispatcher(store, &mockDispatcher{
		outputs: map[string]map[string]interface{}{
			"step1": {"ktsu_needs_human": true, "data": "something"},
		},
	})

	wf := makeWorkflow(config.PipelineStep{
		ID:    "step1",
		Agent: "agents/foo.agent.yaml",
	})

	ctx := context.Background()
	err := r.Execute(ctx, "test-workflow", "run-needs-human", wf, nil, nil)
	if err == nil {
		t.Fatal("expected Execute to fail on needs_human")
	}

	run, _ := store.GetRun(ctx, "run-needs-human")
	if run.Status != types.RunStatusFailed {
		t.Errorf("expected run failed, got %s", run.Status)
	}
}

// TestRunner_reservedField_skipReason verifies step is marked skipped.
func TestRunner_reservedField_skipReason(t *testing.T) {
	store := state.NewMemStore()
	r := NewWithDispatcher(store, &mockDispatcher{
		outputs: map[string]map[string]interface{}{
			"step1": {"ktsu_skip_reason": "nothing to process", "data": "value"},
		},
	})

	wf := makeWorkflow(config.PipelineStep{
		ID:    "step1",
		Agent: "agents/foo.agent.yaml",
	})

	ctx := context.Background()
	err := r.Execute(ctx, "test-workflow", "run-skip", wf, nil, nil)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	step, _ := store.GetStep(ctx, "run-skip", "step1")
	if step.Status != types.StepStatusSkipped {
		t.Errorf("expected skipped, got %s", step.Status)
	}
}

// TestRunner_reservedField_confidence_below_threshold verifies step fails when confidence is low.
func TestRunner_reservedField_confidence_below_threshold(t *testing.T) {
	store := state.NewMemStore()
	r := NewWithDispatcher(store, &mockDispatcher{
		outputs: map[string]map[string]interface{}{
			"step1": {"ktsu_confidence": 0.5, "data": "output"},
		},
	})

	wf := makeWorkflow(config.PipelineStep{
		ID:                  "step1",
		Agent:               "agents/foo.agent.yaml",
		ConfidenceThreshold: 0.8,
	})

	ctx := context.Background()
	err := r.Execute(ctx, "test-workflow", "run-conf", wf, nil, nil)
	if err == nil {
		t.Fatal("expected Execute to fail due to low confidence")
	}

	step, _ := store.GetStep(ctx, "run-conf", "step1")
	if step.Status != types.StepStatusFailed {
		t.Errorf("expected failed, got %s", step.Status)
	}
}

// TestRunner_multiStep_dag verifies multi-step pipeline executes in DAG order.
func TestRunner_multiStep_dag(t *testing.T) {
	store := state.NewMemStore()
	r := NewWithDispatcher(store, &mockDispatcher{
		outputs: map[string]map[string]interface{}{
			"step_a": {"name": "Bob"},
		},
	})

	wf := makeWorkflow(
		config.PipelineStep{
			ID:    "step_a",
			Agent: "agents/a.agent.yaml",
		},
		config.PipelineStep{
			ID:        "step_b",
			DependsOn: []string{"step_a"},
			Transform: &config.TransformSpec{
				Inputs: []config.TransformInput{{From: "step_a"}},
				Ops:    []map[string]interface{}{},
			},
		},
		config.PipelineStep{
			ID:        "step_c",
			DependsOn: []string{"step_b"},
			Webhook: &config.WebhookSpec{
				URL: "http://127.0.0.1:1", // unreachable — we test via condition skip instead
			},
			Condition: "step_a.missing_field", // false → skipped
		},
	)

	ctx := context.Background()
	err := r.Execute(ctx, "test-workflow", "run-dag", wf, nil, nil)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	for _, tc := range []struct {
		id     string
		status types.StepStatus
	}{
		{"step_a", types.StepStatusComplete},
		{"step_b", types.StepStatusComplete},
		{"step_c", types.StepStatusSkipped},
	} {
		step, err := store.GetStep(ctx, "run-dag", tc.id)
		if err != nil {
			t.Fatalf("GetStep(%s) failed: %v", tc.id, err)
		}
		if step.Status != tc.status {
			t.Errorf("step %s: expected %s, got %s", tc.id, tc.status, step.Status)
		}
	}

	stepB, _ := store.GetStep(ctx, "run-dag", "step_b")
	if _, hasResult := stepB.Output["result"]; !hasResult {
		t.Errorf("step_b should have result from transform, got: %v", stepB.Output)
	}
}

// TestRunner_timeout_failsRunOnDeadline verifies that a workflow with timeout_s fires and marks the run failed.
func TestRunner_timeout_failsRunOnDeadline(t *testing.T) {
	// unblock is closed after Execute returns so the hanging handler can exit cleanly.
	unblock := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		// Block until the run timeout fires (signalled via unblock).
		<-unblock
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	store := state.NewMemStore()
	r := New(store)

	wf := makeWorkflow(config.PipelineStep{
		ID: "slow",
		Webhook: &config.WebhookSpec{
			URL:      srv.URL,
			TimeoutS: 30, // webhook's own timeout is generous; run timeout fires first
		},
	})
	wf.ModelPolicy = &config.ModelPolicy{TimeoutS: 1} // 1-second run deadline

	start := time.Now()
	ctx := context.Background()
	err := r.Execute(ctx, "test-workflow", "run-timeout", wf, nil, nil)
	elapsed := time.Since(start)
	close(unblock) // release the handler goroutine

	if err == nil {
		t.Fatal("want error from timed-out run, got nil")
	}
	// Verify the run deadline (1s) fired, not the webhook timeout (30s).
	if elapsed > 10*time.Second {
		t.Errorf("run should have been cancelled in ~1s (run timeout), took %v", elapsed)
	}

	run, gerr := store.GetRun(ctx, "run-timeout")
	if gerr != nil {
		t.Fatalf("GetRun: %v", gerr)
	}
	if run.Status != types.RunStatusFailed {
		t.Errorf("want run status failed, got %s", run.Status)
	}
	if run.Error == "" {
		t.Error("want non-empty run error message")
	}
}

// TestRunner_webhookStep_envBodyValue verifies env:VAR_NAME body values are resolved from the environment.
func TestRunner_webhookStep_envBodyValue(t *testing.T) {
	t.Setenv("WEBHOOK_TENANT", "tenant-xyz")

	var receivedBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var err error
		receivedBody, err = io.ReadAll(req.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	store := state.NewMemStore()
	r := New(store)

	wf := makeWorkflow(config.PipelineStep{
		ID: "notify",
		Webhook: &config.WebhookSpec{
			URL:    srv.URL,
			Method: "POST",
			Body: map[string]interface{}{
				"tenant_id": "env:WEBHOOK_TENANT",
			},
		},
	})

	ctx := context.Background()
	if err := r.Execute(ctx, "test-workflow", "run-env-body", wf, nil, nil); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(receivedBody, &payload); err != nil {
		t.Fatalf("invalid JSON received: %v", err)
	}
	if payload["tenant_id"] != "tenant-xyz" {
		t.Errorf("want tenant_id=tenant-xyz, got %v", payload["tenant_id"])
	}
}

// TestRunner_fanout_basic verifies each item in the array is dispatched once.
func TestRunner_fanout_basic(t *testing.T) {
	d := &capturingDispatcher{}
	store := state.NewMemStore()
	r := NewWithDispatcher(store, d)

	wf := makeWorkflow(config.PipelineStep{
		ID:    "process",
		Agent: "agents/processor.agent.yaml",
		ForEach: &config.ForEachSpec{
			From: "params.items",
		},
	})

	input := map[string]interface{}{
		"items": []interface{}{"a", "b", "c"},
	}

	ctx := context.Background()
	if err := r.Execute(ctx, "test-workflow", "run-fanout", wf, input, nil); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if len(d.calls) != 3 {
		t.Fatalf("expected 3 dispatches, got %d", len(d.calls))
	}

	step, err := store.GetStep(ctx, "run-fanout", "process")
	if err != nil {
		t.Fatalf("GetStep failed: %v", err)
	}
	if step.Status != types.StepStatusComplete {
		t.Errorf("expected complete, got %s", step.Status)
	}
	results, ok := step.Output["results"].([]interface{})
	if !ok {
		t.Fatalf("expected results array, got %T: %v", step.Output["results"], step.Output)
	}
	if len(results) != 3 {
		t.Errorf("expected 3 results, got %d", len(results))
	}
}

// TestRunner_fanout_maxItems verifies MaxItems caps the number of dispatches.
func TestRunner_fanout_maxItems(t *testing.T) {
	d := &capturingDispatcher{}
	store := state.NewMemStore()
	r := NewWithDispatcher(store, d)

	wf := makeWorkflow(config.PipelineStep{
		ID:    "process",
		Agent: "agents/processor.agent.yaml",
		ForEach: &config.ForEachSpec{
			From:     "params.items",
			MaxItems: 2,
		},
	})

	input := map[string]interface{}{
		"items": []interface{}{"a", "b", "c", "d", "e"},
	}

	ctx := context.Background()
	if err := r.Execute(ctx, "test-workflow", "run-fanout-max", wf, input, nil); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if len(d.calls) != 2 {
		t.Errorf("expected 2 dispatches (MaxItems=2), got %d", len(d.calls))
	}

	step, _ := store.GetStep(ctx, "run-fanout-max", "process")
	results := step.Output["results"].([]interface{})
	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}
}

// TestRunner_fanout_itemAndIndex verifies item and item_index are injected into each dispatch.
func TestRunner_fanout_itemAndIndex(t *testing.T) {
	d := &capturingDispatcher{}
	store := state.NewMemStore()
	r := NewWithDispatcher(store, d)

	wf := makeWorkflow(config.PipelineStep{
		ID:    "process",
		Agent: "agents/processor.agent.yaml",
		ForEach: &config.ForEachSpec{
			From: "params.items",
		},
	})

	input := map[string]interface{}{
		"items": []interface{}{"first", "second"},
	}

	ctx := context.Background()
	if err := r.Execute(ctx, "test-workflow", "run-fanout-idx", wf, input, nil); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	indexes := make(map[int]bool)
	for i, call := range d.calls {
		if call.input["item"] == nil {
			t.Errorf("call %d: expected 'item' in input", i)
		}
		idx, ok := call.input["item_index"].(int)
		if !ok {
			t.Errorf("call %d: item_index not an int: %T %v", i, call.input["item_index"], call.input["item_index"])
			continue
		}
		indexes[idx] = true
	}
	for _, want := range []int{0, 1} {
		if !indexes[want] {
			t.Errorf("expected item_index %d to appear in dispatches", want)
		}
	}
}

// TestRunner_fanout_emptyArray verifies an empty input array produces empty results without error.
func TestRunner_fanout_emptyArray(t *testing.T) {
	d := &capturingDispatcher{}
	store := state.NewMemStore()
	r := NewWithDispatcher(store, d)

	wf := makeWorkflow(config.PipelineStep{
		ID:    "process",
		Agent: "agents/processor.agent.yaml",
		ForEach: &config.ForEachSpec{
			From: "params.items",
		},
	})

	input := map[string]interface{}{
		"items": []interface{}{},
	}

	ctx := context.Background()
	if err := r.Execute(ctx, "test-workflow", "run-fanout-empty", wf, input, nil); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if len(d.calls) != 0 {
		t.Errorf("expected 0 dispatches for empty array, got %d", len(d.calls))
	}

	step, _ := store.GetStep(ctx, "run-fanout-empty", "process")
	results := step.Output["results"].([]interface{})
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

// TestRunner_fanout_metricsAggregated verifies token metrics are summed across all fanout invocations.
func TestRunner_fanout_metricsAggregated(t *testing.T) {
	d := &capturingDispatcher{} // returns TokensIn:10, TokensOut:5 per call
	store := state.NewMemStore()
	r := NewWithDispatcher(store, d)

	wf := makeWorkflow(config.PipelineStep{
		ID:    "process",
		Agent: "agents/processor.agent.yaml",
		ForEach: &config.ForEachSpec{
			From: "params.items",
		},
	})

	input := map[string]interface{}{
		"items": []interface{}{"a", "b", "c"},
	}

	ctx := context.Background()
	if err := r.Execute(ctx, "test-workflow", "run-fanout-metrics", wf, input, nil); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	step, _ := store.GetStep(ctx, "run-fanout-metrics", "process")
	if step.Metrics.TokensIn != 30 {
		t.Errorf("expected tokens_in=30 (3x10), got %d", step.Metrics.TokensIn)
	}
	if step.Metrics.TokensOut != 15 {
		t.Errorf("expected tokens_out=15 (3x5), got %d", step.Metrics.TokensOut)
	}
}

// TestRunner_fanout_hyphenatedStepID verifies that a for_each.from expression referencing a
// step ID containing hyphens (e.g. "search-hn.repos") resolves without a SyntaxError.
func TestRunner_fanout_hyphenatedStepID(t *testing.T) {
	d := &mockDispatcher{
		outputs: map[string]map[string]interface{}{
			"search-hn": {
				"repos": []interface{}{"repo-a", "repo-b"},
			},
		},
	}
	store := state.NewMemStore()
	r := NewWithDispatcher(store, d)

	wf := makeWorkflow(
		config.PipelineStep{
			ID:    "search-hn",
			Agent: "agents/search-hn.agent.yaml",
		},
		config.PipelineStep{
			ID:    "process",
			Agent: "agents/process.agent.yaml",
			ForEach: &config.ForEachSpec{
				From: "search-hn.repos",
			},
			DependsOn: []string{"search-hn"},
		},
	)

	ctx := context.Background()
	if err := r.Execute(ctx, "test-workflow", "run-hyphen-fanout", wf, map[string]interface{}{}, nil); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	step, err := store.GetStep(ctx, "run-hyphen-fanout", "process")
	if err != nil {
		t.Fatalf("GetStep failed: %v", err)
	}
	if step.Status != types.StepStatusComplete {
		t.Errorf("expected complete, got %s", step.Status)
	}
	results, ok := step.Output["results"].([]interface{})
	if !ok {
		t.Fatalf("expected results array, got %T: %v", step.Output["results"], step.Output)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}
}

// TestRunner_webhookBody_hyphenatedStepRef verifies that a webhook body value referencing a
// hyphenated step ID (e.g. "repo-github.title") resolves correctly.
func TestRunner_webhookBody_hyphenatedStepRef(t *testing.T) {
	var received map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&received)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d := &mockDispatcher{
		outputs: map[string]map[string]interface{}{
			"repo-github": {"title": "my-repo"},
		},
	}
	store := state.NewMemStore()
	r := NewWithDispatcher(store, d)

	wf := makeWorkflow(
		config.PipelineStep{
			ID:    "repo-github",
			Agent: "agents/repo-github.agent.yaml",
		},
		config.PipelineStep{
			ID: "save",
			Webhook: &config.WebhookSpec{
				URL:    srv.URL,
				Method: "POST",
				Body: map[string]interface{}{
					"message": "repo-github.title",
				},
			},
			DependsOn: []string{"repo-github"},
		},
	)

	ctx := context.Background()
	if err := r.Execute(ctx, "test-workflow", "run-hyphen-webhook", wf, map[string]interface{}{}, nil); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if received["message"] != "my-repo" {
		t.Errorf("expected message=my-repo, got %v", received["message"])
	}
}

// TestRunner_condition_hyphenatedStepRef verifies that a step condition referencing a
// hyphenated step ID does not produce a SyntaxError and evaluates correctly.
func TestRunner_condition_hyphenatedStepRef(t *testing.T) {
	fired := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fired = true
		io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	d := &mockDispatcher{
		outputs: map[string]map[string]interface{}{
			"search-hn": {"success": true},
		},
	}
	store := state.NewMemStore()
	r := NewWithDispatcher(store, d)

	wf := makeWorkflow(
		config.PipelineStep{
			ID:    "search-hn",
			Agent: "agents/search-hn.agent.yaml",
		},
		config.PipelineStep{
			ID:        "notify",
			Condition: "search-hn.success",
			Webhook: &config.WebhookSpec{
				URL:    srv.URL,
				Method: "POST",
			},
			DependsOn: []string{"search-hn"},
		},
	)

	ctx := context.Background()
	if err := r.Execute(ctx, "test-workflow", "run-hyphen-cond", wf, map[string]interface{}{}, nil); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !fired {
		t.Error("expected webhook to fire when condition search-hn.success is true")
	}
}

// TestRunner_fanout_defaultFailFast verifies that with max_failures=0 (default),
// a single item failure fails the step. Metrics from all items are still collected.
func TestRunner_fanout_defaultFailFast(t *testing.T) {
	store := state.NewMemStore()
	r := NewWithDispatcher(store, &failingDispatcher{
		failStepIDs:   map[string]bool{"process.1": true},
		failErr:       fmt.Errorf("max_turns_exceeded"),
		successOutput: map[string]interface{}{"name": "ok"},
	})

	wf := makeWorkflow(config.PipelineStep{
		ID:    "process",
		Agent: "agents/foo.agent.yaml",
		ForEach: &config.ForEachSpec{
			From: "params.items",
		},
	})

	input := map[string]interface{}{
		"items": []interface{}{"a", "b", "c"},
	}

	ctx := context.Background()
	err := r.Execute(ctx, "test-workflow", "run-fanout-fail", wf, input, nil)
	if err == nil {
		t.Fatal("expected Execute to fail when max_failures=0 and an item fails")
	}

	step, _ := store.GetStep(ctx, "run-fanout-fail", "process")
	if step.Status != types.StepStatusFailed {
		t.Errorf("expected step failed, got %s", step.Status)
	}
	// Metrics should include contributions from all items (failed + successful).
	if step.Metrics.TokensIn == 0 {
		t.Error("expected non-zero TokensIn from metrics aggregation")
	}
}

// TestRunner_fanout_maxFailures_tolerateOne verifies that with max_failures=1,
// one item failure is tolerated. The results array contains an error marker.
func TestRunner_fanout_maxFailures_tolerateOne(t *testing.T) {
	store := state.NewMemStore()
	r := NewWithDispatcher(store, &failingDispatcher{
		failStepIDs:   map[string]bool{"process.1": true},
		failErr:       fmt.Errorf("max_turns_exceeded"),
		successOutput: map[string]interface{}{"name": "ok"},
	})

	wf := makeWorkflow(config.PipelineStep{
		ID:    "process",
		Agent: "agents/foo.agent.yaml",
		ForEach: &config.ForEachSpec{
			From:        "params.items",
			MaxFailures: 1,
		},
	})

	input := map[string]interface{}{
		"items": []interface{}{"a", "b", "c"},
	}

	ctx := context.Background()
	err := r.Execute(ctx, "test-workflow", "run-fanout-tol1", wf, input, nil)
	if err != nil {
		t.Fatalf("expected Execute to succeed with max_failures=1 and 1 failure, got: %v", err)
	}

	step, _ := store.GetStep(ctx, "run-fanout-tol1", "process")
	if step.Status != types.StepStatusComplete {
		t.Errorf("expected complete, got %s", step.Status)
	}

	results, ok := step.Output["results"].([]interface{})
	if !ok {
		t.Fatalf("expected results array, got %T", step.Output["results"])
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// Index 0 and 2 should be successful outputs.
	r0, _ := results[0].(map[string]interface{})
	if r0["name"] != "ok" {
		t.Errorf("expected results[0] to be success, got %v", results[0])
	}

	// Index 1 should be an error marker.
	r1, _ := results[1].(map[string]interface{})
	if r1["ktsu_error"] == nil {
		t.Errorf("expected results[1] to have ktsu_error, got %v", results[1])
	}
	if r1["item_index"] != 1 {
		t.Errorf("expected item_index=1, got %v", r1["item_index"])
	}
}

// TestRunner_fanout_maxFailures_exceedThreshold verifies that exceeding
// max_failures causes the step to fail.
func TestRunner_fanout_maxFailures_exceedThreshold(t *testing.T) {
	store := state.NewMemStore()
	r := NewWithDispatcher(store, &failingDispatcher{
		failStepIDs:   map[string]bool{"process.0": true, "process.2": true},
		failErr:       fmt.Errorf("max_turns_exceeded"),
		successOutput: map[string]interface{}{"name": "ok"},
	})

	wf := makeWorkflow(config.PipelineStep{
		ID:    "process",
		Agent: "agents/foo.agent.yaml",
		ForEach: &config.ForEachSpec{
			From:        "params.items",
			MaxFailures: 1,
		},
	})

	input := map[string]interface{}{
		"items": []interface{}{"a", "b", "c"},
	}

	ctx := context.Background()
	err := r.Execute(ctx, "test-workflow", "run-fanout-exceed", wf, input, nil)
	if err == nil {
		t.Fatal("expected Execute to fail when failures exceed max_failures")
	}

	if !strings.Contains(err.Error(), "2 items failed") {
		t.Errorf("expected error to mention failure count, got: %s", err.Error())
	}
}

// TestRunner_fanout_maxFailures_unlimited verifies max_failures=-1
// tolerates all failures.
func TestRunner_fanout_maxFailures_unlimited(t *testing.T) {
	store := state.NewMemStore()
	r := NewWithDispatcher(store, &failingDispatcher{
		failStepIDs:   map[string]bool{"process.0": true, "process.1": true, "process.2": true},
		failErr:       fmt.Errorf("max_turns_exceeded"),
		successOutput: map[string]interface{}{"name": "ok"},
	})

	wf := makeWorkflow(config.PipelineStep{
		ID:    "process",
		Agent: "agents/foo.agent.yaml",
		ForEach: &config.ForEachSpec{
			From:        "params.items",
			MaxFailures: -1,
		},
	})

	input := map[string]interface{}{
		"items": []interface{}{"a", "b", "c"},
	}

	ctx := context.Background()
	err := r.Execute(ctx, "test-workflow", "run-fanout-unlimited", wf, input, nil)
	if err != nil {
		t.Fatalf("expected Execute to succeed with max_failures=-1, got: %v", err)
	}

	step, _ := store.GetStep(ctx, "run-fanout-unlimited", "process")
	if step.Status != types.StepStatusComplete {
		t.Errorf("expected complete, got %s", step.Status)
	}

	results, ok := step.Output["results"].([]interface{})
	if !ok {
		t.Fatalf("expected results array, got %T", step.Output["results"])
	}
	// All 3 items should be error markers.
	for i, r := range results {
		rm, _ := r.(map[string]interface{})
		if rm["ktsu_error"] == nil {
			t.Errorf("expected results[%d] to be error marker, got %v", i, r)
		}
	}
}

// TestRunner_metrics_preserved_on_skip verifies that LLM cost/token metrics are stored
// on the step record even when the step is skipped via ktsu_skip_reason.
func TestRunner_metrics_preserved_on_skip(t *testing.T) {
	store := state.NewMemStore()
	// failingDispatcher returns explicit non-zero metrics on success paths.
	fd := &failingDispatcher{
		failStepIDs: map[string]bool{},
		successOutput: map[string]interface{}{
			"label":            "irrelevant",
			"ktsu_skip_reason": "not_applicable",
		},
	}
	r := NewWithDispatcher(store, fd)

	wf := makeWorkflow(config.PipelineStep{
		ID:    "classify",
		Agent: "agents/classifier.agent.yaml",
	})

	ctx := context.Background()
	// Execute succeeds at the workflow level (the step is skipped, not failed).
	_ = r.Execute(ctx, "test-workflow", "run-skip-metrics", wf, map[string]interface{}{}, nil)

	step, err := store.GetStep(ctx, "run-skip-metrics", "classify")
	if err != nil {
		t.Fatalf("step not found: %v", err)
	}
	if step.Status != types.StepStatusSkipped {
		t.Errorf("expected step skipped, got %s", step.Status)
	}
	if step.Metrics.CostUSD == 0 {
		t.Error("expected non-zero CostUSD on skipped step — metrics must be preserved")
	}
	if step.Metrics.TokensIn == 0 {
		t.Error("expected non-zero TokensIn on skipped step")
	}
}

// TestRunner_metrics_preserved_on_airlock_fail verifies that LLM cost/token metrics
// are stored on the step record even when airlock schema validation fails.
func TestRunner_metrics_preserved_on_airlock_fail(t *testing.T) {
	store := state.NewMemStore()

	// Dispatcher returns output that violates a required schema field.
	fd := &failingDispatcher{
		failStepIDs: map[string]bool{},
		successOutput: map[string]interface{}{
			// "required_field" is absent — airlock validation will reject this.
			"other_field": "value",
		},
	}
	r := NewWithDispatcher(store, fd)

	wf := makeWorkflow(config.PipelineStep{
		ID:    "validate_step",
		Agent: "agents/validator.agent.yaml",
		Output: &config.OutputSpec{
			Schema: map[string]interface{}{
				"type":     "object",
				"required": []interface{}{"required_field"},
				"properties": map[string]interface{}{
					"required_field": map[string]interface{}{"type": "string"},
				},
			},
		},
	})

	ctx := context.Background()
	err := r.Execute(ctx, "test-workflow", "run-airlock-metrics", wf, map[string]interface{}{}, nil)
	if err == nil {
		t.Fatal("expected Execute to fail due to airlock validation error")
	}

	step, getErr := store.GetStep(ctx, "run-airlock-metrics", "validate_step")
	if getErr != nil {
		t.Fatalf("step not found: %v", getErr)
	}
	if step.Status != types.StepStatusFailed {
		t.Errorf("expected step failed, got %s", step.Status)
	}
	if step.Metrics.CostUSD == 0 {
		t.Error("expected non-zero CostUSD on airlock-failed step — metrics must be preserved")
	}
}

// reflectingDispatcher always returns StepMetrics with Reflected=true.
type reflectingDispatcher struct{}

func (d *reflectingDispatcher) Dispatch(_ context.Context, _, _ string, _ *config.PipelineStep, _ map[string]interface{}) (map[string]interface{}, types.StepMetrics, error) {
	return map[string]interface{}{"result": "done"}, types.StepMetrics{
		LLMCalls:     2,
		Reflected:    true,
		ReflectCalls: 1,
	}, nil
}

func TestRunner_agentStep_setsReflected(t *testing.T) {
	store := state.NewMemStore()
	r := NewWithDispatcher(store, &reflectingDispatcher{})

	wf := makeWorkflow(config.PipelineStep{
		ID:    "step-a",
		Agent: "agents/foo.agent.yaml",
	})

	ctx := context.Background()
	if err := r.Execute(ctx, "test-wf", "run-reflect", wf, nil, nil); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	step, err := store.GetStep(ctx, "run-reflect", "step-a")
	if err != nil {
		t.Fatalf("GetStep: %v", err)
	}
	if step.Reflected == nil {
		t.Fatal("want Reflected non-nil for agent step, got nil")
	}
	if !*step.Reflected {
		t.Errorf("want Reflected=true, got false")
	}
}

func TestRunner_fanoutAgentStep_setsReflected(t *testing.T) {
	store := state.NewMemStore()
	r := NewWithDispatcher(store, &reflectingDispatcher{})

	step := config.PipelineStep{
		ID:    "step-fanout",
		Agent: "agents/foo.agent.yaml",
		ForEach: &config.ForEachSpec{
			From: "params.items",
		},
	}
	wf := makeWorkflow(step)

	ctx := context.Background()
	input := map[string]interface{}{
		"items": []interface{}{"a", "b"},
	}
	if err := r.Execute(ctx, "test-wf", "run-fanout", wf, input, nil); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	got, err := store.GetStep(ctx, "run-fanout", "step-fanout")
	if err != nil {
		t.Fatalf("GetStep: %v", err)
	}
	if got.Reflected == nil {
		t.Fatal("want Reflected non-nil for fanout agent step, got nil")
	}
	if !*got.Reflected {
		t.Errorf("want Reflected=true for fanout with reflecting sub-dispatches, got false")
	}
	// ReflectCalls should be the sum across all sub-dispatches (2 items × 1 call each).
	if got.Metrics.ReflectCalls != 2 {
		t.Errorf("want ReflectCalls=2 for fanout with 2 items, got %d", got.Metrics.ReflectCalls)
	}
}

func TestRunner_nonAgentStep_nilReflected(t *testing.T) {
	// Use a webhook step — avoids needing to know valid transform ops.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}))
	defer srv.Close()

	store := state.NewMemStore()
	runner := New(store)

	wf := makeWorkflow(config.PipelineStep{
		ID:      "hook",
		Webhook: &config.WebhookSpec{URL: srv.URL},
	})

	ctx := context.Background()
	if err := runner.Execute(ctx, "test-wf", "run-webhook", wf, nil, nil); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	step, err := store.GetStep(ctx, "run-webhook", "hook")
	if err != nil {
		t.Fatalf("GetStep: %v", err)
	}
	if step.Reflected != nil {
		t.Errorf("want Reflected=nil for webhook step, got %v", step.Reflected)
	}
}

func TestRunner_workflowStep_executesTransformInline(t *testing.T) {
	dir := t.TempDir()
	// Sub-workflow uses output.map to surface params.message as the result.
	subWFYAML := `kind: workflow
name: inner
version: "1.0.0"
pipeline: []
output:
  map:
    result: "{{ params.message }}"
`
	subPath := filepath.Join(dir, "inner.workflow.yaml")
	if err := os.WriteFile(subPath, []byte(subWFYAML), 0644); err != nil {
		t.Fatalf("write sub-workflow: %v", err)
	}

	parentWF := makeWorkflow(config.PipelineStep{
		ID:       "call",
		Workflow: subPath,
		Params: map[string]interface{}{
			"message": "{{ params.greeting }}",
		},
	})
	parentWF.Name = "parent"

	store := state.NewMemStore()
	r := New(store)
	ctx := context.Background()
	err := r.Execute(ctx, "parent", "run1", parentWF, map[string]interface{}{"greeting": "hello"}, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	steps, _ := store.ListSteps(ctx, "run1")
	var callStep *types.Step
	for i := range steps {
		if steps[i].ID == "call" {
			callStep = steps[i]
		}
	}
	if callStep == nil {
		t.Fatal("expected step 'call' in store")
	}
	if callStep.Status != types.StepStatusComplete {
		t.Errorf("expected complete, got %s: %s", callStep.Status, callStep.Error)
	}
	// The sub-workflow's output.map surfaces params.message as result.
	if callStep.Output["result"] != "hello" {
		t.Errorf("expected result=hello, got %v", callStep.Output["result"])
	}
}

func TestRunner_workflowStep_webhookSuppressedByDefault(t *testing.T) {
	callCount := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	dir := t.TempDir()
	subWFYAML := fmt.Sprintf(`kind: workflow
name: sub
version: "1.0.0"
webhooks: suppress
pipeline:
  - id: notify
    webhook:
      url: %q
      method: POST
`, ts.URL)
	subPath := filepath.Join(dir, "sub.workflow.yaml")
	if err := os.WriteFile(subPath, []byte(subWFYAML), 0644); err != nil {
		t.Fatalf("write sub-workflow: %v", err)
	}

	parentWF := makeWorkflow(config.PipelineStep{
		ID:       "call",
		Workflow: subPath,
	})
	parentWF.Name = "parent"

	store := state.NewMemStore()
	r := New(store)
	if err := r.Execute(context.Background(), "parent", "run1", parentWF, nil, nil); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if callCount != 0 {
		t.Errorf("webhook should be suppressed by default, called %d times", callCount)
	}
}

func TestRunner_workflowStep_webhookExecutesWhenBothOptIn(t *testing.T) {
	callCount := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	dir := t.TempDir()
	subWFYAML := fmt.Sprintf(`kind: workflow
name: sub
version: "1.0.0"
webhooks: execute
pipeline:
  - id: notify
    webhook:
      url: %q
      method: POST
`, ts.URL)
	subPath := filepath.Join(dir, "sub.workflow.yaml")
	if err := os.WriteFile(subPath, []byte(subWFYAML), 0644); err != nil {
		t.Fatalf("write sub-workflow: %v", err)
	}

	parentWF := makeWorkflow(config.PipelineStep{
		ID:               "call",
		Workflow:         subPath,
		WorkflowWebhooks: "execute",
	})
	parentWF.Name = "parent"

	store := state.NewMemStore()
	r := New(store)
	if err := r.Execute(context.Background(), "parent", "run1", parentWF, nil, nil); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if callCount != 1 {
		t.Errorf("webhook should fire once when both opt in, called %d times", callCount)
	}
}

func TestRunner_workflowStep_paramPassthrough(t *testing.T) {
	dir := t.TempDir()
	subWFYAML := `kind: workflow
name: sub
version: "1.0.0"
params:
  schema:
    type: object
    required: [greeting]
    properties:
      greeting: {type: string}
pipeline: []
output:
  map:
    result: "{{ params.greeting }}"
`
	subPath := filepath.Join(dir, "sub.workflow.yaml")
	if err := os.WriteFile(subPath, []byte(subWFYAML), 0644); err != nil {
		t.Fatalf("write sub-workflow: %v", err)
	}

	parentWF := makeWorkflow(config.PipelineStep{
		ID:       "call",
		Workflow: subPath,
		Params:   map[string]interface{}{"greeting": "`hello`"},
	})
	parentWF.Name = "parent"

	store := state.NewMemStore()
	r := New(store)
	if err := r.Execute(context.Background(), "parent", "run1", parentWF, map[string]interface{}{"text": "world"}, nil); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	steps, _ := store.ListSteps(context.Background(), "run1")
	var callStep *types.Step
	for i := range steps {
		if steps[i].ID == "call" {
			callStep = steps[i]
		}
	}
	if callStep == nil || callStep.Status != types.StepStatusComplete {
		t.Fatalf("call step not complete: %+v", callStep)
	}
}

func TestRunner_workflowStep_missingRequiredParam_returnsError(t *testing.T) {
	dir := t.TempDir()
	subWFYAML := `kind: workflow
name: sub
version: "1.0.0"
params:
  schema:
    type: object
    required: [webhook_url]
    properties:
      webhook_url: {type: string}
pipeline: []
`
	subPath := filepath.Join(dir, "sub.workflow.yaml")
	if err := os.WriteFile(subPath, []byte(subWFYAML), 0644); err != nil {
		t.Fatalf("write sub-workflow: %v", err)
	}

	parentWF := makeWorkflow(config.PipelineStep{
		ID:       "call",
		Workflow: subPath,
		// webhook_url intentionally omitted
	})
	parentWF.Name = "parent"

	store := state.NewMemStore()
	r := New(store)
	err := r.Execute(context.Background(), "parent", "run1", parentWF, nil, nil)
	if err == nil {
		t.Fatal("expected error for missing required param")
	}
	if !strings.Contains(err.Error(), "missing required param") {
		t.Errorf("expected 'missing required param' in error, got: %v", err)
	}
}

// --- Expression system: {{ expr }} templates, rich context, output.map, condition for all step types ---

// TestRunner_workflowStep_condition_skips verifies that a workflow step with a condition
// that evaluates to false is skipped.
func TestRunner_workflowStep_condition_skips(t *testing.T) {
	dir := t.TempDir()
	subWFYAML := `kind: workflow
name: inner
version: "1.0.0"
pipeline: []
`
	subPath := filepath.Join(dir, "inner.workflow.yaml")
	if err := os.WriteFile(subPath, []byte(subWFYAML), 0644); err != nil {
		t.Fatalf("write sub-workflow: %v", err)
	}

	// params.text is null → condition evaluates to false → workflow step skipped
	parentWF := makeWorkflow(config.PipelineStep{
		ID:        "call",
		Workflow:  subPath,
		Condition: "params.text != null",
	})
	parentWF.Name = "parent"

	store := state.NewMemStore()
	r := New(store)
	// No "text" in input → params.text is null → condition false → step should be skipped
	if err := r.Execute(context.Background(), "parent", "run1", parentWF, map[string]interface{}{}, nil); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	callStep, err := store.GetStep(context.Background(), "run1", "call")
	if err != nil {
		t.Fatalf("GetStep: %v", err)
	}
	if callStep.Status != types.StepStatusSkipped {
		t.Errorf("expected skipped, got %s (error: %s)", callStep.Status, callStep.Error)
	}
}

// TestRunner_workflowStep_condition_executes verifies that a workflow step with a condition
// that evaluates to true is executed normally.
func TestRunner_workflowStep_condition_executes(t *testing.T) {
	dir := t.TempDir()
	subWFYAML := `kind: workflow
name: inner
version: "1.0.0"
pipeline: []
`
	subPath := filepath.Join(dir, "inner.workflow.yaml")
	if err := os.WriteFile(subPath, []byte(subWFYAML), 0644); err != nil {
		t.Fatalf("write sub-workflow: %v", err)
	}

	// params.text is "hello" → condition evaluates to true → workflow step executes
	parentWF := makeWorkflow(config.PipelineStep{
		ID:        "call",
		Workflow:  subPath,
		Condition: "params.text != null",
	})
	parentWF.Name = "parent"

	store := state.NewMemStore()
	r := New(store)
	if err := r.Execute(context.Background(), "parent", "run1", parentWF, map[string]interface{}{"text": "hello"}, nil); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	callStep, err := store.GetStep(context.Background(), "run1", "call")
	if err != nil {
		t.Fatalf("GetStep: %v", err)
	}
	if callStep.Status != types.StepStatusComplete {
		t.Errorf("expected complete, got %s (error: %s)", callStep.Status, callStep.Error)
	}
}

// TestRunner_subWorkflow_outputMap verifies that a sub-workflow with an empty pipeline
// and an output.map section produces the mapped output using params.* context.
func TestRunner_subWorkflow_outputMap(t *testing.T) {
	dir := t.TempDir()
	subWFYAML := `kind: workflow
name: normalize
version: "1.0.0"
pipeline: []
output:
  schema:
    type: object
    properties:
      chat_id: {type: integer}
      text: {type: string}
  map:
    chat_id: "{{ params.message.chat_id }}"
    text: "{{ params.message.text }}"
`
	subPath := filepath.Join(dir, "normalize.workflow.yaml")
	if err := os.WriteFile(subPath, []byte(subWFYAML), 0644); err != nil {
		t.Fatalf("write sub-workflow: %v", err)
	}

	parentWF := makeWorkflow(config.PipelineStep{
		ID:       "norm",
		Workflow: subPath,
		Params: map[string]interface{}{
			"message": "{{ params.msg }}",
		},
	})
	parentWF.Name = "parent"

	store := state.NewMemStore()
	r := New(store)
	input := map[string]interface{}{
		"msg": map[string]interface{}{
			"chat_id": float64(123),
			"text":    "hello",
		},
	}
	if err := r.Execute(context.Background(), "parent", "run1", parentWF, input, nil); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	step, err := store.GetStep(context.Background(), "run1", "norm")
	if err != nil {
		t.Fatalf("GetStep: %v", err)
	}
	if step.Status != types.StepStatusComplete {
		t.Errorf("expected complete, got %s (error: %s)", step.Status, step.Error)
	}
	if step.Output["chat_id"] != float64(123) {
		t.Errorf("expected chat_id=123, got %v", step.Output["chat_id"])
	}
	if step.Output["text"] != "hello" {
		t.Errorf("expected text=hello, got %v", step.Output["text"])
	}
}

// TestRunner_workflowStep_templateParam_passesTypedValue verifies that {{ step.X.field }}
// in workflow step params resolves to a typed value usable in the sub-workflow.
func TestRunner_workflowStep_templateParam_passesTypedValue(t *testing.T) {
	dir := t.TempDir()
	subWFYAML := `kind: workflow
name: echo
version: "1.0.0"
pipeline: []
output:
  map:
    value: "{{ params.val }}"
`
	subPath := filepath.Join(dir, "echo.workflow.yaml")
	if err := os.WriteFile(subPath, []byte(subWFYAML), 0644); err != nil {
		t.Fatalf("write sub-workflow: %v", err)
	}

	parentWF := makeWorkflow(config.PipelineStep{
		ID:      "call",
		Workflow: subPath,
		Params:  map[string]interface{}{"val": "{{ params.number }}"},
	})
	parentWF.Name = "parent"

	store := state.NewMemStore()
	r := New(store)
	if err := r.Execute(context.Background(), "parent", "run1", parentWF, map[string]interface{}{"number": float64(42)}, nil); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	step, err := store.GetStep(context.Background(), "run1", "call")
	if err != nil {
		t.Fatalf("GetStep: %v", err)
	}
	if step.Status != types.StepStatusComplete {
		t.Errorf("expected complete, got %s (error: %s)", step.Status, step.Error)
	}
	if step.Output["value"] != float64(42) {
		t.Errorf("expected value=42, got %v", step.Output["value"])
	}
}

// TestRunner_envVar_rootWorkflowOnly verifies that env.* is available in root workflow params
// but NOT available when passed into a sub-workflow context.
func TestRunner_envVar_rootWorkflowOnly(t *testing.T) {
	t.Setenv("TEST_TOKEN", "secret123")

	dir := t.TempDir()
	subWFYAML := `kind: workflow
name: inner
version: "1.0.0"
pipeline: []
output:
  map:
    token: "{{ params.tok }}"
`
	subPath := filepath.Join(dir, "inner.workflow.yaml")
	if err := os.WriteFile(subPath, []byte(subWFYAML), 0644); err != nil {
		t.Fatalf("write sub-workflow: %v", err)
	}

	rootWF := makeWorkflow(config.PipelineStep{
		ID:       "call",
		Workflow: subPath,
		Params:   map[string]interface{}{"tok": "{{ env.TEST_TOKEN }}"},
	})
	rootWF.Name = "root"
	rootWF.Env = []config.EnvVarDecl{{Name: "TEST_TOKEN"}}

	store := state.NewMemStore()
	r := New(store)
	if err := r.Execute(context.Background(), "root", "run1", rootWF, nil, nil); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	step, err := store.GetStep(context.Background(), "run1", "call")
	if err != nil {
		t.Fatalf("GetStep: %v", err)
	}
	if step.Status != types.StepStatusComplete {
		t.Errorf("expected complete, got %s (error: %s)", step.Status, step.Error)
	}
	if step.Output["token"] != "secret123" {
		t.Errorf("expected token=secret123, got %v", step.Output["token"])
	}
}

// TestRunner_webhookStep_templateURL verifies that {{ params.X }} inline in a webhook URL
// is substituted correctly.
func TestRunner_webhookStep_templateURL(t *testing.T) {
	var capturedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		capturedPath = req.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	dir := t.TempDir()
	subWFYAML := fmt.Sprintf(`kind: workflow
name: sender
version: "1.0.0"
webhooks: execute
params:
  schema:
    type: object
    required: [token]
    properties:
      token: {type: string}
pipeline:
  - id: send
    webhook:
      url: "%s/bot{{ params.token }}/sendMessage"
      method: POST
`, srv.URL)
	subPath := filepath.Join(dir, "sender.workflow.yaml")
	if err := os.WriteFile(subPath, []byte(subWFYAML), 0644); err != nil {
		t.Fatalf("write sub-workflow: %v", err)
	}

	rootWF := makeWorkflow(config.PipelineStep{
		ID:               "call",
		Workflow:         subPath,
		Params:           map[string]interface{}{"token": "`mytoken`"},
		WorkflowWebhooks: "execute",
	})
	rootWF.Name = "root"

	store := state.NewMemStore()
	r := New(store)
	if err := r.Execute(context.Background(), "root", "run1", rootWF, nil, nil); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if capturedPath != "/botmytoken/sendMessage" {
		t.Errorf("expected path /botmytoken/sendMessage, got %s", capturedPath)
	}
}

// TestRunner_webhookBody_templateExpr verifies that {{ params.X }} in webhook body fields
// resolves correctly using the rich params context.
func TestRunner_webhookBody_templateExpr(t *testing.T) {
	var body map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		_ = json.NewDecoder(req.Body).Decode(&body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	dir := t.TempDir()
	subWFYAML := fmt.Sprintf(`kind: workflow
name: sender
version: "1.0.0"
webhooks: execute
params:
  schema:
    type: object
    required: [chat_id, text]
    properties:
      chat_id: {type: integer}
      text: {type: string}
pipeline:
  - id: send
    webhook:
      url: %q
      method: POST
      body:
        chat_id: "{{ params.chat_id }}"
        text: "{{ params.text }}"
`, srv.URL)
	subPath := filepath.Join(dir, "sender.workflow.yaml")
	if err := os.WriteFile(subPath, []byte(subWFYAML), 0644); err != nil {
		t.Fatalf("write sub-workflow: %v", err)
	}

	parentWF := makeWorkflow(config.PipelineStep{
		ID:      "call",
		Workflow: subPath,
		Params: map[string]interface{}{
			"chat_id": "{{ params.chat_id }}",
			"text":    "{{ params.text }}",
		},
		WorkflowWebhooks: "execute",
	})
	parentWF.Name = "parent"

	store := state.NewMemStore()
	r := New(store)
	input := map[string]interface{}{"chat_id": float64(456), "text": "world"}
	if err := r.Execute(context.Background(), "parent", "run1", parentWF, input, nil); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if body["chat_id"] != float64(456) {
		t.Errorf("expected chat_id=456, got %v", body["chat_id"])
	}
	if body["text"] != "world" {
		t.Errorf("expected text=world, got %v", body["text"])
	}
}
