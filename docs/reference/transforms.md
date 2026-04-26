# Transform Ops

**What it does:** A Transform step applies a sequence of deterministic data operations to the outputs of one or more upstream steps. No LLM call is made.

---

## How Transforms Work

A transform step collects its inputs, then applies each op in `ops:` left-to-right. Each op receives the output of the previous op, so the sequence acts as a pipeline.

```yaml
- id: process
  transform:
    inputs:
      - from: previous_step      # use one step's output as input
    ops:
      - filter:
          expr: "score > `0.5`"
      - sort:
          field: "score"
          order: "desc"
```

When multiple inputs are listed, they are deep-merged into a single object (right-to-left, right wins) before the first op runs.

The transform step emits `{"result": <finalValue>}`. Downstream steps reference it as `step.<id>.result`.

---

## The `ops:` Array

Each element of `ops:` is a map with exactly one key — the op name — whose value is a sub-map of op-specific fields (or an array for `merge`).

```yaml
ops:
  - <op_name>:
      <field>: <value>
```

---

## Op Reference

### `merge`

Deep-merges the outputs of the listed steps into the current data object. Map keys from later steps override earlier ones; arrays are concatenated.

**Sub-fields:** an array of step IDs.

```yaml
ops:
  - merge:
      - step_a
      - step_b
```

| Sub-field | Type | Description |
|---|---|---|
| *(value)* | array of strings | Ordered list of step IDs to merge. |

---

### `filter`

Keeps only array items where the JMESPath expression evaluates to a truthy value. Non-array input is coerced to a single-element array first.

```yaml
ops:
  - filter:
      expr: "confidence > `0.5`"
```

| Sub-field | Type | Description |
|---|---|---|
| `expr` | string | JMESPath expression. Items where it is truthy are kept. |

---

### `sort`

Sorts an array by a field value. Numeric fields are compared numerically; all others are compared as strings. `nil` values sort last.

```yaml
ops:
  - sort:
      field: "score"
      order: "desc"
```

| Sub-field | Type | Description |
|---|---|---|
| `field` | string | Field name (or JMESPath) to sort by. |
| `order` | string | `"asc"` (default) or `"desc"`. |

---

### `map`

Projects each array item through a JMESPath expression, replacing it with the expression's result. Non-array input is coerced to a single-element array first.

```yaml
ops:
  - map:
      expr: "{ title: title, score: confidence }"
```

| Sub-field | Type | Description |
|---|---|---|
| `expr` | string | JMESPath expression evaluated against each item. |

---

### `flatten`

Flattens one level of nesting: each element that is itself an array is spread into the parent array. Non-array elements are kept as-is.

**Sub-fields:** none.

```yaml
ops:
  - flatten: {}
```

Input `[[1, 2], [3], 4]` → output `[1, 2, 3, 4]`.

---

### `deduplicate`

Removes duplicate array items, keeping the first occurrence. Two items are considered duplicates if the given field has the same string representation.

```yaml
ops:
  - deduplicate:
      field: "url"
```

| Sub-field | Type | Description |
|---|---|---|
| `field` | string | Field name (or JMESPath) used as the deduplication key. |

---

## Complete Pipeline Example

A transform step that takes fanout results, filters out failures and low-confidence items, removes duplicates by URL, and sorts by score descending.

```yaml
pipeline:
  - id: score_urls
    agent: "./agents/scorer.agent.yaml"
    params:
      url: "{{ item }}"
    for_each:
      from: "fetch.urls"
      concurrency: 10
      max_failures: -1

  - id: clean_results
    transform:
      inputs:
        - from: score_urls
      ops:
        # Unwrap the fanout results array
        - map:
            expr: "results"

        # Drop failed iterations and low-confidence scores
        - filter:
            expr: "score > `0.6` && !ktsu_error"

        # One result per URL
        - deduplicate:
            field: "url"

        # Highest confidence first
        - sort:
            field: "score"
            order: "desc"
    depends_on: [score_urls]
```

`clean_results` emits `{"result": [...]}` — a deduplicated, sorted array of passing scored objects.

---

*See also [Pipeline Primitives](../concepts/pipeline-primitives.md) for the full list of step types, and [Fanout](../concepts/fanout.md) for running an agent step over an array.*

*Revised April 2026*
