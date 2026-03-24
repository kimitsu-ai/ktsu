# Agent Runtime Implementation Plan

**Date:** 2026-03-23
**Spec:** `docs/superpowers/specs/2026-03-23-agent-runtime-design.md`

---

## Overview

Implement the Agent Runtime reasoning loop in phases. The runtime skeleton (`internal/runtime/`) already exists with a stub `/invoke` handler. Phases 1–5 build the runtime. Phase 6 wires the orchestrator to dispatch to it. Phase 7 extends the gateway for tool support. Phase 8 adds tests throughout.

---

## Phase 1 — Types

Define all shared types before writing logic.

- [ ] **Task 1.1** — Create `internal/runtime/agent/types.go`
  - `InvokeRequest` struct (run_id, step_id, agent_name, system, max_turns, model, input, tool_servers, callback_url)
  - `ModelSpec` struct (group, max_tokens)
  - `ToolServerSpec` struct (name, url, allowlist []string)
  - `CallbackPayload` struct (run_id, step_id, status, output, error, metrics)
  - `Metrics` struct (model_resolved, tokens_in, tokens_out, cost_usd, duration_ms, tool_calls)

---

## Phase 2 — MCP Client

Implement the JSON-RPC-over-HTTP MCP client used by the reasoning loop to discover and call tools.

- [ ] **Task 2.1** — Create `internal/runtime/agent/mcp/client.go`
  - `ToolDefinition` struct (name, description, inputSchema)
  - `ToolCallResult` struct (content []ContentBlock)
  - `ContentBlock` struct (type, text)
  - `Client` struct with `http.Client`

- [ ] **Task 2.2** — Implement `DiscoverTools(ctx, url string, allowlist []string) ([]ToolDefinition, error)`
  - POST `{"jsonrpc":"2.0","method":"tools/list","id":1}` to `url`
  - Parse `result.tools` array
  - Filter: keep only tools whose `name` appears in `allowlist`
  - Return pruned slice

- [ ] **Task 2.3** — Implement `CallTool(ctx, url, name string, arguments map[string]any) (ToolCallResult, error)`
  - Validate `name` is in `allowlist` (second enforcement layer); return `tool_not_permitted` error if not
  - POST `{"jsonrpc":"2.0","method":"tools/call","params":{"name":name,"arguments":arguments},"id":N}` to `url`
  - Parse `result.content` array
  - Return `ToolCallResult`

---

## Phase 3 — Reasoning Loop

Core logic: build messages, call the gateway, handle tool use, accumulate metrics.

- [ ] **Task 3.1** — Create `internal/runtime/agent/loop.go`
  - `Loop` struct with fields: `gatewayURL string`, `mcpClient *mcp.Client`
  - `Run(ctx context.Context, req InvokeRequest) (CallbackPayload, error)`

- [ ] **Task 3.2** — Tool discovery phase (inside `Run`)
  - For each `ToolServerSpec` in `req.ToolServers`: call `mcp.DiscoverTools`
  - Build `toolsByServer map[string][]mcp.ToolDefinition` (keyed by server URL)
  - Build flat `allTools []ToolDefinition` for LLM context

- [ ] **Task 3.3** — Initial message construction
  - `messages[0] = {role:"system", content: req.System}`
  - `messages[1] = {role:"user", content: JSON-stringify(req.Input)}`

- [ ] **Task 3.4** — Turn loop
  - `for turn := 1; turn <= req.MaxTurns; turn++`
  - On `turn == req.MaxTurns`: append forced-conclusion user message
  - POST `{gateway_url}/invoke` with `{run_id, step_id, group, max_tokens, messages, tools}`
  - Accumulate `tokens_in`, `tokens_out`, `cost_usd` from gateway response
  - Record `model_resolved` from first successful gateway response

- [ ] **Task 3.5** — Tool call dispatch (inside turn loop)
  - If gateway response has `tool_calls`:
    - For each call: resolve which server owns the tool (match name against per-server allowlists)
    - Call `mcp.CallTool`; increment `tool_calls` counter
    - Append `tool_use` block to messages (role: assistant, structured content)
    - Append `tool_result` block to messages (role: user, tool call ID + result)
    - Continue loop
  - If no tool calls: parse `response.content` as JSON → `output`; break

- [ ] **Task 3.6** — Error handling
  - `tool_not_permitted`: immediately return failed `CallbackPayload`
  - MCP call failure: return failed `CallbackPayload`
  - Gateway error: return failed `CallbackPayload` (propagate gateway error message)
  - `max_turns_exceeded` (still tool_calls after forced-conclusion turn): return failed `CallbackPayload`
  - JSON parse failure on final content: return failed `CallbackPayload`

