package runner

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
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

func (m *mockDispatcher) Dispatch(_ context.Context, _, stepID string, _ map[string]interface{}) (map[string]interface{}, error) {
	if out, ok := m.outputs[stepID]; ok {
		return out, nil
	}
	return map[string]interface{}{"stubbed": true}, nil
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
