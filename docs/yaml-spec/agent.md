# agent.yaml

**What it does:** Defines an LLM agent — its model group, system and user prompts, tool servers, optional sub-agents, and typed output schema.

**Filename convention:** `agents/*.agent.yaml` — no `kind` field.

## Annotated Example

```yaml
# No kind field needed for agents
name: triage-agent               # identity — used in logs and metrics
description: "Support triage"    # optional — human-readable description
model: standard                  # model group name as defined in gateway.yaml
max_turns: 10                    # optional — max reasoning turns before forced conclusion; default: 10
params:                          # declared parameters — used for prompt interpolation and server config
  schema:                        # JSON Schema format
    type: object
    required: [message, team]
    properties:
      message:
        type: string
        description: "The raw support message to analyze"
      team:
        type: string
        description: "The target escalation team"

prompt:
  # system prompt must be STATIC (no {{ }} expressions) to support prompt caching
  system: |
    You are a triage specialist. Analyze the user message and categorize it.
    Use the provided tools if you need more information about the user's history.
  # user prompt supports {{ params.NAME }} interpolation
  user: |
    Help me triage this message: {{ params.message }}
    The customer is in team: {{ params.team }}

  reflect: |                       # optional — evaluation turn after reasoning loop
    Review your classification above.
    1. Is the category unambiguous given the input?
    2. Is your confidence score justified?
    If you would classify differently, return a complete revised output.

servers:                         # optional — tool servers this agent may invoke
  - name: wiki-search            # logical name — used in logs
    path: servers/wiki.server.yaml # relative to project root
    params:                      # values for this server's declared params
      region: "us-east"          # literal value
      api_key: "{{ params.key }}" # template value — pulled from agent params
    access:                      # controls tool permissions
      allowlist:
        - wiki-search            # exact tool name
        - crm-read-*             # prefix wildcard
        - name: delete-*         # object form — adds approval policy
          require_approval:
            on_reject: fail      # "fail" | "recover"
            timeout: 30m         # optional — e.g. "30m", "2h"
            timeout_behavior: reject # "fail" | "reject"

sub_agents:                      # optional — agents this agent may invoke
  - agents/summarizer.agent.yaml # path relative to project root

output:
  schema:                        # JSON Schema — Air-Lock validated before downstream consumption
    type: object
    required: [category, priority, ktsu_confidence]
    properties:
      category:        { type: string, enum: [billing, technical, legal] }
      priority:        { type: string, enum: [low, medium, high] }
      ktsu_confidence: { type: number, minimum: 0, maximum: 1 } # reserved — triggers reflect/fail on threshold
      ktsu_rationale:  { type: string }                         # reserved — agent's reasoning trace
```

## Fields

| Field | Type | Required | Description |
|---|---|---|---|
| `name` | string | yes | Identity used in logs and metrics |
| `description` | string | no | Human-readable explanation |
| `model` | string | yes | Model group name from `gateway.yaml` |
| `max_turns` | number | no | Max reasoning turns before forced conclusion; default: 10 |
| `params.schema` | object | no | JSON Schema declaring named params (`type: object`). These are passed from workflows. |
| `prompt.system` | string | yes | **MUST BE STATIC**. No `{{ }}` allowed. Encourages prompt caching. |
| `prompt.user` | string | yes | Supports `{{ params.NAME }}` interpolation. |
| `prompt.reflect` | string | no | Reflection prompt. Runs one additional LLM turn to review results. Supports `{{ }}`. |
| `servers` | array | no | List of tool servers the agent can access. |
| `servers[].params` | map | no | Overrides for server parameters. Supports `{{ params.NAME }}`. |
| `servers[].access.allowlist` | array | yes | Tools permitted: `name`, `prefix-*`, or `*`. Can be objects with `require_approval`. |
| `output.schema` | object | yes | Final result schema. Air-Lock validated. |

## Variable Substitution

Agents use `{{ params.NAME }}` to reference parameters declared in the `params.schema` block. The `params.` prefix is strictly required; shorthand access (e.g. `{{ message }}`) is not supported to ensure clear scoping.

- **`{{ params.message }}`**: Accesses the value passed into the agent.
- **Static System Prompts**: Using `{{ }}` in `prompt.system` is a boot error. All dynamic content must go in `prompt.user`.

## Reserved Output Fields (`ktsu_` prefix)

These fields have special meaning to the Kimitsu runtime:

| Field | Type | Description |
|---|---|---|
| `ktsu_injection_attempt` | boolean | If true, fails the entire run immediately. |
| `ktsu_untrusted_content` | boolean | If true, fails the step (untrusted input detected). |
| `ktsu_confidence` | number (0–1) | Triggers `reflect` or fails step if below `confidence_threshold`. |
| `ktsu_low_quality` | boolean | If true, fails the step (low quality output). |
| `ktsu_needs_human` | boolean | Stops the run and flags it for human review. |
| `ktsu_skip_reason` | string | If non-empty, marks the step `skipped` instead of `complete`. |
| `ktsu_flags` | string[] | Warning strings recorded in metrics; no pipeline effect. |
| `ktsu_rationale` | string | Agent's reasoning trace; recorded in metrics. |

## Notes

- **Secrets**: Agent files never reference `env` directly. Credentials must be passed from a workflow via a parameter marked `secret: true`.
- **Tool Access**: `*` allows all tools from a server. Use `require_approval` to gate destructive actions.