- [ ] **Task 3.7** — Build and return `CallbackPayload`
  - `status = "ok"`, `output = parsed JSON`, `metrics` filled from accumulators
  - `duration_ms` = wall-clock time from `Run` entry to return

---

## Phase 4 — Server: `/invoke` Handler

Wire the loop into the HTTP server skeleton.

- [ ] **Task 4.1** — Update `internal/runtime/server.go`
  - Add `activeInvocations sync.Map` to `server` struct (key: `"run_id/step_id"`, value: `struct{}`)
  - Inject `loop *agent.Loop` into `server` struct
  - Update `newServer` to accept and store a `*agent.Loop`

- [ ] **Task 4.2** — Implement `handleInvoke`
  - Decode `InvokeRequest` from request body; return 400 on parse error
  - Register key in `activeInvocations`
  - Return 202 `{"run_id":..., "step_id":..., "status":"accepted"}`
  - Launch goroutine: call `loop.Run(ctx, req)`, then POST callback, then delete key from `activeInvocations`

- [ ] **Task 4.3** — Implement `postCallback(ctx, payload CallbackPayload) error`
  - Marshal payload to JSON
  - POST to `payload.CallbackURL` with `Content-Type: application/json`
  - Log error if callback POST fails (do not panic)

- [ ] **Task 4.4** — Update `internal/runtime/runtime.go`
  - In `New(cfg Config)`: create `mcp.Client`, create `agent.Loop`, pass to `newServer`

---

## Phase 5 — Heartbeat

Background goroutine that reports active invocations to the orchestrator every 5 seconds.

- [ ] **Task 5.1** — Update `internal/runtime/runtime.go`
  - In `Start(ctx)`: launch `go r.heartbeatLoop(ctx, srv.activeInvocations)`
  - `heartbeatLoop`: `ticker := time.NewTicker(5 * time.Second)`; on each tick, collect keys from `activeInvocations`, POST to `{cfg.OrchestratorURL}/heartbeat`
  - Heartbeat payload: `{"runtime_id": "rt-1", "active": [{"run_id":..., "step_id":...}]}`
  - Stop on context cancellation

---

## Phase 6 — Orchestrator Integration

Add the completion callback endpoint and replace the stubbed agent dispatch.

- [ ] **Task 6.1** — Add `POST /runs/{run_id}/steps/{step_id}/complete` to `internal/orchestrator/server.go`
  - Decode `CallbackPayload`
  - Retrieve the in-flight `*types.Step` from the state store by `(run_id, step_id)`
  - Call `processReservedFields(payload.Output, step.ConfidenceThreshold)` (already in `runner.go`)
  - Run `airlock.Validate(cleanOutput, schema, reservedFields)`
  - Update step: `status`, `output`, `error`, `completed_at`, metrics fields
  - Return 200

- [ ] **Task 6.2** — Implement `runner.executeAgent` in `internal/orchestrator/runner/runner.go`
  - Replace `case types.StepTypeAgent: rawOutput, execErr = map[string]interface{}{"stubbed": true}, nil`
  - Build `agent.InvokeRequest` from the step config, assembled inputs, and runner config
  - Construct `callback_url` as `{orchestrator_url}/runs/{run_id}/steps/{step_id}/complete`
  - POST to `{runtime_url}/invoke`; verify 202 response
  - Return nil (result arrives via callback asynchronously)

  Note: The runner's current synchronous execution model needs to become async for agent steps. The step is created as `running`, the runner moves on, and the callback handler completes it. This requires tracking in-flight agent steps differently from synchronous steps.

---

## Phase 7 — Gateway Extension (Tool Support)

Extend the LLM Gateway to support tool definitions and structured content blocks.

- [ ] **Task 7.1** — Update `internal/gateway/providers/provider.go`
  - Add `Tools []ToolDefinition` to `InvokeRequest`
  - Add `ToolCalls []ToolCall` to `InvokeResponse`
  - Define `ToolDefinition` struct (name, description, inputSchema)
  - Define `ToolCall` struct (id, name, arguments map[string]any)
  - Update `Message.Content` to `interface{}` (supports string or []ContentBlock) — or add `ContentBlocks []ContentBlock` alongside `Content string`

- [ ] **Task 7.2** — Update `internal/gateway/providers/anthropic/provider.go`
  - Map `InvokeRequest.Tools` to Anthropic `tools` array in the request body
  - Parse Anthropic `tool_use` content blocks in the response → `InvokeResponse.ToolCalls`
  - Support `tool_result` content blocks in user messages

- [ ] **Task 7.3** — Update `internal/gateway/providers/openai/provider.go`
  - Map `InvokeRequest.Tools` to OpenAI `tools` array (function calling format)
  - Parse OpenAI `tool_calls` in the response → `InvokeResponse.ToolCalls`
  - Support `tool` role messages (tool call results)

