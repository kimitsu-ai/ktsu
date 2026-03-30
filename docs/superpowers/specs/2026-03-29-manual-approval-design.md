# Manual Approval for Dangerous Tool Calls

**Date:** 2026-03-29
**Status:** Draft

---

## Context

Operators need the ability to require human (or automated) approval before an agent executes a tool call that has destructive, irreversible, or high-risk side effects. Without this, any tool on the allowlist fires immediately when the agent decides to call it — there's no gate.

This feature adds a per-tool `require_approval` policy to the allowlist configuration. When an agent attempts to call an approval-gated tool, the runtime checkpoints its full message context and reports back to the orchestrator with a `pending_approval` status. The orchestrator stores the suspended state, optionally fires a webhook notification, and waits for an external decision before re-invoking the runtime to resume.

As a companion improvement, the runtime now always sends its full message context in the callback payload — not only during approval checkpoints. This enables operators to inspect exactly what the LLM saw and said during any step, which is independently useful for debugging.

---

## Architecture

The system follows a **suspend/resume checkpoint pattern**. The runtime remains stateless and load-balancer-safe. The orchestrator owns all approval state.

```
Normal completion:
  invoke → loop → callback(ok, messages)

Approval checkpoint:
  invoke → loop → callback(pending_approval, messages, pending_tool_call)
                    ↓
          orchestrator stores state
          orchestrator fires approval webhook (optional)
                    ↓
          [external decision via API or webhook callback]
                    ↓
  re-invoke(messages + decision injected) → loop → callback(ok, messages)
```

The agent loop has no concept of "waiting" — it either completes a turn or terminates at a checkpoint. Any runtime instance can resume a suspended step.

---

## Configuration Changes

### `AccessConfig` — mixed allowlist entries

Plain strings remain valid (backward compatible). Approval-required tools use an object form:

```yaml
access:
  allowlist:
    - "read-*"                           # plain string, no change
    - name: "delete-*"
      require_approval:
        on_reject: fail                  # fail | recover
        timeout: 30m                     # Go duration; omit = no timeout
        timeout_behavior: reject         # fail | reject
    - name: "mutate-records"
      require_approval:
        on_reject: recover
        timeout: 1h
        timeout_behavior: fail
```

**`on_reject` values:**
- `fail` — step and run are marked failed immediately on rejection
- `recover` — orchestrator injects a synthetic tool result into the message context and re-invokes the runtime so the agent can reason about the rejection and try an alternative

**`timeout` / `timeout_behavior`:**
- `timeout` is a Go duration string (`30s`, `5m`, `1h`). Omit or set to `0` for no timeout.
- `timeout_behavior: fail` — treat timeout as a hard failure
- `timeout_behavior: reject` — treat timeout as a rejection (applies `on_reject` policy)

### Go types

```go
type AccessConfig struct {
    Allowlist []ToolAccess
}

type ToolAccess struct {
    Name            string          // exact, "prefix-*", or "*"
    RequireApproval *ApprovalPolicy // nil = no approval required
}

type ApprovalPolicy struct {
    OnReject        string        // "fail" | "recover"
    Timeout         time.Duration // 0 = no timeout
    TimeoutBehavior string        // "fail" | "reject"
}
```

YAML unmarshaling handles the mixed list: a plain string unmarshals as `ToolAccess{Name: "...", RequireApproval: nil}`.

### Boot-time validation additions

- `on_reject` must be `"fail"` or `"recover"`
- `timeout_behavior` must be `"fail"` or `"reject"`
- Patterns in `require_approval` must be covered by the allowlist
- Wildcard rules apply identically to allowlist patterns

---

## Runtime Changes

### `InvokeRequest` additions

```go
type InvokeRequest struct {
    // ... existing fields unchanged ...
    Messages          []Message // populated on resume; nil = fresh start
    ApprovedToolCalls []string  // tool_use IDs pre-approved by orchestrator
    IsResume          bool      // true when re-invoking after approval; used for metrics accumulation
}
```

### Loop logic (`internal/runtime/agent/loop.go`)

**On resume** (when `Messages` is non-nil):
- Skip system prompt construction and fresh tool discovery setup
- Restore message history from `Messages`
- Continue the loop from the last message

