# workflow.yaml

**What it does:** Defines a pipeline — input schema, ordered steps, and model cost policy.

**Filename convention:** `workflows/*.workflow.yaml`

## Annotated Example

```yaml
kind: workflow
name: support-triage            # unique name — used in POST /invoke/{name}
version: "1.2.0"                # semver string
description: "..."              # optional
visibility: root                # "root" (default) | "sub-workflow"

params:
  schema:                       # JSON Schema — validated on invoke for root workflows; 422 on failure
    type: object
    required: [message, user_id]
    properties:
      message:    { type: string }
      user_id:    { type: string }
      channel_id: { type: string }

env:
  - name: SLACK_WEBHOOK_URL     # environment variable name
    secret: true                # true = masked as [hidden] in logs and envelope; default: false
    description: "Slack incoming webhook URL"   # optional
  - name: USER_NAMESPACE
    secret: false
  - name: APPROVAL_WEBHOOK_URL
    secret: true
    description: "Webhook URL for approval notifications"

pipeline:

  # ── Agent step ────────────────────────────────────────────────────────────
  - id: parse                   # unique step ID — used in depends_on and expressions
    agent: ktsu/secure-parser@1.0.0
    params:
      agent:                    # params.agent.* — values for the agent's declared params
        source_field: message
        extract:
          intent: { type: string, enum: [billing, technical, legal, other] }
    model:
      group: economy
      max_tokens: 512
    # no depends_on — receives workflow params

  - id: triage
    agent: ./agents/triage.agent.yaml
    depends_on: [parse]
    confidence_threshold: 0.7

  - id: recall
    agent: ./agents/recall.agent.yaml
    params:
      agent:
        persona: "billing specialist"
      server:
        memory:
          namespace: "{{ env.USER_NAMESPACE }}"
    depends_on: [parse]

  # ── Agent step with fanout ────────────────────────────────────────────────
  - id: enrich
    agent: ./agents/enricher.agent.yaml
    depends_on: [triage]
    for_each:
      from: step.triage.tickets  # JMESPath against accumulated step outputs — must resolve to an array
      max_items: 20
      concurrency: 4
      max_failures: -1

  # ── Transform step ────────────────────────────────────────────────────────
  - id: merge-reviews
    depends_on: [legal-review, risk-review]   # explicit ordering; deps may also be inferred from step.* refs in ops
    transform:
      ops:
        - merge: [step.legal-review, step.risk-review]
        - deduplicate: { field: ticket_id }
        - filter:      { expr: "confidence > 0.7" }
        - sort:        { field: confidence, order: desc }
        - map:         { expr: "{ id: ticket_id, score: confidence }" }
        - flatten:     {}
    output:
      schema:
        type: array
        items: { type: object }

  # ── Webhook step ──────────────────────────────────────────────────────────
  - id: notify
    webhook:
      url: "{{ env.SLACK_WEBHOOK_URL }}"
      method: POST
      body:
        text:    "{{ step.triage.summary }}"
        channel: "{{ params.channel_id }}"
        source:  "ktsu"                       # plain string = literal
      timeout_s: 10
    condition: "step.triage.category == 'billing'"   # bare JMESPath; step.* prefix required
    depends_on: [triage]

  # ── Approval notification step ────────────────────────────────────────────
  - id: notify-approval-needed
    on: approval
    depends_on: [enrich]
    webhook:
      url: "{{ env.APPROVAL_WEBHOOK_URL }}"
      method: POST
      timeout_s: 10

model_policy:
  cost_budget_usd: 0.50
  group_map:
    frontier: standard
  force_group: local
```

## Top-Level Fields

