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
	"testing"

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

func writeInletYAML(t *testing.T, dir string, name string, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write inlet yaml: %v", err)
	}
	return path
}

func writeOutletYAML(t *testing.T, dir string, name string, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write outlet yaml: %v", err)
	}
	return path
}

// TestRunner_inletStep verifies a single inlet step with a webhook trigger maps fields.
func TestRunner_inletStep(t *testing.T) {
	dir := t.TempDir()
	inletYAML := `
kind: inlet
name: test-inlet
version: "1.0.0"
trigger:
  type: webhook
  path: /webhook
mapping:
  output:
    name: body.name
`
	inletPath := writeInletYAML(t, dir, "test.inlet.yaml", inletYAML)

	store := state.NewMemStore()
	r := New(store, dir)

	wf := makeWorkflow(config.PipelineStep{
		ID:    "step1",
		Inlet: inletPath,
	})

	trigger := TriggerContext{
		Type: "webhook",
		Body: map[string]interface{}{"name": "Alice"},
	}

	ctx := context.Background()
	err := r.Execute(ctx, "test-workflow", "run-1", wf, trigger)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	step, err := store.GetStep(ctx, "run-1", "step1")
	if err != nil {
		t.Fatalf("GetStep failed: %v", err)
	}
	if step.Status != types.StepStatusComplete {
		t.Errorf("expected step status complete, got %s", step.Status)
	}
	if step.Output["name"] != "Alice" {
		t.Errorf("expected output name=Alice, got %v", step.Output["name"])
	}
}

