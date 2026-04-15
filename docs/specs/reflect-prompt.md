# Spec: Agent Reflect Field
## Implementation Spec — April 2026

---

## Overview

Add a `reflect` field to agent files. When present, the Agent Runtime performs a second reasoning pass after the initial output draft, before Air-Lock runs. The agent evaluates its own draft and either revises it or returns it unchanged. The reflected output — not the draft — is what Air-Lock validates.

This implements the reflection agentic design pattern at the infrastructure level. Reflection criteria are defined by the agent author in the `reflect` prompt, not hardcoded in the runtime. Each agent reflects on what matters for its domain.

---

## New Field: `reflect`

**File:** `agents/*.agent.yaml`  
**Type:** string (multiline prompt)  
**Required:** no

```yaml
system: |
  You are a triage agent...

reflect: |
  Review your classification above.
  1. Is the category unambiguous given the input?
  2. Is your confidence score justified?
  If you would classify differently on reflection, revise your output completely.
  If confident, return the same output unchanged.
```

---

## Reasoning Loop — With Reflect

Without `reflect`, the Agent Runtime reasoning loop is:

```
system prompt → reasoning turns (tool calls, LLM calls) → draft output → Air-Lock
```

With `reflect`, the loop becomes:

```
system prompt → reasoning turns → draft output →
reflect prompt + draft output injected as context →
single reflect turn → reflected output → Air-Lock
```

The reflect turn is a single LLM call. It is not a loop. There is no reflection-on-reflection.

The Agent Runtime constructs the reflect turn messages as follows:

```
system  → <reflect prompt text>
user    → <original input envelope (same JSON as initial reasoning turn)>
user    → <draft output as JSON>
```

The reflect turn produces a complete replacement output object in the same schema as the initial output. Air-Lock runs on the reflected output only — the draft is discarded.

---

## Trigger Logic

The reflect turn does not always run. The trigger depends on whether `ktsu_confidence` is declared in the agent's output schema and whether a `confidence_threshold` is set on the pipeline step.

| `ktsu_confidence` in schema | `confidence_threshold` on step | Reflect runs when |
|---|---|---|
| No | — | Always |
| Yes | Not set | Always |
| Yes | Set | Draft confidence < threshold only |

The only case where reflection is **skipped** is when `ktsu_confidence` is declared, a `confidence_threshold` is set on the step, and the draft output's confidence meets or exceeds that threshold.

This creates a natural incentive: agents that declare `ktsu_confidence` and configure a threshold skip the reflect turn — and its token cost — on high-confidence outputs. Reflection is the safe default; skipping it must be earned.

---

## Relationship to Existing Retry Logic

Reflection and retry are independent mechanisms that operate in sequence:

```
draft output →
  [reflect turn if triggered] →
    reflected output →
      Air-Lock →
        [retry if Air-Lock fails and retries remain]
```

- If the reflected output fails Air-Lock and `retry.max > 0`, normal retry behavior applies from that point. The retry reinjects the Air-Lock validation error into the reasoning loop and produces a new draft, which then goes through reflection again if the trigger condition still applies.
- Reflection does not consume a retry slot. It is not a retry — it is a pre-Air-Lock quality pass.
- If the draft output fails reserved field processing (e.g. `ktsu_injection_attempt: true`) before reflection is reached, the run fails immediately. Reflection never runs on a draft that has already triggered a fatal reserved field condition.

---

## Reserved Field Interactions

Reserved field processing order with `reflect` inserted:

```
1. ktsu_injection_attempt   (on draft) → if true: fail run immediately
2. ktsu_untrusted_content   (on draft) → if true: fail step
3. ktsu_low_quality         (on draft) → if true: fail step
4. ktsu_needs_human         (on draft) → if true: fail run with needs_human_review
5. [reflect turn runs here if triggered — produces reflected output]
6. ktsu_injection_attempt   (on reflected output) → if true: fail run immediately
7. ktsu_untrusted_content   (on reflected output) → if true: fail step
8. ktsu_low_quality         (on reflected output) → if true: fail step
9. ktsu_needs_human         (on reflected output) → if true: fail run
10. ktsu_confidence          (on reflected output) → check threshold
11. ktsu_skip_reason         (on reflected output) → mark skipped if set
12. ktsu_flags               (on reflected output) → record in metrics
13. ktsu_rationale           (on reflected output) → record in metrics
14. → Air-Lock runs on reflected output
```

