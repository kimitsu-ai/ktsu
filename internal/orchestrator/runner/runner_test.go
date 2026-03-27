package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kimitsu-ai/ktsu/internal/config"
	"github.com/kimitsu-ai/ktsu/internal/orchestrator/state"
	"github.com/kimitsu-ai/ktsu/pkg/types"
)

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

// TestRunner_workflowInput verifies the workflow input is available to pipeline steps.
func TestRunner_workflowInput(t *testing.T) {
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
	err := r.Execute(ctx, "test-workflow", "run-input", wf, map[string]interface{}{"message": "hello"})
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
	err := r.Execute(ctx, "test-workflow", "run-merge", wf, nil)
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

// TestRunner_transformStep_filter verifies filter op on an array from workflow input.
func TestRunner_transformStep_filter(t *testing.T) {
	store := state.NewMemStore()
	r := New(store)

	wf := makeWorkflow(
		config.PipelineStep{
			ID:        "filter_step",
			Transform: &config.TransformSpec{
				Inputs: []config.TransformInput{{From: "input"}},
				Ops: []map[string]interface{}{
					{"map": map[string]interface{}{"expr": "items"}},
					{"flatten": nil},
					{"filter": map[string]interface{}{"expr": "status == 'active'"}},
				},
			},
		},
	)

	input := map[string]interface{}{
		"items": []interface{}{
			map[string]interface{}{"status": "active", "name": "item1"},
			map[string]interface{}{"status": "inactive", "name": "item2"},
		},
	}

	ctx := context.Background()
	err := r.Execute(ctx, "test-workflow", "run-filter", wf, input)
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

// TestRunner_transformStep_sort verifies sort op on array from workflow input.
func TestRunner_transformStep_sort(t *testing.T) {
	store := state.NewMemStore()
	r := New(store)

	wf := makeWorkflow(
		config.PipelineStep{
			ID:        "sort_step",
			Transform: &config.TransformSpec{
				Inputs: []config.TransformInput{{From: "input"}},
				Ops: []map[string]interface{}{
					{"map": map[string]interface{}{"expr": "items"}},
					{"flatten": nil},
					{"sort": map[string]interface{}{"field": "age", "order": "asc"}},
				},
			},
		},
	)

	input := map[string]interface{}{
		"items": []interface{}{
			map[string]interface{}{"name": "Charlie", "age": float64(30)},
			map[string]interface{}{"name": "Alice", "age": float64(25)},
			map[string]interface{}{"name": "Bob", "age": float64(28)},
		},
	}

	ctx := context.Background()
	err := r.Execute(ctx, "test-workflow", "run-sort", wf, input)
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
				"message": "input.msg",
			},
		},
	})

	ctx := context.Background()
	err := r.Execute(ctx, "test-workflow", "run-webhook", wf, map[string]interface{}{"msg": "hello world"})
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
	err := r.Execute(ctx, "test-workflow", "run-webhook-fail", wf, nil)
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
	err := r.Execute(ctx, "test-workflow", "run-cond", wf, nil)
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
	err := r.Execute(ctx, "test-workflow", "run-agent", wf, nil)
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
	err := r.Execute(ctx, "test-workflow", "run-injection", wf, nil)
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
	err := r.Execute(ctx, "test-workflow", "run-needs-human", wf, nil)
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
	err := r.Execute(ctx, "test-workflow", "run-skip", wf, nil)
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
	err := r.Execute(ctx, "test-workflow", "run-conf", wf, nil)
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
	err := r.Execute(ctx, "test-workflow", "run-dag", wf, nil)
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
	err := r.Execute(ctx, "test-workflow", "run-timeout", wf, nil)
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
	if err := r.Execute(ctx, "test-workflow", "run-env-body", wf, nil); err != nil {
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
			From: "input.items",
		},
	})

	input := map[string]interface{}{
		"items": []interface{}{"a", "b", "c"},
	}

	ctx := context.Background()
	if err := r.Execute(ctx, "test-workflow", "run-fanout", wf, input); err != nil {
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
			From:     "input.items",
			MaxItems: 2,
		},
	})

	input := map[string]interface{}{
		"items": []interface{}{"a", "b", "c", "d", "e"},
	}

	ctx := context.Background()
	if err := r.Execute(ctx, "test-workflow", "run-fanout-max", wf, input); err != nil {
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
			From: "input.items",
		},
	})

	input := map[string]interface{}{
		"items": []interface{}{"first", "second"},
	}

	ctx := context.Background()
	if err := r.Execute(ctx, "test-workflow", "run-fanout-idx", wf, input); err != nil {
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
			From: "input.items",
		},
	})

	input := map[string]interface{}{
		"items": []interface{}{},
	}

	ctx := context.Background()
	if err := r.Execute(ctx, "test-workflow", "run-fanout-empty", wf, input); err != nil {
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
			From: "input.items",
		},
	})

	input := map[string]interface{}{
		"items": []interface{}{"a", "b", "c"},
	}

	ctx := context.Background()
	if err := r.Execute(ctx, "test-workflow", "run-fanout-metrics", wf, input); err != nil {
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
	if err := r.Execute(ctx, "test-workflow", "run-hyphen-fanout", wf, map[string]interface{}{}); err != nil {
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
	if err := r.Execute(ctx, "test-workflow", "run-hyphen-webhook", wf, map[string]interface{}{}); err != nil {
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
	if err := r.Execute(ctx, "test-workflow", "run-hyphen-cond", wf, map[string]interface{}{}); err != nil {
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
			From: "input.items",
		},
	})

	input := map[string]interface{}{
		"items": []interface{}{"a", "b", "c"},
	}

	ctx := context.Background()
	err := r.Execute(ctx, "test-workflow", "run-fanout-fail", wf, input)
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
			From:        "input.items",
			MaxFailures: 1,
		},
	})

	input := map[string]interface{}{
		"items": []interface{}{"a", "b", "c"},
	}

	ctx := context.Background()
	err := r.Execute(ctx, "test-workflow", "run-fanout-tol1", wf, input)
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
	if r1["item_index"] != float64(1) {
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
			From:        "input.items",
			MaxFailures: 1,
		},
	})

	input := map[string]interface{}{
		"items": []interface{}{"a", "b", "c"},
	}

	ctx := context.Background()
	err := r.Execute(ctx, "test-workflow", "run-fanout-exceed", wf, input)
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
			From:        "input.items",
			MaxFailures: -1,
		},
	})

	input := map[string]interface{}{
		"items": []interface{}{"a", "b", "c"},
	}

	ctx := context.Background()
	err := r.Execute(ctx, "test-workflow", "run-fanout-unlimited", wf, input)
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
