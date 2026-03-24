# Agent Runtime Design

**Date:** 2026-03-23
**Status:** Approved

---

## Overview

The Agent Runtime is a generic, stateless HTTP service that executes agent reasoning loops on behalf of the orchestrator. It receives invocation payloads from the orchestrator, runs the LLM reasoning loop (calling the LLM Gateway and MCP tool servers), and POSTs results back to the orchestrator when done.

The runtime holds no workflow-specific code. What makes an invocation a specific agent is the payload: the system prompt, input data, tool server endpoint URLs, model group, and turn limit. The runtime is horizontally scalable — each invocation is independent.

---

## HTTP Contract

### `POST /invoke`

Called by the orchestrator to dispatch an agent step. The runtime accepts the request, returns **202 immediately**, and runs the reasoning loop in a goroutine. Results are delivered via `callback_url`.

**Request:**

```json
{
  "run_id":     "run_abc123",
  "step_id":    "triage",
  "agent_name": "triage-agent",
  "system":     "You are a triage agent. Classify the incoming support request.",
  "max_turns":  10,
  "model": {
    "group":      "standard",
    "max_tokens": 2048
  },
  "input": {
    "message":  "I was charged twice for my subscription",
    "channel":  "#support",
    "user_id":  "U8821AB"
  },
  "tool_servers": [
    { "name": "ktsu/kv",  "url": "http://ktsu-kv:8090",  "allowlist": ["kv-get", "kv-set"] },
    { "name": "ktsu/log", "url": "http://ktsu-log:8091", "allowlist": ["log-write"] }
  ],
  "callback_url": "http://orchestrator:8080/runs/run_abc123/steps/triage/complete"
}
```

| Field | Type | Required | Notes |
|---|---|---|---|
| `run_id` | string | yes | Forwarded to the LLM Gateway on every call |
| `step_id` | string | yes | Forwarded to the LLM Gateway on every call |
| `agent_name` | string | yes | Informational; used in logs |
| `system` | string | yes | LLM system prompt |
| `max_turns` | int | yes | Maximum LLM gateway calls (see Turn Limit) |
| `model.group` | string | yes | Model group name in `gateway.yaml` |
| `model.max_tokens` | int | yes | Output token cap per LLM call |
| `input` | object | yes | Step input data; JSON-serialized into the first user message |
| `tool_servers` | array | no | Tool servers the agent may call; empty means no tool use |
| `tool_servers[].name` | string | yes | Server name for logging |
| `tool_servers[].url` | string | yes | MCP server base URL |
| `tool_servers[].allowlist` | []string | yes | Permitted tool names; empty allowlist permits nothing |
| `callback_url` | string | yes | Orchestrator endpoint for result delivery |

**Response (202 Accepted):**

```json
{ "run_id": "run_abc123", "step_id": "triage", "status": "accepted" }
```

### `GET /health`

Returns `{"status": "ok"}`. Used for liveness checks.

---

## Callback — Runtime → Orchestrator

When the reasoning loop finishes (success or failure), the runtime POSTs to `callback_url`:

**Success:**

```json
{
  "run_id":  "run_abc123",
  "step_id": "triage",
  "status":  "ok",
  "output": {
    "category":      "billing",
    "ktsu_confidence": 0.95,
    "ktsu_flags":    []
  },
  "error": "",
  "metrics": {
    "model_resolved": "anthropic/claude-sonnet-4-6",
    "tokens_in":     2104,
    "tokens_out":    512,
    "cost_usd":      0.0341,
    "duration_ms":   3200,
    "tool_calls":    5
  }
}
```

**Failure:**

```json
{
  "run_id":  "run_abc123",
  "step_id": "triage",
  "status":  "failed",
  "output":  null,
  "error":   "max_turns_exceeded",
  "metrics": {
    "model_resolved": "anthropic/claude-sonnet-4-6",
    "tokens_in":     8200,
    "tokens_out":    1800,
    "cost_usd":      0.12,
    "duration_ms":   14000,
    "tool_calls":    22
  }
}
```

The `output` field contains the raw LLM JSON output, **including any `ktsu_*` fields**. The orchestrator processes reserved fields and runs Air-Lock on the `output` after receiving the callback. The runtime is transparent to `ktsu_*` semantics.

---

## Orchestrator: New Endpoint

