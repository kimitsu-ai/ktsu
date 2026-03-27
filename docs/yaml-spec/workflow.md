# workflow.yaml

**What it does:** Defines a pipeline — input schema, ordered steps, and model cost policy.

**Filename convention:** `workflows/*.workflow.yaml`

## Annotated Example

```yaml
kind: workflow
name: support-triage            # unique name — used in POST /invoke/{name}
version: "1.2.0"                # semver string
description: "..."              # optional

input:
  schema:                       # JSON Schema — validated on invoke; 422 on failure
    type: object
    required: [message, user_id]
    properties:
      message:    { type: string }
      user_id:    { type: string }
      channel_id: { type: string }

pipeline:

  # ── Agent step ────────────────────────────────────────────────────────────
  - id: parse                   # unique step ID — used in depends_on and body references
    agent: ktsu/secure-parser@1.0.0   # built-in: ktsu/<name>@<ver>
                                      # local:    ./agents/foo.agent.yaml  (optional @<ver>)
    params:                     # parameters for built-in agents only
      source_field: message
      extract:
        intent: { type: string, enum: [billing, technical, legal, other] }
    model:                      # optional — overrides agent file's model group
      group: economy            # model group name from gateway.yaml
      max_tokens: 512
    # no depends_on — receives workflow input

  - id: triage
    agent: ./agents/triage.agent.yaml
    depends_on: [parse]         # step IDs this step waits for
    confidence_threshold: 0.7  # min ktsu_confidence; agent output schema must declare ktsu_confidence

  # ── Agent step with fanout ────────────────────────────────────────────────
  - id: enrich
    agent: ./agents/enricher.agent.yaml
    depends_on: [triage]
    for_each:
      from: triage.tickets       # JMESPath against accumulated step outputs — must resolve to an array
      max_items: 20              # optional — truncates array before fanout
      concurrency: 4             # optional — max parallel invocations (default: unbounded)
    # Each invocation receives the full step envelope plus two extra keys:
    #   item        — the current array element
    #   item_index  — zero-based integer index
    # Step output is always: { "results": [...] } — one entry per item in original order
    # Reference downstream as: enrich.results  (JMESPath)
    # Metrics (tokens, cost, tool calls) are summed across all invocations.

  # ── Transform step ────────────────────────────────────────────────────────
  - id: merge-reviews
    transform:
      inputs:
        - from: legal-review     # upstream step IDs — depends_on derived automatically
        - from: risk-review
      ops:                       # applied sequentially; each op receives output of previous
        - merge: [legal-review, risk-review]          # concat arrays or deep-merge objects
        - deduplicate: { field: ticket_id }           # first occurrence wins
        - filter:      { expr: "confidence > `0.7`" } # JMESPath; removes falsy items
        - sort:        { field: confidence, order: desc }
        - map:         { expr: "{ id: ticket_id, score: confidence }" }
        - flatten:     {}                             # one level of array nesting
    output:
      schema:                    # Air-Lock validated
        type: array
        items: { type: object }

  # ── Webhook step ──────────────────────────────────────────────────────────
  - id: notify
    webhook:
      url: "env:SLACK_WEBHOOK_URL"   # env:VAR_NAME resolved at runtime
      method: POST                    # default: POST
      body:
        text:    "triage.summary"     # JMESPath against merged step outputs
        channel: "input.channel_id"   # workflow input fields are under "input"
        source:  "`kimitsu`"          # literal string — JMESPath backtick syntax
      timeout_s: 10                   # default: 30
    condition: "triage.category == 'billing'"  # JMESPath; if falsy → skipped (not failed)
    depends_on: [triage]

model_policy:
  cost_budget_usd: 0.50          # circuit breaker — rejects LLM calls when exhausted
  group_map:
    frontier: standard           # selectively remaps group names for this workflow
  force_group: local             # overrides ALL group declarations (useful for local dev)
```

## Top-Level Fields

| Field | Type | Required | Description |
|---|---|---|---|
| `kind` | string | yes | Must be `workflow` |
| `name` | string | yes | Unique workflow name; used in `POST /invoke/{name}` |
| `version` | string | yes | Semver string |
| `description` | string | no | Human-readable description |
| `input.schema` | JSON Schema | yes | Validated against the invoke request body; 422 on failure |
| `pipeline` | array | yes | Ordered list of pipeline steps |
| `model_policy.cost_budget_usd` | number | no | LLM cost circuit breaker for the run |
| `model_policy.group_map` | object | no | Remaps model group names; keys and values are group names |
| `model_policy.force_group` | string | no | Overrides ALL model group declarations to this group |
| `model_policy.timeout_s` | number | no | Per-run wall-clock timeout in seconds |

## Agent Step Fields