| Field | Type | Required | Description |
|---|---|---|---|
| `kind` | string | yes | Must be `workflow` |
| `name` | string | yes | Unique workflow name; used in `POST /invoke/{name}` |
| `version` | string | yes | Semver string |
| `description` | string | no | Human-readable description |
| `visibility` | string | no | `"root"` (default for local files) \| `"sub-workflow"` — root workflows can be invoked via `POST /invoke`; sub-workflows return 404 on direct invocation |
| `params.schema` | JSON Schema | yes (root) | For root workflows: validated against the HTTP request body; 422 on failure. For sub-workflows: declares named params the parent step must supply. |
| `env` | array | no | Declared environment variables. Each entry: `name` (string, required), `secret` (bool, default false), `description` (string, optional). Resolved at run start and injected into the envelope under `env`. Referencing an undeclared env var is a boot error. |
| `pipeline` | array | yes | Ordered list of pipeline steps |
| `webhooks` | string | no | `"execute"` \| `"suppress"` (default: `"suppress"`) — whether webhook steps fire when this workflow is used as a sub-workflow |
| `output` | object | no | Declares what the workflow returns when used as a subworkflow step. See Workflow Output. |
| `model_policy.cost_budget_usd` | number | no | LLM cost circuit breaker for the run |
| `model_policy.group_map` | object | no | Remaps model group names |
| `model_policy.force_group` | string | no | Overrides ALL model group declarations |
| `model_policy.timeout_s` | number | no | Per-run wall-clock timeout in seconds |

## Pipeline Envelope

Every step receives the full pipeline state as a JSON object in its first user message. The envelope has three namespaced top-level keys:

```json
{
  "env": {
    "SLACK_WEBHOOK_URL": "[hidden]",
    "USER_NAMESPACE":    "prod-us"
  },
  "params": {
    "message":    "I was charged twice",
    "user_id":    "U8821AB",
    "channel_id": "C04XZ99"
  },
  "step": {
    "parse":  { "intent": "billing" },
    "triage": { "category": "billing", "priority": "high" }
  }
}
```

| Key | Contents |
|---|---|
| `env` | Resolved values of all env vars declared in `env:`. Secrets appear as `[hidden]`. |
| `params` | Resolved workflow params. For root workflows: validated HTTP request body. For sub-workflows: values supplied by the parent step. |
| `step` | Step outputs keyed by step ID. Populated as steps complete. |

A step named `env`, `params`, or `step` is a boot error.

## Expression Syntax

Values in step `params:` blocks, webhook `body:` values, and `output.map` values use `{{ expr }}` for envelope references. Plain strings are always literals.

| Value | Resolves to |
|---|---|
| `"support-bot"` | Literal string `support-bot` |
| `"{{ env.SLACK_WEBHOOK_URL }}"` | Env var value |
| `"{{ params.user_id }}"` | Workflow param |
| `"{{ step.parse.channel_id }}"` | Step output field |
| `"{{ step.triage.category }}"` | Step output field |

**Type preservation:** When the entire string is a single `{{ expr }}`, the result is the typed value (object, array, number, bool). When `{{ expr }}` appears inside a larger string (e.g. `"Hello {{ params.name }}"`) the result is a string.

`expr:` fields in transform ops and `condition:` on steps use bare JMESPath — always an expression context, no `{{ }}` needed. Step references in these contexts require the `step.` prefix (e.g. `step.triage.category == 'billing'`).

## Agent Step Fields

| Field | Type | Required | Description |
|---|---|---|---|
| `id` | string | yes | Unique step ID; referenced in `depends_on` and output expressions |
| `agent` | string | yes | Built-in: `ktsu/<name>@<ver>`; local: `./agents/foo.agent.yaml` (optional `@<ver>`) |
| `depends_on` | string[] | no | Step IDs to wait for; omit to receive workflow input |
| `confidence_threshold` | number | no | Min `ktsu_confidence` required; agent output schema must declare `ktsu_confidence` |
| `params.agent` | map | no | Values for the agent's declared params. Overrides agent-file defaults. Values use `{{ expr }}` syntax for envelope references; plain strings are literals. |
| `params.server.<name>` | map | no | Values for the named server's declared params (`<name>` matches `servers[].name` in the agent file). Overrides agent-file server ref params and server defaults. Values use `{{ expr }}` syntax for envelope references; plain strings are literals. |
| `model.group` | string | no | Overrides agent file's model group |
| `model.max_tokens` | number | no | Token limit override |
| `for_each.from` | string | no | JMESPath resolving to an array; runs agent once per item |
| `for_each.max_items` | number | no | Truncates array before fanout |
| `for_each.concurrency` | number | no | Max parallel invocations; default: unbounded |
| `for_each.max_failures` | number | no | Max item failures to tolerate; `0` = fail on first error (default), `-1` = unlimited, `N` = tolerate up to N |
| `consolidation` | string | no | Fan-in strategy: `array` \| `merge` \| `first` — accepted but not yet enforced at runtime |