`POST /runs/{run_id}/steps/{step_id}/complete`

Receives the callback payload. Orchestrator responsibilities:
1. Parse callback, extract `output`, `status`, `error`, `metrics`
2. Call `processReservedFields(output)` — already implemented in `runner.go`
3. Run Air-Lock validation
4. Update step in state store: status, output, metrics, `completed_at`
5. Resolve DAG: trigger any downstream steps that are now unblocked

Also: `runner.executeAgent` is updated to call `POST {runtime_url}/invoke` and return immediately (the goroutine waits for the callback).

---

## Reasoning Loop

```
1. For each tool_server:
   - POST tools/list (MCP JSON-RPC) to server URL
   - Prune result against server allowlist
   - Collect pruned ToolDefinitions

2. Build initial messages:
   - {"role": "system", "content": system}
   - {"role": "user",   "content": JSON.stringify(input)}

3. turn = 1
   Loop:
   a. If turn == max_turns: append forced-conclusion user message (see Turn Limit)
   b. POST gateway /invoke {run_id, step_id, group, max_tokens, messages, tools}
   c. Accumulate tokens_in, tokens_out, cost_usd
   d. If response contains tool_calls:
      - For each tool call:
        i.  Verify tool name is in allowlist — if not, fail step with tool_not_permitted
        ii. POST tools/call to the named server (MCP JSON-RPC)
        iii.Append tool_use block to messages
        iv. Append tool_result block to messages
      - turn++; continue loop
   e. If no tool calls:
      - Parse response.content as JSON → output
      - Break
   f. If turn > max_turns: fail with max_turns_exceeded

4. POST callback_url with output + accumulated metrics
```

---

## Turn Limit

`max_turns` is the maximum number of LLM gateway calls within one invocation. Each `POST gateway /invoke` consumes one turn.

- The LLM is **never told how many turns remain** during normal execution.
- On turn `max_turns` (the last allowed call), before invoking the gateway, append:

  ```
  {"role": "user", "content": "You have reached the maximum number of tool calls. Provide your final answer now without requesting any additional tools."}
  ```

- If the LLM still returns tool calls after the forced-conclusion message → step fails with `max_turns_exceeded`.

This gives the LLM a chance to conclude gracefully rather than hard-failing.

---

## Reserved Output Fields (`ktsu_*`)

The LLM's final JSON response may include `ktsu_*` fields that carry signals for the orchestrator:

| Field | Orchestrator Effect |
|---|---|
| `ktsu_injection_attempt: true` | Step fails immediately (before Air-Lock) |
| `ktsu_untrusted_content: true` | Step fails |
| `ktsu_low_quality: true` | Step fails |
| `ktsu_needs_human: true` | Step fails |
| `ktsu_confidence: float64` | Checked against `confidence_threshold`; below → fail |
| `ktsu_skip_reason: string` | Step marked `skipped` |
| `ktsu_flags: []string` | Stored in step record |
| `ktsu_rationale: string` | Stored in step record |

The runtime passes `output` verbatim (all `ktsu_*` keys included) to the callback. The orchestrator's `processReservedFields` (implemented in `runner.go`) handles interpretation. The runtime must not strip, transform, or react to `ktsu_*` fields.

---

## MCP Client (JSON-RPC over HTTP)

Tool servers implement MCP over HTTP using JSON-RPC 2.0.

**tools/list:**

```
POST {url}/
Content-Type: application/json

{"jsonrpc":"2.0","method":"tools/list","id":1}
```

Response:
```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "tools": [
      { "name": "kv-get", "description": "...", "inputSchema": { ... } },
      { "name": "kv-set", "description": "...", "inputSchema": { ... } }
    ]
  }
}
```

**tools/call:**

```
POST {url}/
Content-Type: application/json

{"jsonrpc":"2.0","method":"tools/call","params":{"name":"kv-get","arguments":{"key":"user:123"}},"id":2}
```

Response:
```json
{
  "jsonrpc": "2.0",
  "id": 2,
  "result": { "content": [{ "type": "text", "text": "{\"value\":\"active\"}" }] }
}
```

**Allowlist enforcement** is applied twice:
1. At **discovery**: prune `tools/list` result to the declared allowlist before building the LLM context. The agent never sees tools it is not permitted to call.
2. At **call time**: before making any `tools/call` request, verify the tool name is in the allowlist. A call to an unlisted tool returns a structured `tool_not_permitted` error to the loop and fails the step.