**Before each tool call:**
1. Check if the tool name matches any `ApprovalPolicy` pattern in the agent's config
2. If it requires approval AND the tool_use ID is in `ApprovedToolCalls` → call normally
3. If it requires approval AND it is NOT pre-approved → checkpoint and exit:
   - Serialize current `messages` (including the pending tool_use as the last assistant message)
   - POST to `callback_url` with `status: "pending_approval"` and `PendingApproval` payload
   - Exit the loop (the runtime's job is done for this leg)

### `CallbackPayload` additions

```go
type CallbackPayload struct {
    // ... existing fields unchanged ...
    Messages        []Message       // full conversation context, always populated
    PendingApproval *PendingApproval // non-nil only when status == "pending_approval"
}

type PendingApproval struct {
    ToolName  string
    ToolUseID string
    Arguments map[string]any
}
```

`Messages` is always set — for `ok`, `failed`, and `pending_approval` callbacks. This enables full message context inspection for every completed step regardless of approvals.

---

## Orchestrator Changes

### State store additions

**`Step` record** — new field:
- `Messages []Message` — stored on every callback (not just approvals)

**New `Approval` record:**
```go
type Approval struct {
    ApprovalID string
    RunID      string
    StepID     string
    ToolName   string
    ToolUseID  string
    Arguments  map[string]any
    Status     string    // "pending" | "approved" | "rejected"
    Decision   string    // "approve" | "reject"
    Reason     string    // optional operator-provided reason
    CreatedAt  time.Time
    ResolvedAt time.Time
}
```

### Callback handler additions

On `pending_approval` status:
1. Store `Messages` on the step (same as `ok`/`failed`)
2. Store partial `Metrics` on the step
3. Create an `Approval` record with `status: "pending"`
4. Start timeout goroutine if `Timeout > 0`
5. Do NOT signal the dispatcher channel — step is suspended

On `ok` or `failed` for a **resumed step** (step already has stored metrics):
- **Add** new metrics to existing step metrics rather than overwrite:
  ```
  step.Metrics.TokensIn    += new.TokensIn
  step.Metrics.TokensOut   += new.TokensOut
  step.Metrics.CostUSD     += new.CostUSD
  step.Metrics.DurationMS  += new.DurationMS
  step.Metrics.LLMCalls    += new.LLMCalls
  step.Metrics.ToolCalls   += new.ToolCalls
  ```

The orchestrator detects a resume via the `IsResume` flag on the `InvokeRequest` it built — not by inspecting stored state. This avoids ambiguity if a step somehow receives a callback before metrics are populated.

### Decision logic

**Approve:**
1. Update `Approval` record to `status: "approved"`
2. Build new `InvokeRequest` with stored `Messages` and `ApprovedToolCalls: [tool_use_id]`
3. Re-dispatch to runtime (same `step_id`, any runtime instance)

**Reject + `on_reject: recover`:**
1. Update `Approval` record to `status: "rejected"`
2. Append synthetic tool result to stored messages:
   ```json
   {"role": "tool", "tool_use_id": "<id>", "content": "Tool call rejected by operator: <reason>"}
   ```
3. Re-dispatch with extended message context (no `ApprovedToolCalls`)

**Reject + `on_reject: fail`:**
1. Update `Approval` record to `status: "rejected"`
2. Mark step and run as failed with error: `"tool_call_rejected: <tool_name>: <reason>"`
3. No re-invoke

**Timeout fires:**
- Apply `TimeoutBehavior`: treat as rejection (using `on_reject` policy) or fail directly

### New API endpoints

```
GET  /runs/{run_id}/steps/{step_id}/approval
     Returns: Approval record (or 404 if no approval on this step)

POST /runs/{run_id}/steps/{step_id}/approval/decide
     Body: { "decision": "approve" | "reject", "reason": "optional string" }
     Returns: 200 OK, 404 if no pending approval, 409 if already resolved

GET  /approvals?status=pending
     Returns: paginated list of all approvals across all runs, filterable by status
```

---

## Approval Webhook Output Step

Operators configure approval notifications in the workflow using a webhook step with `on: approval`. This fires when a dependent step enters `pending_approval` — independent of the normal pipeline flow.

```yaml
steps:
  - id: process-records
    type: agent
    agent: data-processor

  - id: notify-approver
    type: webhook
    on: approval                      # fires on pending_approval (not completion)
    depends_on: [process-records]
    url: "https://myapp.com/approvals/incoming"
    body:
      run_id: "{{ run_id }}"
      step_id: "{{ step_id }}"
      tool_name: "{{ pending_approval.tool_name }}"
      arguments: "{{ pending_approval.arguments }}"
      approve_url: "{{ approval_url }}"
      reject_url: "{{ rejection_url }}"
```

**Template variables injected by orchestrator for `on: approval` webhooks:**
- `{{ approval_url }}` — `POST /runs/{run_id}/steps/{step_id}/approval/decide` with `{"decision":"approve"}`
- `{{ rejection_url }}` — same endpoint with `{"decision":"reject"}`
- `{{ pending_approval.tool_name }}` — the tool that triggered the gate
- `{{ pending_approval.arguments }}` — the arguments the agent intended to pass

`on: approval` webhook steps are fire-and-forget. They do not block the run and webhook delivery failures are logged but do not affect approval state. The run stays in `pending_approval` until the decide endpoint is called.

Both mechanisms are available simultaneously: the webhook for push notification, the REST endpoints for pull/query.

---

## Files to Modify

| File | Change |
|------|--------|
| `internal/config/types.go` | `AccessConfig`, `ToolAccess`, `ApprovalPolicy` types; YAML unmarshaling for mixed allowlist |
| `internal/config/loader.go` | Boot validation for new approval policy fields |
| `internal/runtime/agent/types.go` | Add `Messages`, `ApprovedToolCalls` to `InvokeRequest`; add `Messages`, `PendingApproval` to `CallbackPayload` |
| `internal/runtime/agent/loop.go` | Resume-from-context on startup; approval check before tool calls; checkpoint logic |
| `internal/orchestrator/server.go` | Handle `pending_approval` callback status; decision handler; timeout goroutine |
| `internal/orchestrator/runner/runner.go` | Cumulative metrics merge on resume; `on: approval` webhook step firing |
| `internal/orchestrator/state/store.go` | `Messages` on `Step`; new `Approval` record and CRUD operations |
| `internal/orchestrator/state/mem_store.go` | In-memory implementation of `Approval` operations |
| `pkg/types/run.go` | `Approval` type definition |
| `docs/kimitsu-reserved-outputs.md` | No change (approval is not a reserved output field) |
| `docs/kimitsu-tool-servers.md` | Document `require_approval` block in allowlist config |
| `docs/kimitsu-configuration.md` | Document new boot validation rules |
| `docs/kimitsu-runtime.md` | Document `pending_approval` callback status and resume flow |

---

## Verification

1. **Boot validation** — add a tool with `require_approval` and valid values: `ktsu validate` passes. Add invalid `on_reject: "maybe"`: validate fails with a clear error naming the field.
2. **Happy path** — invoke a workflow containing an approval-gated tool. Confirm `GET /runs/{id}` shows `pending_approval`. Call `POST .../decide` with `approve`. Confirm step completes and cumulative metrics across both legs are correct (tokens, cost, duration summed).
3. **Approval webhook** — configure `on: approval` webhook step. Confirm it fires when step enters `pending_approval` and includes `approve_url` / `reject_url` in the payload.
4. **Reject + recover** — call `/decide` with `reject`. Confirm the agent receives a synthetic tool result and continues reasoning to a valid final output.
5. **Reject + fail** — with `on_reject: fail`, call `/decide` with `reject`. Confirm step and run are marked failed.
6. **Timeout** — set `timeout: 5s`, wait for it to expire. Confirm `timeout_behavior` is applied.
7. **Message context debugging** — after any completed step (no approval), confirm `GET /runs/{id}` includes full message history on the step.
8. **Cumulative metrics** — verify tokens, cost, LLM calls, tool calls, and duration are summed across both legs of a resumed step.
9. **Load balancer safety** — run two runtime instances, trigger an approval, verify the resume lands on a different instance and completes successfully.
10. **List pending approvals** — confirm `GET /approvals?status=pending` returns all open approvals across all active runs.