## Fanout Input and Output Contract

When `for_each` is set, the runner resolves `from` as a JMESPath expression against the pipeline envelope. It then invokes the agent once per element of the resulting array.

**Each agent invocation receives the standard pipeline envelope** (`env`, `params`, and `step` keys) **plus two additional keys:**

| Key | Value |
|---|---|
| `item` | The current array element (any JSON type) |
| `item_index` | Zero-based integer index of this element |

Example — if `step.triage.tickets` resolves to `[{...}, {...}, {...}]`, the agent for index 1 receives:

```json
{
  "env":    { "SLACK_WEBHOOK_URL": "[hidden]" },
  "params": { "message": "...", "user_id": "..." },
  "step": {
    "parse":  { "intent": "billing" },
    "triage": { "tickets": [...] }
  },
  "item":       { "id": "T-42", "subject": "..." },
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
  tickets: "{{ step.enrich.results }}"       # the full results array
  first:   "{{ step.enrich.results[0] }}"    # first result

# In a transform op:
- id: process-results
  depends_on: [enrich]
  transform:
    ops:
      - flatten: {}    # step.enrich.results is already an array; flatten if items are arrays
```

Metrics (tokens in/out, cost, LLM calls, tool calls) are summed across all fan-out invocations and reported as a single step metric.

Sub-invocation step IDs are `<step-id>.<index>` (e.g. `enrich.0`, `enrich.1`) and appear in logs.

## Fanout Failure Tolerance

By default (`max_failures: 0`), a single item failure fails the entire step. Set `max_failures` to tolerate partial failures:

```yaml
for_each:
  from: step.search-hn.repos
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
| `depends_on` | string[] | yes | Step IDs to wait for; auto-derived from `step.*` references in ops when possible |
| `transform.ops` | array | yes | Ordered op list; each op receives the output of the previous |
| `output.schema` | JSON Schema | yes | Air-Lock validated |

Transform ops reference step outputs as `step.<id>` or `step.<id>.<field>`:

```yaml
- id: merge-reviews
  depends_on: [legal-review, risk-review]
  transform:
    ops:
      - merge: [step.legal-review, step.risk-review]
      - filter: { expr: "confidence > 0.7" }
      - sort:   { field: confidence, order: desc }
  output:
    schema:
      type: array
      items: { type: object }
```

### Transform Ops

| Op | Arguments | Description |
|---|---|---|
| `merge` | `[step-id, ...]` | Combines outputs of two or more upstream steps. Reference steps as `step.<id>` for the full output or `step.<id>.<field>` for a specific field. Array outputs are concatenated; object outputs are deep-merged (later entries win on key conflicts). Mixing array and object outputs is a boot error. |
| `sort` | `{ field, order }` | Sorts array by JMESPath field; `order`: `asc` \| `desc` (default: `asc`) |
| `filter` | `{ expr }` | Removes items where JMESPath `expr` is falsy |
| `map` | `{ expr }` | Projects each item to a new shape via JMESPath |
| `flatten` | `{}` | Flattens one level of array nesting |
| `deduplicate` | `{ field }` | Removes duplicates by key field; first occurrence wins |

Example merge op:
```yaml
- merge: [step.legal-review, step.risk-review]
# or cherry-pick specific fields:
- merge: [step.legal-review.tickets, step.risk-review.tickets]
```

## Webhook Step Fields

| Field | Type | Required | Description |
|---|---|---|---|
| `id` | string | yes | Unique step ID |
| `webhook.url` | string | yes | Destination URL. Use `{{ env.VAR_NAME }}` to reference an environment variable. |
| `webhook.method` | string | no | HTTP method; default: `POST` |
| `webhook.body` | object | no | Key-value map. Values use `{{ expr }}` for envelope references; plain strings are literals. |
| `webhook.timeout_s` | number | no | Request timeout in seconds; default: 30 |
| `on` | string | no | `approval` — fires when a `depends_on` step enters `pending_approval`. Body is always the approval event (fixed JSON; `webhook.body` is ignored). |
| `condition` | string | no | JMESPath; if falsy step is `skipped` (not failed). Not evaluated for `on: approval` steps. |
| `depends_on` | string[] | no | Step IDs to wait for |

## Workflow Step

A workflow step invokes another workflow's full pipeline inline.

### Fields

| Field | Type | Description |
|---|---|---|
| `workflow` | string | Workflow reference: `ktsu/name` (shipped), `./path/file.yaml` (local), or absolute path |
| `params` | map | Param values for the sub-workflow. Values use `{{ expr }}` syntax for envelope references; plain strings are literals. |
| `webhooks` | `"execute"` \| `"suppress"` | Parent-side webhook opt-in. Webhooks fire only if BOTH this and the sub-workflow declare `execute` |
| `depends_on` | list | Step IDs this step depends on |

### Example

```yaml
- id: notify
  workflow: ktsu/slack-reply
  webhooks: execute
  params:
    webhook_url: "{{ env.SLACK_WEBHOOK_URL }}"
    username:    "support-bot"
    channel_id:  "{{ step.parse.channel_id }}"
    text:        "{{ step.agent.reply }}"
  depends_on: [agent]
