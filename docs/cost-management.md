# Cost Management

Kimitsu gives you several levers for controlling LLM spend. This page explains how costs accumulate, what fields are available, and which controls are enforced today versus planned.

---

## How Costs Accumulate

Every agent step makes at least one LLM call per turn, up to `max_turns`. Three factors multiply that baseline:

- **Fanout** — a `for_each` block runs the agent once per element in the input array, concurrently. Ten items means up to ten parallel agent invocations, each with their own turn budget.
- **Reflection** — if the Air-Lock rejects an agent's output (schema validation failure), the orchestrator feeds the error back into the agent's loop for a retry. Each reflection attempt is one additional LLM call.
- **Model choice** — frontier models cost significantly more per token than standard or economy groups. The gateway resolves `model.group` names to concrete models; choosing a cheaper group reduces per-call cost without changing pipeline logic.

---

## The `cost_budget_usd` Field

`model_policy` is a top-level block on a workflow that sets runtime constraints on how models are used. It accepts a `cost_budget_usd` field declaring the maximum dollar spend intended for a single run of that workflow:

```yaml
model_policy:
  cost_budget_usd: 0.50        # intended ceiling for this workflow run
  force_group: standard        # optional: override model group for all steps
  timeout_s: 120               # optional: per-step timeout in seconds
```

`cost_budget_usd` can also be set on an agent definition file to scope the budget to a single agent rather than the whole workflow.

### Current Status — Not Enforced

**`cost_budget_usd` is parsed and stored but not yet enforced as a circuit breaker.** The orchestrator reads the value and includes it in the run envelope, but it does not halt execution when accumulated spend crosses the threshold. Tracking for enforcement is in [GitHub issue #12](https://github.com/kimitsu-ai/ktsu/issues/12).

Do not rely on this field to prevent runaway spend in production. Treat it as a declaration of intent that will become a hard limit once #12 is resolved.

---

## Cost Controls Available Today

These controls are fully enforced in the current release:

**`max_turns` on agents** — limits the number of LLM calls an agent can make in a single invocation. Set this in the agent YAML to cap how long a single step can run.

```yaml
# agents/summarize.agent.yaml
max_turns: 3
```

**`max_items` on fanout** — limits how many elements a `for_each` loop will process, regardless of the input array length.

**`max_failures` on fanout** — stops a fanout loop after a given number of per-item failures, preventing a bad batch from consuming the full iteration budget.

```yaml
- id: summarize
  agent: "./agents/summarize.agent.yaml"
  for_each:
    items: "{{ step.filter.output }}"
    as: item
    max_items: 20        # process at most 20 articles
    max_failures: 3      # stop after 3 agent failures
```

**Model group selection via gateway** — define economy, standard, and frontier model groups in the gateway config and use `model.group` on individual steps to route cheaper steps to smaller models.

---

## Summary

| Control | Enforced today? | Scope |
|---|---|---|
| `max_turns` | Yes | Per agent invocation |
| `max_items` | Yes | Per fanout loop |
| `max_failures` | Yes | Per fanout loop |
| Model group selection | Yes | Per step |
| `cost_budget_usd` | No (see [#12](https://github.com/kimitsu-ai/ktsu/issues/12)) | Per workflow or agent |

---

*Revised April 2026*
