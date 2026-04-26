# Fanout (for_each)

Fanout runs an agent step once for each item in an array — concurrently, up to a configurable limit — and collects all results in a single output object.

---

## YAML Syntax

Add a `for_each` block to any agent step. The step's `params` receive an `item` and `item_index` variable in addition to the normal step context.

```yaml
- id: analyze
  agent: "./agents/analyzer.agent.yaml"
  params:
    url: "{{ item }}"             # current array element
    position: "{{ item_index }}"  # zero-based index of this element
  for_each:
    from: "step.fetch.urls"       # JMESPath resolving to an array
    concurrency: 5                # max parallel dispatches (default: all)
    max_items: 100                # truncate input array (default: no limit)
    max_failures: 2               # tolerate up to N failures (default: 0 = fail-fast)
```

### `for_each` Fields

| Field | Type | Required | Description |
|---|---|---|---|
| `from` | string | yes | JMESPath expression that resolves to an array from accumulated step outputs or `params`. |
| `concurrency` | int | no | Maximum goroutines running at once. Defaults to the full array length (all parallel). |
| `max_items` | int | no | Truncate the array to at most N items before dispatching. |
| `max_failures` | int | no | `0` (default): fail on first error. `-1`: tolerate all failures. `N`: tolerate up to N failures. |

### What `item` and `item_index` resolve to

When the orchestrator dispatches each iteration it injects two extra keys into the agent's input context:

- **`item`** — the raw array element for this iteration (object, string, number, etc.)
- **`item_index`** — the zero-based position of that element in the original array

Reference them in `params` with `{{ item }}` and `{{ item_index }}` like any other template variable.

---

## Output Shape

Every fanout step emits `{"results": [...]}`, with one entry per input element in the original array order regardless of completion order.

```json
{
  "results": [
    { "score": 0.95, "label": "safe" },
    { "score": 0.12, "label": "spam" },
    { "ktsu_error": "agent timeout", "item_index": 2 }
  ]
}
```

A successful iteration contributes the agent's output object directly. A failed iteration contributes:

```json
{ "ktsu_error": "<error message>", "item_index": <n> }
```

---

## Referencing Fanout Output

Downstream steps reference the fanout step's output through `step.<id>.results`:

```yaml
- id: summarize
  agent: "./agents/summarizer.agent.yaml"
  params:
    all_scores: "{{ step.analyze.results }}"      # full array
  depends_on: [analyze]
```

To address a single result by index, use JMESPath bracket notation inside a transform or condition:

```yaml
- id: check_first
  transform:
    inputs:
      - from: analyze
    ops:
      - map:
          expr: "results[0].score"   # first result's score field
```

---

## Complete Example

A workflow that fetches a list of URLs from a previous step, scores each one in parallel, and then merges only the high-confidence results.

```yaml
pipeline:
  # Step 1 — emit the list of URLs to process
  - id: fetch_urls
    agent: "./agents/url-fetcher.agent.yaml"
    params:
      query: "{{ params.topic }}"

  # Step 2 — fan out over each URL concurrently
  - id: score_urls
    agent: "./agents/scorer.agent.yaml"
    params:
      url: "{{ item }}"
      index: "{{ item_index }}"
    for_each:
      from: "fetch_urls.urls"   # fetch_urls must output {"urls": [...]}
      concurrency: 10
      max_failures: -1          # collect all results even if some fail
    depends_on: [fetch_urls]

  # Step 3 — filter down to high-confidence hits
  - id: high_confidence
    transform:
      inputs:
        - from: score_urls
      ops:
        - map:
            expr: "results"                          # unwrap the results array
        - filter:
            expr: "score > `0.8` && !ktsu_error"    # drop failures and low scores
        - sort:
            field: "score"
            order: "desc"
    depends_on: [score_urls]
```

The final step `high_confidence` emits `{"result": [...]}` containing only the passing scored objects, sorted highest-first.

---

*See also [Pipeline Primitives](./pipeline-primitives.md) for the full list of step types, and [Transforms](./transforms.md) for all available transform ops.*

*Revised April 2026*
