# Fanout Failure Tolerance Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Allow fanout steps to tolerate item failures via a `max_failures` field, producing error markers for failed items instead of failing the entire step.

**Architecture:** Add `MaxFailures` to `ForEachSpec` config. Modify `executeFanout` to always wait for all goroutines, then apply a threshold check. Failed items produce `{"ktsu_error": msg, "item_index": N}` markers in the results array.

**Tech Stack:** Go, YAML config, existing runner/config packages

---

### Task 1: Add `MaxFailures` to `ForEachSpec`

**Files:**
- Modify: `internal/config/types.go:38-42`

- [ ] **Step 1: Add the field**

In `internal/config/types.go`, add `MaxFailures` to `ForEachSpec`:

```go
// ForEachSpec configures fanout iteration for an agent step.
// The agent is invoked once per item in the array resolved from From.
type ForEachSpec struct {
	From        string `yaml:"from"`
	MaxItems    int    `yaml:"max_items,omitempty"`
	Concurrency int    `yaml:"concurrency,omitempty"`
	MaxFailures int    `yaml:"max_failures,omitempty"` // 0=fail-fast (default), -1=unlimited, N=tolerate up to N
}
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/config/...`
Expected: success, no errors

- [ ] **Step 3: Commit**

```bash
git add internal/config/types.go
git commit -m "feat(config): add MaxFailures field to ForEachSpec"
```

---

### Task 2: Add a `failingDispatcher` test helper

**Files:**
- Modify: `internal/orchestrator/runner/runner_test.go`

The existing test dispatchers (`mockDispatcher`, `capturingDispatcher`) always succeed. We need one that can fail specific items by step ID pattern.

- [ ] **Step 1: Write the `failingDispatcher` helper**

Add this after the existing `capturingDispatcher` in `internal/orchestrator/runner/runner_test.go`:

```go
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
```

- [ ] **Step 2: Verify it compiles**

