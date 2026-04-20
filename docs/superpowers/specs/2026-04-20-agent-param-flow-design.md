# Agent Param Flow Redesign

**Date:** 2026-04-20  
**Status:** Approved for implementation

## Problem

The current agent step param design has three interconnected issues:

1. **Unnecessary nesting** — agent step params require `params.agent.*` nesting, inconsistent with how workflow step params work at top level.
2. **Data leak** — `executeAgent` forwards all upstream step outputs to the agent runtime as `InvokeRequest.Input`, the entire blob becoming the user message. The agent sees data it was never explicitly given.
3. **Cache-busting system prompts** — `prompt.system` supports `{{ params.* }}` interpolation, causing the system prompt to vary per invocation and preventing Anthropic prompt cache hits.
4. **Server params owned by the wrong layer** — server params live on the workflow step (`params.server.*`), forcing the workflow author to know server internals. The agent file is the right owner.
5. **No secret propagation enforcement** — secrets resolved from `env:` references can silently flow into stored envelope records.

## Goals

- Flat top-level agent step params (no `agent:` nesting)
- Static system prompts enforced at load time — always cache-friendly
- Explicit dynamic input via `prompt.user` template
- Agent file owns server param wiring
- Secret params validated at each boundary; scrubbed from envelope storage
- No upstream step outputs forwarded to the agent

## Data Flow

### Before

```
workflow step
  └─ params.agent.*  ──────────────────────► interpolated into system prompt (breaks caching)
  └─ params.server.{name}.* ───────────────► server MCP init params
  └─ all upstream step outputs (Input) ────► user message (raw JSON blob, data leak)
```

### After

```
workflow step
  └─ params.* (flat, {{ }} templates resolved against step outputs)
       └─ agent params
            ├─ prompt.system (static, validated no {{ }}, cached)  ──► LLM system turn
            ├─ prompt.user (optional {{ params.* }} template)  ──────► LLM user turn
            │    └─ fallback: resolved params as JSON
            └─ ServerRef.params ({{ params.* }} in agent file)  ─────► server MCP init
```

Step outputs are used **only** to resolve `{{ }}` templates in step params. They are never forwarded to the agent.

## Section 1: Config Schema Changes

### Agent YAML — `prompt` block

```yaml
prompt:
  system: "You are a friendly greeter. Return exactly the JSON output described in your schema."
  user: "Name: {{ params.name }}\nMessage: {{ params.message }}"  # optional
```

- `prompt.system` — static string. Validated at load time to contain no `{{ }}` expressions. Hard error if violated.
- `prompt.user` — optional template. Interpolated against resolved agent params at dispatch time. If absent, resolved params are JSON-serialized as the user message.
- Secret params are **blocked** from appearing in `prompt.user`. Hard error at dispatch.

### Agent YAML — server params

```yaml
servers:
  - name: memory
    path: servers/memory.server.yaml
    params:
      namespace: "{{ params.namespace }}"  # references agent's own params; static values also valid
```

`{{ params.* }}` expressions in `ServerRef.Params` are evaluated against the agent's resolved params. The agent file owns the server wiring — the workflow step has no `params.server.*` key.

### Server YAML — secret param declarations

```yaml
params:
  api_token:
    type: string
    secret: true
```

### Agent param schema — secret declarations

```yaml
params:
  schema:
    type: object
    required: [api_token]
    properties:
      api_token:
        type: string
        secret: true
```

### Workflow step YAML — flat params

```yaml
# Before
- id: greet
  agent: agents/greeter.agent.yaml
  params:
    agent:
      name: "{{ step.receive.user.first_name }}"
      message: "{{ step.receive.text }}"

# After
- id: greet
  agent: agents/greeter.agent.yaml
  params:
    name: "{{ step.receive.user.first_name }}"
    message: "{{ step.receive.text }}"
```

`params.agent:` nesting and `params.server:` on workflow steps are removed.

## Section 2: Secret Propagation

A secret contract is enforced at every boundary:

```
server.yaml:   params.api_token: { secret: true }
                 ↑ validated at agent load time
agent.yaml:    params.api_token: { secret: true }   ← must be marked secret
                 ↑ validated at dispatch time
workflow step: api_token: "env:API_KEY"              ← env: implies secret
```

**Rules:**
- A secret server param may only be fed from a secret agent param. Validation error otherwise.
- A secret agent param may only be fed from an `env:` source at the workflow step level. Both literal strings and `{{ }}` template expressions are validation errors — there is no static way to verify that a template resolves to a secret, so `env:` is the only permitted form.
- Secret params cannot appear in `prompt.user` templates. Hard error at dispatch.

**Envelope scrubbing:**
- Resolved secret param values are collected into a `scrubSet` during param resolution.
- Before any string is written to the envelope (step output, step error, run error), it is passed through a scrubber that replaces any known secret value with `[REDACTED]`.
- Structured param values marked secret are written as `[REDACTED]` directly.

This covers: structured fields (preventive), error strings (best-effort), LLM outputs (clean by construction — secret values never reach the LLM).

## Section 3: Runtime Protocol Changes

### `InvokeRequest`