---

## Fan-Out

When the orchestrator dispatches the same logical step against multiple items (e.g., one invocation per file), it generates a unique `step_id` per item:

- Logical step `process_file` over 3 files → `process_file.0`, `process_file.1`, `process_file.2`
- Each is an independent `(run_id, step_id)` pair — a separate row in the state store and a separate `POST /invoke` to the runtime.

The runtime is unaware of fan-out. Each invocation is just a step. The orchestrator manages fan-out ID generation, parallel tracking, and downstream consolidation.

---

## Heartbeat

The runtime maintains an in-process set of active `(run_id, step_id)` pairs. A background goroutine (started in `Runtime.Start`) fires every 5 seconds and POSTs to `{orchestrator_url}/heartbeat`:

```json
{
  "runtime_id": "rt-1",
  "active": [
    { "run_id": "run_abc123", "step_id": "triage" },
    { "run_id": "run_def456", "step_id": "legal-review" }
  ]
}
```

Active invocations are tracked in a `sync.Map` keyed by `"run_id/step_id"`. Entries are added when the runtime accepts a `POST /invoke` (before returning 202) and removed when the callback POST to the orchestrator returns.

If the runtime has no active invocations, the heartbeat POST sends an empty `active` array — this is not an error.

---

## Gateway Extension Required

The current LLM Gateway `InvokeRequest.messages[].content` is `string`. Tool calling requires structured content blocks:

- `tools []ToolDefinition` in `InvokeRequest` (passed through to the LLM provider)
- `tool_calls []ToolCall` in `InvokeResponse` (LLM's structured tool use requests)
- Multi-part content in messages: `tool_use` blocks in assistant turns, `tool_result` blocks in user turns

The gateway extension is a separate task, phased after the runtime loop is running without tool support. The runtime loop initially works with text-only LLM responses (no tool calling) until the gateway extension is complete.

---

## Internal Architecture

```
internal/runtime/
  runtime.go         — Runtime struct, Config, Start, heartbeat goroutine
  server.go          — HTTP server; /invoke and /health handlers; active invocations sync.Map
  agent/
    types.go         — InvokeRequest, InvokeResponse, CallbackPayload, ToolServerSpec, ModelSpec, Metrics
    loop.go          — Loop struct; Run(ctx, req) executes the reasoning loop
    mcp/
      client.go      — Client struct; DiscoverTools(url, allowlist), CallTool(url, name, args)
```

### Key Types

```go
// InvokeRequest is the payload the orchestrator sends to POST /invoke.
type InvokeRequest struct {
    RunID       string            `json:"run_id"`
    StepID      string            `json:"step_id"`
    AgentName   string            `json:"agent_name"`
    System      string            `json:"system"`
    MaxTurns    int               `json:"max_turns"`
    Model       ModelSpec         `json:"model"`
    Input       map[string]any    `json:"input"`
    ToolServers []ToolServerSpec  `json:"tool_servers"`
    CallbackURL string            `json:"callback_url"`
}

type ModelSpec struct {
    Group     string `json:"group"`
    MaxTokens int    `json:"max_tokens"`
}

type ToolServerSpec struct {
    Name      string   `json:"name"`
    URL       string   `json:"url"`
    Allowlist []string `json:"allowlist"`
}

// CallbackPayload is the result POSTed to callback_url.
type CallbackPayload struct {
    RunID   string             `json:"run_id"`
    StepID  string             `json:"step_id"`
    Status  string             `json:"status"` // "ok" | "failed"
    Output  map[string]any     `json:"output"`
    Error   string             `json:"error"`
    Metrics Metrics            `json:"metrics"`
}

type Metrics struct {
    ModelResolved string  `json:"model_resolved"`
    TokensIn      int     `json:"tokens_in"`
    TokensOut     int     `json:"tokens_out"`
    CostUSD       float64 `json:"cost_usd"`
    DurationMS    int64   `json:"duration_ms"`
    ToolCalls     int     `json:"tool_calls"`
}
```

---

## Out of Scope (v1)

- Streaming LLM responses
- Sub-agent invocations (agent calling another agent)
- Per-invocation timeout (max_turns is the only bound)
- Runtime-side Air-Lock (orchestrator handles this after callback)
- Tool result caching
