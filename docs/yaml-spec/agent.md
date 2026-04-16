# agent.yaml

**What it does:** Defines an LLM agent — its model group, system prompt, tool servers, optional sub-agents, and typed output schema.

**Filename convention:** `agents/*.agent.yaml` — no `kind` field.

## Annotated Example

```yaml
# No kind field
name: triage-agent               # identity — used in logs and metrics
description: "..."               # optional
model: standard                  # model group name from gateway.yaml
max_turns: 10                    # max reasoning turns before forced conclusion; default: 10
params:                          # declared parameters — JSON Schema format
  schema:
    type: object
    required: [escalation_team]
    properties:
      persona:
        type: string
        description: "The role this agent plays"
        default: "triage specialist"
      escalation_team:
        type: string
        description: "Team name for escalations — required, no default"

prompt:
  system: |
    You are a {{ params.persona }}. The full pipeline envelope is provided as JSON in the first user message.
    Escalate critical issues to the {{ params.escalation_team }} team.
    Reference upstream step outputs as step.<id>.<field> (e.g. step.parse.intent).
    Workflow params are under params.<field> (e.g. params.message).
    Env vars are under env.<name> (e.g. env.DATABASE_URL).

reflect: |
  Review your classification above.
  1. Is the category unambiguous given the input?
  2. Is your confidence score justified by the evidence?
  If you would classify differently, return a complete revised output.
  If confident in your original output, return it unchanged.

servers:                         # omit entirely for a toolless agent
  - name: wiki-search            # logical name — used in logs
    path: servers/wiki-search.server.yaml  # relative to project root
    params:                      # values for this server's declared params
      region: "us-east"
    access:
      allowlist:
        - wiki-search            # exact tool name (plain string)
        - crm-read-*             # prefix wildcard — any tool starting with "crm-read-"
        - "*"                    # all tools this server exposes
        - name: delete-*         # object form — adds require_approval policy
          require_approval:
            on_reject: fail      # "fail" | "recover"
            timeout: 30m         # optional — duration string; omit for no timeout
            timeout_behavior: reject  # "fail" | "reject" — required when timeout is set

sub_agents:                      # sub-agents this agent may invoke (optional)
  - agents/summarizer.agent.yaml # path relative to project root

output:
  schema:                        # JSON Schema — Air-Lock validated before downstream steps read it
    type: object
    required: [category, priority, ktsu_confidence]
    properties:
      category:        { type: string, enum: [billing, technical, legal] }
      priority:        { type: string, enum: [low, medium, high] }
      ktsu_confidence: { type: number, minimum: 0, maximum: 1 }   # reserved
      ktsu_flags:      { type: array, items: { type: string } }    # reserved
      ktsu_rationale:  { type: string }                            # reserved
```

## Fields

| Field | Type | Required | Description |
|---|---|---|---|
| `name` | string | yes | Identity — used in logs and metrics |
| `description` | string | no | Human-readable description |
| `model` | string | yes | Model group name from `gateway.yaml` |
| `max_turns` | number | no | Max reasoning turns before forced conclusion; default: 10 |
| `params` | map | no | Declared parameters in JSON Schema format (`params.schema`). Required params have no `default`; optional params have a `default`. Missing required params are a boot error. Agent files may not use `env:` references directly — values are passed in from the parent workflow's `params.agent.*` block and are available in the envelope as `{{ params.name }}`. |
| `params.schema` | JSON Schema | no | Schema object declaring named params: `type: object`, optional `required: [...]`, and `properties` map with per-param `type`, `description`, and optional `default`. |
| `params.schema.properties.<name>.description` | string | yes | Human-readable explanation of what the param controls |
| `params.schema.properties.<name>.default` | string | no | Default value. Omit to make the param required. |
| `prompt.system` | string | yes | System prompt. May reference envelope values using `{{ expr }}` syntax: `{{ params.name }}`, `{{ env.VAR }}`, `{{ step.id.field }}`. |
| `reflect` | string | no | Reflection prompt. After the reasoning loop produces a draft output, the agent evaluates it in a single additional LLM turn before Air-Lock runs. The reflected output replaces the draft entirely. Supports `{{ expr }}` interpolation against the full pipeline envelope. See Reflect Trigger Logic. |
| `servers` | array | no | Tool servers this agent may call; omit for toolless agent |
| `servers[].name` | string | yes | Logical name — used in logs |
| `servers[].path` | string | yes | Path to `.server.yaml` file, relative to project root |
| `servers[].params` | map | no | Values for this server's declared params. Overrides server defaults; can be overridden by workflow step `params.server.<name>.*`. Agent files may not use `env:` references — pass env var values down from the parent workflow via `params.agent.*`. |
| `servers[].access.allowlist` | string[] or object[] | yes | Permitted tools: exact name, `prefix-*`, or `*`. Each entry may be a plain string or an object `{name, require_approval}` to add an approval policy. |
| `servers[].access.allowlist[].require_approval.on_reject` | string | yes (if require_approval set) | `fail` — step fails on rejection. `recover` — agent receives a rejection message and may try an alternative. |
| `servers[].access.allowlist[].require_approval.timeout` | duration | no | Approval deadline (e.g. `30m`, `2h`). If no decision is received in time, `timeout_behavior` applies. |
| `servers[].access.allowlist[].require_approval.timeout_behavior` | string | yes (if timeout set) | `fail` — step fails on timeout. `reject` — treated as a rejection (respects `on_reject`). |
| `sub_agents` | string[] | no | Paths to `.agent.yaml` files this agent may invoke, relative to project root |
| `output.schema` | JSON Schema | yes | Air-Lock validated before downstream steps can read this agent's output |