```go
type InvokeRequest struct {
    RunID               string            `json:"run_id"`
    StepID              string            `json:"step_id"`
    AgentName           string            `json:"agent_name"`
    System              string            `json:"system"`
    UserMessage         string            `json:"user_message"`  // replaces Input
    Reflect             string            `json:"reflect,omitempty"`
    ConfidenceThreshold float64           `json:"confidence_threshold,omitempty"`
    MaxTurns            int               `json:"max_turns"`
    Model               ModelSpec         `json:"model"`
    ToolServers         []ToolServerSpec  `json:"tool_servers"`
    CallbackURL         string            `json:"callback_url"`
    OutputSchema        map[string]any    `json:"output_schema,omitempty"`
    // Resume fields — unchanged
    Messages            []Message         `json:"messages,omitempty"`
    ApprovedToolCalls   []string          `json:"approved_tool_calls,omitempty"`
    IsResume            bool              `json:"is_resume,omitempty"`
}
```

`Input map[string]any` is removed. `UserMessage string` is set by the dispatcher from `prompt.user` interpolation or params JSON fallback.

### `ToolServerSpec`

```go
type ToolServerSpec struct {
    Name          string             `json:"name"`
    URL           string             `json:"url"`
    Allowlist     []string           `json:"allowlist"`
    AuthToken     string             `json:"auth_token,omitempty"`
    Params        map[string]any     `json:"params,omitempty"`
    SecretKeys    []string           `json:"secret_keys,omitempty"`  // param keys to redact in runtime logs
    ApprovalRules []ToolApprovalRule `json:"approval_rules,omitempty"`
}
```

## Section 4: Code Changes by Layer

### `internal/config/types.go`
- `PromptConfig` adds `User string` yaml field
- `ServerRef.Params` stays `map[string]string` — values may now be `{{ params.* }}` templates
- `AgentParams()` returns all top-level `Params` entries excluding the `server:` key
- `ServerParams()` removed from `PipelineStep`

### `internal/config/params.go`
- `ValidatePromptRefs` — repurposed to reject `{{ }}` in `prompt.system` (hard error)
- `InterpolatePrompt` — unchanged, used for `prompt.user`
- `ResolveAgentParams` — signature becomes `(declared, stepParams) (map[string]string, map[string]bool, error)` where bool map is `isSecret[name]`
- `ResolveServerParams` — drops step-level params argument; resolves `ServerRef.Params` `{{ params.* }}` templates against resolved agent params; validates secret constraints
- New: `BuildScrubSet(resolved map[string]string, isSecret map[string]bool) []string`

### `internal/config/` — server YAML types
- `ToolServerConfig` param declarations gain `secret: bool` field

### `internal/orchestrator/runner/runner.go`
- `executeAgent` — resolves top-level step params (no `agent:` nesting); builds scrub set; passes `richParams` for expression context
- Envelope writes (step output, step error) pass through scrubber
- All `ServerParams()` call sites removed

### `internal/orchestrator/server.go`
- Validates `prompt.system` has no `{{ }}` at dispatch (belt-and-suspenders)
- Validates secret server params fed from secret agent params
- Blocks secret params from `prompt.user` interpolation
- Interpolates `prompt.user`; falls back to params JSON
- Builds `ToolServerSpec.SecretKeys`
- No longer reads `step.ServerParams()`
- Sets `InvokeRequest.UserMessage` instead of `InvokeRequest.Input`

### `internal/runtime/agent/loop.go`
- `{Role: "user", Content: req.UserMessage}` replaces `{Role: "user", Content: string(inputJSON)}`

### Example files
- `examples/telegram/agents/greeter.agent.yaml` — static system prompt, add `prompt.user`
- `examples/telegram/workflows/telegram-echo.workflow.yaml` — flatten `params.agent.*` to top-level

## Section 5: Testing Strategy

### `internal/config` — load-time validation
- `prompt.system` with `{{ }}` → hard error
- Secret server param fed from non-secret agent param → validation error
- Secret agent param fed from non-`env:` source → validation error
- `AgentParams()` returns top-level params excluding `server:` key

### `internal/config` — param resolution
- `ResolveAgentParams` returns correct `isSecret` map
- `ResolveServerParams` resolves `{{ params.* }}` templates against agent params
- `BuildScrubSet` returns only secret values, not plain param values
- Secret param in `prompt.user` template → hard error

### `internal/orchestrator/runner`
- Update `TestRunner_agentParamTemplateResolution` for flat params (remove `agent:` nesting)
- Secret param value scrubbed from step error string before envelope write
- Secret param value scrubbed from step output before envelope write

### `internal/orchestrator/server`
- `prompt.user` interpolated → correct `InvokeRequest.UserMessage`
- No `prompt.user` → params JSON fallback in `UserMessage`
- `ToolServerSpec.SecretKeys` populated for secret server params
- `InvokeRequest.Input` absent

### `internal/runtime/agent`
- `req.UserMessage` used as user turn content
- Resume path (saved `Messages`) unchanged

## Breaking Changes

- `params.agent:` nesting on workflow steps — removed; use flat top-level params
- `params.server:` on workflow steps — removed; move to agent file `ServerRef.Params`
- `prompt.system` with `{{ }}` expressions — load error
- `InvokeRequest.Input` — removed; callers must use `UserMessage`
- `ServerParams()` on `PipelineStep` — removed