- [ ] **Task 7.4** — Update `internal/gateway/dispatcher.go`
  - Pass `Tools` through from `DispatchRequest` to the provider `InvokeRequest`
  - Pass `ToolCalls` through from `InvokeResponse` back to caller

---

## Phase 8 — Tests

- [ ] **Task 8.1** — MCP client tests (`internal/runtime/agent/mcp/client_test.go`)
  - `TestDiscoverTools_filtersAllowlist`: httptest server returns tool list; verify only allowlisted tools returned
  - `TestDiscoverTools_serverError`: httptest returns 500; verify error propagated
  - `TestCallTool_success`: httptest returns result; verify correct JSON-RPC request sent
  - `TestCallTool_notPermitted`: tool name not in allowlist; verify `tool_not_permitted` error before HTTP call

- [ ] **Task 8.2** — Reasoning loop tests (`internal/runtime/agent/loop_test.go`)
  - `TestLoop_noTools`: fake gateway returns final answer on turn 1; verify output parsed, metrics accumulated
  - `TestLoop_withToolCalls`: fake gateway returns tool calls then final answer; fake MCP server returns results; verify message history built correctly
  - `TestLoop_maxTurnsExceeded`: fake gateway always returns tool calls; verify `max_turns_exceeded` after N turns
  - `TestLoop_forcedConclusion`: verify forced-conclusion message appended on final turn
  - `TestLoop_gatewayError`: fake gateway returns error; verify failed callback payload

- [ ] **Task 8.3** — Server tests (`internal/runtime/server_test.go`)
  - `TestHandleInvoke_returns202`: POST valid request; verify 202 response
  - `TestHandleInvoke_callbackDelivered`: fake loop + httptest callback receiver; verify callback POSTed with correct payload
  - `TestHandleInvoke_activeMapUpdated`: verify key added on accept, removed after callback

- [ ] **Task 8.4** — Gateway extension tests
  - Update existing provider tests to cover tools passthrough
  - `TestAnthropic_toolUseResponse`: mock upstream returns tool_use block; verify ToolCalls in response
  - `TestOpenAI_toolCallResponse`: mock upstream returns tool_calls; verify ToolCalls in response

---

## Critical Files

| File | Role |
|---|---|
| `internal/runtime/runtime.go` | Runtime struct, Start, heartbeat goroutine |
| `internal/runtime/server.go` | /invoke handler, active invocations map, callback POST |
| `internal/runtime/agent/types.go` | All shared types |
| `internal/runtime/agent/loop.go` | Reasoning loop |
| `internal/runtime/agent/mcp/client.go` | MCP JSON-RPC client |
| `internal/orchestrator/server.go` | New completion endpoint |
| `internal/orchestrator/runner/runner.go` | executeAgent replaces stub |
| `internal/gateway/providers/provider.go` | Tool types, extended InvokeRequest/Response |
| `internal/gateway/providers/anthropic/provider.go` | Tool support for Anthropic |
| `internal/gateway/providers/openai/provider.go` | Tool support for OpenAI |
| `internal/gateway/dispatcher.go` | Tool passthrough |

---

## Reuse

- `processReservedFields` — `internal/orchestrator/runner/runner.go:641` — used as-is by the orchestrator completion handler
- `airlock.Validate` — `internal/orchestrator/airlock/airlock.go` — used as-is by the orchestrator completion handler
- `gatewayErrorStatus` pattern — `internal/gateway/server.go` — reference for error taxonomy in loop error handling
- `serve` / graceful shutdown pattern — `internal/gateway/server.go` / `internal/runtime/server.go` — already present in runtime skeleton

---

## Sequencing Notes

- Phases 1–5 are fully independent of the orchestrator and can be developed and tested in isolation.
- Phase 6 depends on phases 1–5 being complete (runtime must accept callbacks before orchestrator dispatches to it).
- Phase 7 (gateway extension) is independent of phases 1–6 and can be developed in parallel. The reasoning loop (phase 3) initially works without tool support — tool_calls will be absent from gateway responses until phase 7 is complete.
- All tests in phase 8 should be written alongside their corresponding implementation phase, not deferred.

---

## Verification

```bash
go test ./internal/runtime/...
go test ./internal/gateway/...
go test ./internal/orchestrator/...
go build ./cmd/kimitsu
```

End-to-end: start orchestrator + runtime + gateway + a stub MCP tool server; POST a workflow trigger to the orchestrator; verify:
1. Orchestrator dispatches to runtime (202 returned)
2. Runtime heartbeats appear in orchestrator logs
3. Callback POSTed on completion
4. Step marked `ok` in envelope (`GET /envelope/{run_id}`)