## Reflect Trigger Logic

The reflect turn does not always run. Whether it fires depends on `ktsu_confidence` in the output schema and `confidence_threshold` on the pipeline step.

| `ktsu_confidence` in schema | `confidence_threshold` on step | Reflect runs when |
|---|---|---|
| No | — | Always |
| Yes | Not set | Always |
| Yes | Set | Draft confidence < threshold only |

This creates a natural incentive: agents that declare `ktsu_confidence` and configure a threshold skip the reflect turn — and its token cost — on high-confidence outputs.

## Reserved Output Fields (`ktsu_` prefix)

| Field | Type | Description |
|---|---|---|
| `ktsu_injection_attempt` | boolean | Fail the entire run immediately; use for clear prompt injection attempts |
| `ktsu_untrusted_content` | boolean | Fail the step; use for suspicious content that doesn't rise to injection |
| `ktsu_confidence` | number (0–1) | Fail the step if below the step's `confidence_threshold`; recorded for observability otherwise |
| `ktsu_low_quality` | boolean | Fail the step; use when the agent cannot produce a reliable output |
| `ktsu_needs_human` | boolean | Fail the run with `needs_human_review` error code; surfaces run for human review |
| `ktsu_skip_reason` | string | Mark the step `skipped` with the provided reason; downstream steps are also skipped; not a failure |
| `ktsu_flags` | string[] | Arbitrary warning or flag strings; recorded in metrics, no pipeline effect |
| `ktsu_rationale` | string | Agent's reasoning trace; recorded in metrics, no pipeline effect |

Any `ktsu_` field not in this list is a boot error.

## Built-in Agents

| Agent | Description |
|---|---|
| `ktsu/secure-parser@1.0.0` | Toolless hardened parser for untrusted text; parameterized by `params.extract` schema |

### `ktsu/secure-parser` params

```yaml
- id: parse
  agent: ktsu/secure-parser@1.0.0
  params:
    agent:
      source_field: message      # which workflow param contains the raw text
      extract:
        intent:
          type: string
          enum: [billing, technical, legal, other]
          description: "What the user is asking for"
  model:
    group: economy
    max_tokens: 512
```

Output always includes `ktsu_injection_attempt`, `ktsu_confidence`, `ktsu_low_quality`, and `ktsu_flags` in addition to declared `extract` fields.

## Input Envelope

Every agent receives the full pipeline state as a JSON object in its **first user message**:

```json
{
  "env": {
    "SLACK_WEBHOOK_URL": "[hidden]",
    "DATABASE_URL":      "postgres://..."
  },
  "params": {
    "message":  "I was charged twice for my subscription",
    "user_id":  "U8821AB"
  },
  "step": {
    "parse":  { "intent": "billing" },
    "triage": { "category": "billing", "priority": "high" }
  }
}
```

**Referencing values in the system prompt:**

| What you want | How to write it |
|---|---|
| Workflow param `message` | `{{ params.message }}` |
| Env var `DATABASE_URL` | `{{ env.DATABASE_URL }}` |
| Output field `intent` from step `parse` | `{{ step.parse.intent }}` |
| Fan-out current item (inside `for_each`) | `item` / `item_index` |

`{{ expr }}` resolves to the typed value when the entire value is the expression; coerces to string when mixed with literal text.

## Notes

- Agent files may not use `env:` references directly. Declare env vars in the workflow file's `env:` array; they are available in the agent envelope as `env.<name>` and via `{{ env.NAME }}` interpolation.
- A toolless agent (no `servers` block) has no tools to exploit — recommended as the first pipeline step when handling raw user input.
- Allowlist wildcards: `*` (all tools), `prefix-*` (prefix match). Mid-string wildcards are a boot error.
- Allowlist entries may be plain strings or objects with `require_approval` to gate tool calls on human approval. When matched, the agent runtime suspends the run and sends a `pending_approval` callback; execution resumes after a decision is posted to `POST /runs/{run_id}/steps/{step_id}/approval/decide`.
- Sub-agents are referenced by file path only (no logical name). They do not appear in the pipeline DAG and their cost rolls up to the parent step.