// TestRunner_transformStep_merge verifies two inlet step outputs are merged via transform.
func TestRunner_transformStep_merge(t *testing.T) {
	dir := t.TempDir()

	inlet1YAML := `
kind: inlet
name: inlet1
version: "1.0.0"
trigger:
  type: webhook
mapping:
  output:
    a: body.a
`
	inlet2YAML := `
kind: inlet
name: inlet2
version: "1.0.0"
trigger:
  type: webhook
mapping:
  output:
    b: body.b
`
	step1Path := writeInletYAML(t, dir, "step1.inlet.yaml", inlet1YAML)
	step2Path := writeInletYAML(t, dir, "step2.inlet.yaml", inlet2YAML)

	wf := makeWorkflow(
		config.PipelineStep{
			ID:    "step1",
			Inlet: step1Path,
		},
		config.PipelineStep{
			ID:    "step2",
			Inlet: step2Path,
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

	trigger := TriggerContext{
		Type: "webhook",
		Body: map[string]interface{}{"a": 1, "b": 2},
	}

	ctx := context.Background()
	store := state.NewMemStore()
	r := New(store, dir)
	err := r.Execute(ctx, "test-workflow", "run-merge", wf, trigger)
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
	// Verify no step ID keys leaked into the result (no double-wrapping)
	if _, hasStep1 := resultMap["step1"]; hasStep1 {
		t.Errorf("merged result should not contain 'step1' key: %v", resultMap)
	}
	if _, hasStep2 := resultMap["step2"]; hasStep2 {
		t.Errorf("merged result should not contain 'step2' key: %v", resultMap)
	}
}

// TestRunner_transformStep_filter verifies filter op on an array.
func TestRunner_transformStep_filter(t *testing.T) {
	dir := t.TempDir()

	inletYAML := `
kind: inlet
name: items-inlet
version: "1.0.0"
trigger:
  type: webhook
mapping:
  output:
    items: body.items
`
	inletPath := writeInletYAML(t, dir, "items.inlet.yaml", inletYAML)

	// The inlet produces {"items": [{status:active,...}, {status:inactive,...}]}
	// The transform: single input source → data = {"items":[...]}
	// map op extracts "items" from each item in wrapped array → [[a,b]]
	// flatten → [a, b]
	// filter [?status == 'active'] → [a]
	wf := makeWorkflow(
		config.PipelineStep{
			ID:    "source",
			Inlet: inletPath,
		},
		config.PipelineStep{
			ID:        "filter_step",
			DependsOn: []string{"source"},
			Transform: &config.TransformSpec{
				Inputs: []config.TransformInput{
					{From: "source"},
				},
				Ops: []map[string]interface{}{
					{"map": map[string]interface{}{"expr": "items"}},
					{"flatten": nil},
					{"filter": map[string]interface{}{"expr": "status == 'active'"}},
				},
			},
		},
	)

	trigger := TriggerContext{
		Type: "webhook",
		Body: map[string]interface{}{
			"items": []interface{}{
				map[string]interface{}{"status": "active", "name": "item1"},
				map[string]interface{}{"status": "inactive", "name": "item2"},
			},
		},
	}

	ctx := context.Background()
	store := state.NewMemStore()
	r := New(store, dir)
	err := r.Execute(ctx, "test-workflow", "run-filter", wf, trigger)
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

// TestRunner_transformStep_sort verifies sort op on an array.
func TestRunner_transformStep_sort(t *testing.T) {
	dir := t.TempDir()

	inletYAML := `
kind: inlet
name: ages-inlet
version: "1.0.0"
trigger:
  type: webhook
mapping:
  output:
    items: body.items
`
	inletPath := writeInletYAML(t, dir, "ages.inlet.yaml", inletYAML)

	wf := makeWorkflow(
		config.PipelineStep{
			ID:    "source",
			Inlet: inletPath,
		},
		config.PipelineStep{
			ID:        "sort_step",
			DependsOn: []string{"source"},
			Transform: &config.TransformSpec{
				Inputs: []config.TransformInput{
					{From: "source"},
				},
				Ops: []map[string]interface{}{
					{"map": map[string]interface{}{"expr": "items"}},
					{"flatten": nil},
					{"sort": map[string]interface{}{"field": "age", "order": "asc"}},
				},
			},
		},
	)

	trigger := TriggerContext{
		Type: "webhook",
		Body: map[string]interface{}{
			"items": []interface{}{
				map[string]interface{}{"name": "Charlie", "age": float64(30)},
				map[string]interface{}{"name": "Alice", "age": float64(25)},
				map[string]interface{}{"name": "Bob", "age": float64(28)},
			},
		},
	}

	ctx := context.Background()
	store := state.NewMemStore()
	r := New(store, dir)
	err := r.Execute(ctx, "test-workflow", "run-sort", wf, trigger)
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
	result, ok := step.Output["result"]
	if !ok {
		t.Fatalf("missing result: %v", step.Output)
	}
	resultSlice, ok := result.([]interface{})
	if !ok {
		t.Fatalf("result is not a slice: %T", result)
	}
	if len(resultSlice) != 3 {
		t.Fatalf("expected 3 items, got %d", len(resultSlice))
	}
	first := resultSlice[0].(map[string]interface{})
	if first["name"] != "Alice" {
		t.Errorf("expected Alice first, got %v", first["name"])
	}
	last := resultSlice[2].(map[string]interface{})
	if last["name"] != "Charlie" {
		t.Errorf("expected Charlie last, got %v", last["name"])
	}
}

// TestRunner_outletStep_noop verifies noop action returns {sent: false, action: noop}.
func TestRunner_outletStep_noop(t *testing.T) {
	dir := t.TempDir()

	inletYAML := `
kind: inlet
name: source
version: "1.0.0"
trigger:
  type: webhook
mapping:
  output:
    msg: body.msg
`
	inletPath := writeInletYAML(t, dir, "source.inlet.yaml", inletYAML)

	outletYAML := `
kind: outlet
name: noop-outlet
version: "1.0.0"
mapping:
  action:
    type: noop
    body: {}
`
	outletPath := writeOutletYAML(t, dir, "noop.outlet.yaml", outletYAML)

	store := state.NewMemStore()
	r := New(store, dir)

	wf := makeWorkflow(
		config.PipelineStep{
			ID:    "source",
			Inlet: inletPath,
		},
		config.PipelineStep{
			ID:        "notify",
			Outlet:    outletPath,
			DependsOn: []string{"source"},
		},
	)

	trigger := TriggerContext{
		Type: "webhook",
		Body: map[string]interface{}{"msg": "hello"},
	}

	ctx := context.Background()
	err := r.Execute(ctx, "test-workflow", "run-noop", wf, trigger)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	step, err := store.GetStep(ctx, "run-noop", "notify")
	if err != nil {
		t.Fatalf("GetStep failed: %v", err)
	}
	if step.Status != types.StepStatusComplete {
		t.Errorf("expected complete, got %s", step.Status)
	}
	if step.Output["sent"] != false {
		t.Errorf("expected sent=false, got %v", step.Output["sent"])
	}
	if step.Output["action"] != "noop" {
		t.Errorf("expected action=noop, got %v", step.Output["action"])
	}
}

// TestRunner_outletStep_condition_false verifies step is skipped when condition is false.
func TestRunner_outletStep_condition_false(t *testing.T) {
	dir := t.TempDir()

	inletYAML := `
kind: inlet
name: source
version: "1.0.0"
trigger:
  type: webhook
mapping:
  output:
    msg: body.msg
`
	inletPath := writeInletYAML(t, dir, "source2.inlet.yaml", inletYAML)

	outletYAML := `
kind: outlet
name: cond-outlet
version: "1.0.0"
mapping:
  action:
    type: noop
    body: {}
`
	outletPath := writeOutletYAML(t, dir, "cond.outlet.yaml", outletYAML)

	store := state.NewMemStore()
	r := New(store, dir)

	wf := makeWorkflow(
		config.PipelineStep{
			ID:    "source",
			Inlet: inletPath,
		},
		config.PipelineStep{
			ID:        "conditional_outlet",
			Outlet:    outletPath,
			DependsOn: []string{"source"},
			Condition: "nonexistent_step.something", // evaluates to nil → skip
		},
	)

	trigger := TriggerContext{
		Type: "webhook",
		Body: map[string]interface{}{"msg": "hello"},
	}

	ctx := context.Background()
	err := r.Execute(ctx, "test-workflow", "run-cond", wf, trigger)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	step, err := store.GetStep(ctx, "run-cond", "conditional_outlet")
	if err != nil {
		t.Fatalf("GetStep failed: %v", err)
	}
	if step.Status != types.StepStatusSkipped {
		t.Errorf("expected skipped, got %s", step.Status)
	}
}

// TestRunner_outletStep_httpPost verifies POST action sends request and returns sent=true.
func TestRunner_outletStep_httpPost(t *testing.T) {
	dir := t.TempDir()

	var receivedBody []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		var err error
		receivedBody, err = io.ReadAll(req.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	inletYAML := `
kind: inlet
name: source
version: "1.0.0"
trigger:
  type: webhook
mapping:
  output:
    msg: body.msg
`
	inletPath := writeInletYAML(t, dir, "source3.inlet.yaml", inletYAML)

	outletYAML := fmt.Sprintf(`
kind: outlet
name: http-outlet
version: "1.0.0"
mapping:
  action:
    type: http_post
    url: %s
    body:
      message: source.msg
`, server.URL)
	outletPath := writeOutletYAML(t, dir, "http.outlet.yaml", outletYAML)

	store := state.NewMemStore()
	r := New(store, dir)

	wf := makeWorkflow(
		config.PipelineStep{
			ID:    "source",
			Inlet: inletPath,
		},
		config.PipelineStep{
			ID:        "http_step",
			Outlet:    outletPath,
			DependsOn: []string{"source"},
		},
	)

	trigger := TriggerContext{
		Type: "webhook",
		Body: map[string]interface{}{"msg": "hello world"},
	}

	ctx := context.Background()
	err := r.Execute(ctx, "test-workflow", "run-http", wf, trigger)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	step, err := store.GetStep(ctx, "run-http", "http_step")
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

// TestRunner_agentStep_stub verifies an agent step returns stub output.
func TestRunner_agentStep_stub(t *testing.T) {
	store := state.NewMemStore()
	r := New(store, "/tmp")

	wf := makeWorkflow(
		config.PipelineStep{
			ID:    "agent_step",
			Agent: "agents/foo.agent.yaml@1.0.0",
		},
	)

	trigger := TriggerContext{Type: "webhook"}

	ctx := context.Background()
	err := r.Execute(ctx, "test-workflow", "run-agent", wf, trigger)
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

// TestRunner_reservedField_injectionAttempt verifies run fails on injection attempt.
func TestRunner_reservedField_injectionAttempt(t *testing.T) {
	dir := t.TempDir()

	inletYAML := `
kind: inlet
name: injection-inlet
version: "1.0.0"
trigger:
  type: webhook
mapping:
  output:
    ktsu_injection_attempt: body.ktsu_injection_attempt
    data: body.data
`
	inletPath := writeInletYAML(t, dir, "injection.inlet.yaml", inletYAML)

	store := state.NewMemStore()
	r := New(store, dir)

	wf := makeWorkflow(
		config.PipelineStep{
			ID:    "step1",
			Inlet: inletPath,
		},
	)

	trigger := TriggerContext{
		Type: "webhook",
		Body: map[string]interface{}{
			"ktsu_injection_attempt": true,
			"data":                   "something",
		},
	}

	ctx := context.Background()
	err := r.Execute(ctx, "test-workflow", "run-injection", wf, trigger)
	if err == nil {
		t.Fatal("expected Execute to fail on injection attempt, but it succeeded")
	}

	run, getErr := store.GetRun(ctx, "run-injection")
	if getErr != nil {
		t.Fatalf("GetRun failed: %v", getErr)
	}
	if run.Status != types.RunStatusFailed {
		t.Errorf("expected run status failed, got %s", run.Status)
	}
}

// TestRunner_reservedField_needsHuman verifies run fails when ktsu_needs_human is true.
func TestRunner_reservedField_needsHuman(t *testing.T) {
	dir := t.TempDir()

	inletYAML := `
kind: inlet
name: needs-human-inlet
version: "1.0.0"
trigger:
  type: webhook
mapping:
  output:
    ktsu_needs_human: body.ktsu_needs_human
    data: body.data
`
	inletPath := writeInletYAML(t, dir, "needs_human.inlet.yaml", inletYAML)

	store := state.NewMemStore()
	r := New(store, dir)

	wf := makeWorkflow(
		config.PipelineStep{
			ID:    "step1",
			Inlet: inletPath,
		},
	)

	trigger := TriggerContext{
		Type: "webhook",
		Body: map[string]interface{}{
			"ktsu_needs_human": true,
			"data":             "something",
		},
	}

	ctx := context.Background()
	err := r.Execute(ctx, "test-workflow", "run-needs-human", wf, trigger)
	if err == nil {
		t.Fatal("expected Execute to fail on needs_human, but it succeeded")
	}

	run, getErr := store.GetRun(ctx, "run-needs-human")
	if getErr != nil {
		t.Fatalf("GetRun failed: %v", getErr)
	}
	if run.Status != types.RunStatusFailed {
		t.Errorf("expected run status failed, got %s", run.Status)
	}
}

// TestRunner_reservedField_untrustedContent verifies step fails when ktsu_untrusted_content is true.
func TestRunner_reservedField_untrustedContent(t *testing.T) {
	dir := t.TempDir()

	inletYAML := `
kind: inlet
name: untrusted-inlet
version: "1.0.0"
trigger:
  type: webhook
mapping:
  output:
    ktsu_untrusted_content: body.ktsu_untrusted_content
    data: body.data
`
	inletPath := writeInletYAML(t, dir, "untrusted.inlet.yaml", inletYAML)

	store := state.NewMemStore()
	r := New(store, dir)

	wf := makeWorkflow(
		config.PipelineStep{
			ID:    "step1",
			Inlet: inletPath,
		},
	)

	trigger := TriggerContext{
		Type: "webhook",
		Body: map[string]interface{}{
			"ktsu_untrusted_content": true,
			"data":                   "something",
		},
	}

	ctx := context.Background()
	err := r.Execute(ctx, "test-workflow", "run-untrusted", wf, trigger)
	if err == nil {
		t.Fatal("expected Execute to fail on untrusted content, but it succeeded")
	}

	run, getErr := store.GetRun(ctx, "run-untrusted")
	if getErr != nil {
		t.Fatalf("GetRun failed: %v", getErr)
	}
	if run.Status != types.RunStatusFailed {
		t.Errorf("expected run status failed, got %s", run.Status)
	}
}

// TestRunner_reservedField_skipReason verifies step is marked skipped.
func TestRunner_reservedField_skipReason(t *testing.T) {
	dir := t.TempDir()

	inletYAML := `
kind: inlet
name: skip-inlet
version: "1.0.0"
trigger:
  type: webhook
mapping:
  output:
    ktsu_skip_reason: body.reason
    data: body.data
`
	inletPath := writeInletYAML(t, dir, "skip.inlet.yaml", inletYAML)

	store := state.NewMemStore()
	r := New(store, dir)

	wf := makeWorkflow(
		config.PipelineStep{
			ID:    "step1",
			Inlet: inletPath,
		},
	)

	trigger := TriggerContext{
		Type: "webhook",
		Body: map[string]interface{}{
			"reason": "nothing to process",
			"data":   "value",
		},
	}

	ctx := context.Background()
	err := r.Execute(ctx, "test-workflow", "run-skip", wf, trigger)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	step, err := store.GetStep(ctx, "run-skip", "step1")
	if err != nil {
		t.Fatalf("GetStep failed: %v", err)
	}
	if step.Status != types.StepStatusSkipped {
		t.Errorf("expected skipped, got %s", step.Status)
	}
}

// TestRunner_reservedField_confidence_below_threshold verifies step fails when confidence is low.
func TestRunner_reservedField_confidence_below_threshold(t *testing.T) {
	dir := t.TempDir()

	inletYAML := `
kind: inlet
name: conf-inlet
version: "1.0.0"
trigger:
  type: webhook
mapping:
  output:
    ktsu_confidence: body.confidence
    data: body.data
`
	inletPath := writeInletYAML(t, dir, "conf.inlet.yaml", inletYAML)

	store := state.NewMemStore()
	r := New(store, dir)

	wf := makeWorkflow(
		config.PipelineStep{
			ID:                  "step1",
			Inlet:               inletPath,
			ConfidenceThreshold: 0.8,
		},
	)

	trigger := TriggerContext{
		Type: "webhook",
		Body: map[string]interface{}{
			"confidence": 0.5,
			"data":       "some output",
		},
	}

	ctx := context.Background()
	err := r.Execute(ctx, "test-workflow", "run-conf", wf, trigger)
	if err == nil {
		t.Fatal("expected Execute to fail due to low confidence, but it succeeded")
	}

	step, getErr := store.GetStep(ctx, "run-conf", "step1")
	if getErr != nil {
		t.Fatalf("GetStep failed: %v", getErr)
	}
	if step.Status != types.StepStatusFailed {
		t.Errorf("expected failed, got %s", step.Status)
	}
}

// TestRunner_multiStep_dag verifies multi-step pipeline executes in DAG order.
func TestRunner_multiStep_dag(t *testing.T) {
	dir := t.TempDir()

	inletYAML := `
kind: inlet
name: inlet-a
version: "1.0.0"
trigger:
  type: webhook
mapping:
  output:
    name: body.name
`
	inletPath := writeInletYAML(t, dir, "a.inlet.yaml", inletYAML)

	outletYAML := `
kind: outlet
name: outlet-c
version: "1.0.0"
mapping:
  action:
    type: noop
    body: {}
`
	outletPath := writeOutletYAML(t, dir, "c.outlet.yaml", outletYAML)

	store := state.NewMemStore()
	r := New(store, dir)

	wf := makeWorkflow(
		config.PipelineStep{
			ID:    "step_a",
			Inlet: inletPath,
		},
		config.PipelineStep{
			ID:        "step_b",
			DependsOn: []string{"step_a"},
			Transform: &config.TransformSpec{
				Inputs: []config.TransformInput{
					{From: "step_a"},
				},
				Ops: []map[string]interface{}{},
			},
		},
		config.PipelineStep{
			ID:        "step_c",
			Outlet:    outletPath,
			DependsOn: []string{"step_b"},
		},
	)

	trigger := TriggerContext{
		Type: "webhook",
		Body: map[string]interface{}{"name": "Bob"},
	}

	ctx := context.Background()
	err := r.Execute(ctx, "test-workflow", "run-dag", wf, trigger)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	for _, stepID := range []string{"step_a", "step_b", "step_c"} {
		step, err := store.GetStep(ctx, "run-dag", stepID)
		if err != nil {
			t.Fatalf("GetStep(%s) failed: %v", stepID, err)
		}
		if step.Status != types.StepStatusComplete {
			t.Errorf("step %s: expected complete, got %s", stepID, step.Status)
		}
	}

	// Verify step_b output has result from transform
	stepB, _ := store.GetStep(ctx, "run-dag", "step_b")
	if _, hasResult := stepB.Output["result"]; !hasResult {
		t.Errorf("step_b should have result from transform, got: %v", stepB.Output)
	}
}
