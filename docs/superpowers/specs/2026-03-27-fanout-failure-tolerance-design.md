# Fanout Failure Tolerance

## Problem

When a fanout step has one item fail (e.g., `max_turns_exceeded`), the entire step fails and no downstream steps run. This is unnecessarily strict for workflows where partial results are acceptable. Workflow authors currently have no way to express failure tolerance.

Additionally, the current implementation may lose cost metrics from in-flight items when the step fails early.

## YAML Surface

New `max_failures` field on `for_each`:

```yaml
pipeline:
  - id: repo-github
    agent: agents/fetch-github-repo.agent.yaml
    for_each:
      from: search-hn.repos
      max_items: 10
      concurrency: 3
      max_failures: 1    # tolerate up to 1 item failure
    depends_on: [search-hn]
```

### `max_failures` semantics

| Value | Behavior |
|-------|----------|
| `0` (default) | Fail the step if any item fails. Current behavior. |
| `N > 0` | Tolerate up to N item failures. Fail if N+1 items fail. |
| `-1` | Unlimited tolerance. Never fail the step due to item failures. |

## Runtime Behavior

### Always wait for all goroutines

Regardless of `max_failures`, the runner waits for all fanout goroutines to finish before evaluating results. This ensures accurate cost and token metrics are always collected, even when the step ultimately fails.

### Post-collection threshold check

After all goroutines complete:

1. Count the number of failed items.
2. If `max_failures == 0` and any item failed: return the first error (preserves current behavior).
3. If `max_failures > 0` and failure count exceeds `max_failures`: return an error like `"fanout: 2 items failed (max_failures: 1)"`.
4. If `max_failures == -1`: never fail the step due to item failures.

In cases 3 and 4 where items failed but the step succeeds, failed items are represented as error markers in the results array.

### Failed item representation

Failed items produce an error marker object in the results array, preserving index alignment:

```json
{
  "results": [
    {"name": "repo/a", "stars": 100, "description": "A repo"},
    {"ktsu_error": "max_turns_exceeded", "item_index": 1},
    {"name": "repo/c", "stars": 50, "description": "C repo"}
  ]
}
```

- `results[i]` always corresponds to `items[i]` from the input array.
- `ktsu_error` contains the error message from the failed agent invocation.
- `item_index` is the zero-based index of the failed item.
- The `ktsu_` prefix is consistent with the existing reserved field convention.

## Changes

### `internal/config/types.go`

Add `MaxFailures` to `ForEachSpec`:

```go
type ForEachSpec struct {
    From        string `yaml:"from"`
    MaxItems    int    `yaml:"max_items,omitempty"`
    Concurrency int    `yaml:"concurrency,omitempty"`
    MaxFailures int    `yaml:"max_failures,omitempty"`
}
```

### `internal/orchestrator/runner/runner.go`

Modify `executeFanout`:

- Remove early-return on first error.
- After `wg.Wait()`, build the results array: successful items get their output, failed items get `{"ktsu_error": msg, "item_index": idx}`.
- Apply threshold check based on `max_failures` value.
- Metrics aggregation remains unchanged (already collects from all items).

### `docs/yaml-spec/workflow.md`

Add `for_each.max_failures` to the field reference table with the semantics described above.

### Tests

Four test cases in `runner_test.go`:

1. **Default (max_failures=0):** One item fails, step fails with first error. Metrics from all items are included.
2. **Tolerate one (max_failures=1):** One item fails, step succeeds. Results array contains error marker at failed index.
3. **Exceed threshold (max_failures=1, 2 failures):** Step fails with count-based error message.
4. **Unlimited (max_failures=-1):** All items fail, step still succeeds. Results array is all error markers.