Run: `go build ./internal/orchestrator/runner/...`
Expected: success (the helper is unused for now — Go won't error on unused types, only unused variables)

- [ ] **Step 3: Commit**

```bash
git add internal/orchestrator/runner/runner_test.go
git commit -m "test(runner): add failingDispatcher test helper for fanout error tests"
```

---

### Task 3: Write failing tests for fanout failure tolerance

**Files:**
- Modify: `internal/orchestrator/runner/runner_test.go`

Write four tests that exercise the new behavior. All four should fail until the implementation in Task 4.

- [ ] **Step 1: Write test for default behavior (max_failures=0)**

Add to `internal/orchestrator/runner/runner_test.go`:

```go
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
```

- [ ] **Step 2: Write test for max_failures=1 tolerating one failure**

```go
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
```

- [ ] **Step 3: Write test for exceeding max_failures threshold**

```go
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
```

- [ ] **Step 4: Write test for max_failures=-1 (unlimited)**

```go
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
```

- [ ] **Step 5: Add `"fmt"` and `"strings"` to imports if not already present**

Check the imports block at the top of `runner_test.go`. Add `"fmt"` and `"strings"` if they are not already imported.

- [ ] **Step 6: Run tests to verify they fail**

Run: `go test ./internal/orchestrator/runner/... -run "TestRunner_fanout_(defaultFailFast|maxFailures)" -v`
Expected: all 4 new tests FAIL (the implementation doesn't exist yet). The `defaultFailFast` test may pass since it tests existing behavior — that's fine.

- [ ] **Step 7: Commit**

```bash
git add internal/orchestrator/runner/runner_test.go
git commit -m "test(runner): add failing tests for fanout max_failures feature"
```

---

### Task 4: Implement failure tolerance in `executeFanout`

**Files:**
- Modify: `internal/orchestrator/runner/runner.go:306-325`

- [ ] **Step 1: Replace the post-collection logic in `executeFanout`**

In `internal/orchestrator/runner/runner.go`, replace the result-collection block after `wg.Wait()` (lines 306-325) with:

```go
	wg.Wait()

	outputs := make([]interface{}, len(items))
	var totals types.StepMetrics
	var failCount int
	var firstErr error
	for i, res := range results {
		totals.TokensIn += res.metrics.TokensIn
		totals.TokensOut += res.metrics.TokensOut
		totals.CostUSD += res.metrics.CostUSD
		totals.LLMCalls += res.metrics.LLMCalls
		totals.ToolCalls += res.metrics.ToolCalls
		if res.err != nil {
			failCount++
			if firstErr == nil {
				firstErr = fmt.Errorf("fanout item %d: %w", i, res.err)
			}
			outputs[i] = map[string]interface{}{
				"ktsu_error": res.err.Error(),
				"item_index": i,
			}
		} else {
			outputs[i] = res.output
		}
	}

	maxFailures := spec.MaxFailures
	if maxFailures == 0 && firstErr != nil {
		// Default: fail on first error (preserves existing behavior).
		return nil, totals, firstErr
	}
	if maxFailures > 0 && failCount > maxFailures {
		return nil, totals, fmt.Errorf("fanout: %d items failed (max_failures: %d)", failCount, maxFailures)
	}
	// maxFailures == -1 (unlimited) or failCount within threshold: succeed.

	return map[string]interface{}{"results": outputs}, totals, nil
```

- [ ] **Step 2: Run the new tests**

Run: `go test ./internal/orchestrator/runner/... -run "TestRunner_fanout_(defaultFailFast|maxFailures)" -v`
Expected: all 4 tests PASS

- [ ] **Step 3: Run the full runner test suite**

Run: `go test ./internal/orchestrator/runner/... -v`
Expected: all tests PASS (existing behavior preserved)

- [ ] **Step 4: Commit**

```bash
git add internal/orchestrator/runner/runner.go
git commit -m "feat(runner): implement fanout failure tolerance with max_failures"
```

---

### Task 5: Update documentation

**Files:**
- Modify: `docs/yaml-spec/workflow.md:125`

- [ ] **Step 1: Add `for_each.max_failures` to the field reference table**

In `docs/yaml-spec/workflow.md`, find the line:

```
| `for_each.concurrency` | number | no | Max parallel invocations; default: unbounded |
```

Add after it:

```
| `for_each.max_failures` | number | no | Max item failures to tolerate; `0` = fail on first error (default), `-1` = unlimited, `N` = tolerate up to N |
```

- [ ] **Step 2: Add failure tolerance section after the existing "Fanout Input and Output Contract" section**

Find the end of the "Fanout Input and Output Contract" section and add:

```markdown
## Fanout Failure Tolerance

By default (`max_failures: 0`), a single item failure fails the entire step. Set `max_failures` to tolerate partial failures:

```yaml
for_each:
  from: search-hn.repos
  max_failures: 1    # tolerate up to 1 failure
```

When failures are tolerated, failed items produce an error marker in the results array:

```json
{
  "results": [
    {"name": "repo/a", "stars": 100},
    {"ktsu_error": "max_turns_exceeded", "item_index": 1},
    {"name": "repo/c", "stars": 50}
  ]
}
```

Index alignment is preserved: `results[i]` always corresponds to `items[i]`.

Set `max_failures: -1` for best-effort mode that never fails the step due to item errors.

Metrics (tokens, cost) are always collected from all items, including failed ones.
```

- [ ] **Step 3: Commit**

```bash
git add docs/yaml-spec/workflow.md
git commit -m "docs: add for_each.max_failures to workflow YAML spec"
```

---

### Task 6: Run full test suite

**Files:** none (verification only)

- [ ] **Step 1: Run full project tests**

Run: `go test ./... 2>&1`
Expected: all tests PASS

- [ ] **Step 2: Run go vet**

Run: `go vet ./...`
Expected: no issues