Fatal reserved field conditions on the **draft** (steps 1–4) abort before reflection runs — there is no point reflecting on output the agent has flagged as dangerous or unreliable. Fatal conditions on the **reflected output** (steps 6–9) are evaluated again because the reflect turn is a full LLM call that can produce new content.

---

## Metrics and Observability

The reflect turn is an additional LLM call routed through the LLM Gateway using the same group as the initial reasoning turns. Its token and cost usage are attributed to the same step and rolled into step-level metrics exactly as any other LLM call in the reasoning loop.

A new boolean column `reflected` is added to the `steps` table:

| Column | Type | Description |
|---|---|---|
| `reflected` | boolean | Whether the reflect turn was triggered on this step. Null for non-agent steps. |

This makes it queryable: teams can correlate reflection rate against output quality metrics across runs.

The reflect turn does **not** appear as a separate entry in `skill_calls`. It is part of the agent step's LLM call budget, not a tool call.

---

## Boot Validation

The following are boot errors:

- `reflect` declared on a non-agent step (transform, webhook) — `reflect` is only valid on agent steps
- `reflect` is an empty string — a reflect prompt that does nothing is a misconfiguration

The following is a warning (not an error):

- `reflect` declared on an agent with `max_turns: 1` — the agent has no reasoning turns to reflect on; the reflect turn will operate only on the single-turn output. Not invalid, but likely unintentional.

---

## YAML Spec Changes

### agent.md — Fields Table Addition

| Field | Type | Required | Description |
|---|---|---|---|
| `reflect` | string | no | Reflection prompt. Injected after the initial output draft, before Air-Lock. Triggers a single additional LLM turn where the agent evaluates and optionally revises its output. See Trigger Logic for when the reflect turn runs. |

### agent.md — Annotated Example Addition

```yaml
system: |
  You are a triage agent. The full pipeline envelope is provided as JSON
  in the first user message. Reference upstream step outputs as
  <step-id>.<field>. Workflow input fields are under input.<field>.

reflect: |
  Review your classification above.
  1. Is the category unambiguous given the input?
  2. Is your confidence score justified by the evidence in the message?
  If you would classify differently on reflection, return a complete
  revised output. If confident in your original output, return it unchanged.
```

### kimitsu-invariants.md — New Invariant

> **The reflect turn is a single pass.** When `reflect` is declared on an agent, the Agent Runtime performs one additional LLM call after the initial output draft. There is no loop, no reflection-on-reflection, and no further reflection on retried outputs beyond what the trigger logic dictates. The reflected output is a complete replacement — no merging with the draft.

---

## Implementation Notes for Claude Code

### Agent Runtime Changes

1. After the reasoning loop produces a draft output, check whether the reflect turn should be triggered (see Trigger Logic above).
2. If triggered: extract `ktsu_confidence` from the draft (if present), compare against step's `confidence_threshold` (if set), proceed or skip accordingly.
3. Run fatal reserved field checks on the draft (steps 1–4 in Reserved Field Interactions) before entering the reflect turn. Abort if any fire.
4. Construct reflect turn messages: system = reflect prompt, user[0] = original input envelope, user[1] = draft output as JSON.
5. Make a single LLM Gateway call using the same `run_id`, `step_id`, and `group` as the main reasoning turns.
6. The response is the reflected output. Replace the draft entirely.
7. Continue with reserved field processing on the reflected output, then Air-Lock.

### Orchestrator Changes

1. Pass `reflect` prompt string and `confidence_threshold` (from the pipeline step) in the agent invocation payload to the Agent Runtime.
2. Add `reflected` boolean column to the `steps` table migration.
3. Accept `reflected: true/false` in the step result callback from the Agent Runtime and write it to the `steps` row.

### Boot Validation Changes

1. During step 8 (validate-io), check for `reflect` on non-agent steps — boot error.
2. Check for empty `reflect` string — boot error.
3. Check for `reflect` + `max_turns: 1` — warning only.

---

*Spec authored April 2026*