```

## Workflow Output

A workflow declares `output:` to specify what it returns when used as a subworkflow step. Without `output:`, the step produces no usable output in the parent pipeline.

All steps in the sub-workflow — including webhook steps — complete before the output is assembled. Output is Air-Lock validated before the parent pipeline reads it.

Two mutually exclusive forms — `from` and `map` cannot both be present (boot error):

**`from` — step reference:**
```yaml
output:
  from: triage
  schema:
    type: object
    required: [category, priority]
    properties:
      category: { type: string }
      priority: { type: string }
```

**`map` — field mapping:**
```yaml
output:
  schema:
    type: object
    properties:
      summary: { type: string }
      tickets: { type: array }
  map:
    summary: "{{ step.triage.summary }}"
    tickets: "{{ step.enrich.results }}"
```

The workflow step's output is referenced in the parent as `step.<step-id>.<field>` (e.g. `step.notify.category`).

## params.schema

`params.schema` declares named input values required by a workflow (or agent). It uses JSON Schema format:

```yaml
params:
  schema:
    type: object
    required: [webhook_url]          # params without a default — callers must provide these
    properties:
      webhook_url:
        type: string
        description: "Slack webhook URL for outbound notifications"
      username:
        type: string
        description: "Display name for the bot"
        default: "ktsu-bot"          # optional param — callers may omit
```

- **Required params** have no `default`. Callers must provide them or the invocation fails at validation time.
- **Optional params** have a `default`. If the caller omits them, the default is used.
- Param values in a parent workflow step's `params:` block use `{{ expr }}` syntax for envelope references; plain strings are literals.
- Agent files and server files use the same param declaration pattern; see their respective YAML specs.

## Notes

- Every agent step receives the full pipeline envelope as JSON in its first user message. Reference values as `{{ params.field }}`, `{{ env.VAR }}`, or `{{ step.<id>.<field> }}` in agent system prompts. In bare JMESPath contexts (webhook conditions, transform op `expr:` fields), use `step.<id>.<field>` without `{{ }}`.
- Fanout step output is always `{ "results": [...] }` in original array order. Reference as `{{ step.enrich.results }}`.
- Transform `depends_on` is auto-derived from `step.*` references in ops — declare it explicitly when the dependency is not referenced in an op.
- Webhook non-2xx or network error → step fails immediately; no retry.
- Webhook `condition` false → step is `skipped`; downstream steps still run.
- `on: approval` steps fire when a depended-on agent step hits a `require_approval` gate. They do not fire on normal step completion.
- `pending_approval` is non-terminal. Independent pipeline branches continue while approval is pending.
- Sub-workflow `visibility: sub-workflow` — direct `POST /invoke` returns 404. They declare their named inputs using `params.schema`; the parent step must supply all required params.
