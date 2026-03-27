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
      from: triage.tickets       # JMESPath — must resolve to an array
      max_items: 20              # optional — truncates array before fanout
      concurrency: 4             # optional — max parallel invocations (default: unbounded)
    # output is always: { "results": [...] } — one entry per item in original order

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
| `consolidation` | string | no | Fan-in strategy: `array` \| `merge` \| `first` — accepted but not yet enforced at runtime |

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

- Workflow input is available to all steps as `input`; reference as `input.<field>` in JMESPath expressions and `inputs.input.<field>` inside agent system prompts.
- Fanout step output is always `{ "results": [...] }` in original array order.
- Transform `depends_on` is derived from `inputs[].from` — do not declare it separately.
- Webhook non-2xx or network error → step fails immediately; no retry.
- Webhook `condition` false → step is `skipped`; downstream steps still run.