| Field | Type | Required | Description |
|---|---|---|---|
| `id` | string | yes | Unique step ID; referenced in `depends_on` and output expressions |
| `agent` | string | yes | Built-in: `ktsu/<name>@<ver>`; local: `./agents/foo.agent.yaml` (optional `@<ver>`) |
| `depends_on` | string[] | no | Step IDs to wait for; omit to receive workflow input |
| `confidence_threshold` | number | no | Min `ktsu_confidence` required; agent output schema must declare `ktsu_confidence` |
| `params` | object | no | Parameters for built-in agents (e.g. `ktsu/secure-parser`) |
| `model.group` | string | no | Overrides agent file's model group |
| `model.max_tokens` | number | no | Token limit override |
| `for_each.from` | string | no | JMESPath resolving to an array; runs agent once per item |
| `for_each.max_items` | number | no | Truncates array before fanout |
| `for_each.concurrency` | number | no | Max parallel invocations; default: unbounded |
| `for_each.max_failures` | number | no | Max item failures to tolerate; `0` = fail on first error (default), `-1` = unlimited, `N` = tolerate up to N |
| `consolidation` | string | no | Fan-in strategy: `array` \| `merge` \| `first` — accepted but not yet enforced at runtime |

## Fanout Input and Output Contract

When `for_each` is set, the runner resolves `from` as a JMESPath expression against all accumulated step outputs. It then invokes the agent once per element of the resulting array.

**Each agent invocation receives the standard input envelope** (all upstream step outputs keyed by step ID) **plus two additional keys:**

| Key | Value |
|---|---|
| `item` | The current array element (any JSON type) |
| `item_index` | Zero-based integer index of this element |

Example — if `triage.tickets` resolves to `[{...}, {...}, {...}]`, the agent for index 1 receives:

```json
{
  "input":   { "message": "...", "user_id": "..." },
  "parse":   { "intent": "billing" },
  "triage":  { "tickets": [...] },
  "item":    { "id": "T-42", "subject": "..." },
  "item_index": 1
}
```

**The step's output is always:**

```json
{ "results": [ <output from item 0>, <output from item 1>, ... ] }
```

Results are in the original array order regardless of completion order. Reference them downstream using JMESPath:

```yaml
# In a webhook body:
body:
  tickets: "enrich.results"          # the full results array
  first:   "enrich.results[0]"       # first result

# As the source for a transform:
transform:
  inputs:
    - from: enrich                   # stepOutputs["enrich"] = { "results": [...] }
  ops:
    - flatten: {}                    # enrich.results is already an array; flatten if items are arrays
```

Metrics (tokens in/out, cost, LLM calls, tool calls) are summed across all fan-out invocations and reported as a single step metric.

Sub-invocation step IDs are `<step-id>.<index>` (e.g. `enrich.0`, `enrich.1`) and appear in logs.

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

## Transform Step Fields

| Field | Type | Required | Description |
|---|---|---|---|
| `id` | string | yes | Unique step ID |
| `transform.inputs` | array | yes | Upstream step references; `depends_on` is derived automatically |
| `transform.inputs[].from` | string | yes | Step ID of upstream step |
| `transform.ops` | array | yes | Ordered op list; each op receives the output of the previous |
| `output.schema` | JSON Schema | yes | Air-Lock validated |

### Transform Ops

| Op | Arguments | Description |
|---|---|---|
| `merge` | `[step-id, ...]` | Concatenates arrays or deep-merges objects (later wins on key conflict) |
| `sort` | `{ field, order }` | Sorts array by JMESPath field; `order`: `asc` \| `desc` (default: `asc`) |
| `filter` | `{ expr }` | Removes items where JMESPath `expr` is falsy |
| `map` | `{ expr }` | Projects each item to a new shape via JMESPath |
| `flatten` | `{}` | Flattens one level of array nesting |
| `deduplicate` | `{ field }` | Removes duplicates by key field; first occurrence wins |

## Webhook Step Fields

| Field | Type | Required | Description |
|---|---|---|---|
| `id` | string | yes | Unique step ID |
| `webhook.url` | string | yes | Destination URL; `env:VAR_NAME` resolved at runtime |
| `webhook.method` | string | no | HTTP method; default: `POST` |
| `webhook.body` | object | no | Key-value map; values are JMESPath against merged step outputs |
| `webhook.timeout_s` | number | no | Request timeout in seconds; default: 30 |
| `condition` | string | no | JMESPath; if falsy step is `skipped` (not failed) |
| `depends_on` | string[] | no | Step IDs to wait for |

## Notes

- Every agent step receives the full accumulated step outputs as a JSON envelope in its first user message. Keys are step IDs; the workflow input is always under the key `"input"`. Reference as `input.<field>` in JMESPath expressions (webhooks, transforms) and `input.<field>` / `<step-id>.<field>` in agent system prompts.
- Fanout step output is always `{ "results": [...] }` in original array order; see the Fanout Input and Output Contract section for details.
- Transform `depends_on` is derived from `inputs[].from` — do not declare it separately.
- Webhook non-2xx or network error → step fails immediately; no retry.
- Webhook `condition` false → step is `skipped`; downstream steps still run.
