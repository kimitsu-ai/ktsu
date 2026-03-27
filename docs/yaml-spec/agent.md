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
system: |
  You are a triage agent. The full pipeline envelope is provided as JSON in the first user message.
  Reference upstream step outputs as <step-id>.<field> (e.g. parse.intent).
  Workflow input fields are under input.<field> (e.g. input.message).

servers:                         # omit entirely for a toolless agent
  - name: wiki-search            # logical name — used in logs
    path: servers/wiki-search.server.yaml  # relative to project root
    access:
      allowlist:
        - wiki-search            # exact tool name
        - crm-read-*             # prefix wildcard — any tool starting with "crm-read-"
        - "*"                    # all tools this server exposes

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
| `system` | string | yes | System prompt; reference upstream outputs as `<step-id>.<field>` and workflow input as `input.<field>` |
| `servers` | array | no | Tool servers this agent may call; omit for toolless agent |
| `servers[].name` | string | yes | Logical name — used in logs |
| `servers[].path` | string | yes | Path to `.server.yaml` file, relative to project root |
| `servers[].access.allowlist` | string[] | yes | Permitted tools: exact name, `prefix-*`, or `*` |
| `sub_agents` | string[] | no | Paths to `.agent.yaml` files this agent may invoke, relative to project root |
| `output.schema` | JSON Schema | yes | Air-Lock validated before downstream steps can read this agent's output |

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
    source_field: message      # which workflow input field contains the raw text
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

Every agent receives the full pipeline state as a JSON object in its **first user message**. The runtime assembles this envelope by collecting all outputs produced so far, keyed by step ID, and adds `"input"` for the original workflow input:

```json
{
  "input":  { "message": "...", "user_id": "..." },
  "parse":  { "intent": "billing" },
  "triage": { "category": "billing", "priority": "high" }
}
```

The messages sent to the LLM are therefore:

```
system  → <system prompt text>
user    → <JSON envelope above>
```

**Referencing values in the system prompt:**

| What you want | How to write it |
|---|---|
| Workflow input field `message` | `input.message` |
| Output field `intent` from step `parse` | `parse.intent` |
| Fan-out current item (inside `for_each` step) | `item` / `item_index` |

These are just JSON path conventions for the LLM — use them in prose instructions inside `system`. For JMESPath expressions in `webhook.body` or transform ops, the same keys apply (e.g. `input.message`, `triage.category`).

## Notes

- A toolless agent (no `servers` block) has no tools to exploit — recommended as the first pipeline step when handling raw user input.
- Allowlist wildcards: `*` (all tools), `prefix-*` (prefix match). Mid-string wildcards are a boot error.
- Sub-agents are referenced by file path only (no logical name). They do not appear in the pipeline DAG and their cost rolls up to the parent step.
