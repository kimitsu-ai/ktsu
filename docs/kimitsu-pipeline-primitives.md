# Kimitsu — Pipeline Primitives

## Architecture & Design Reference — v4

---

## The Four Primitives

Every step in a Kimitsu pipeline is exactly one of:

1.  **Transform**: Deterministic data shaping (JMESPath).
2.  **Agent**: Reasoning and synthesis (LLM).
3.  **Webhook**: Outbound integration (HTTP POST).
4.  **Workflow**: Sub-pipeline composition.

---

## Variable Substitution Syntax

Kimitsu uses a standardized template syntax across all primitives.

- **`{{ env.NAME }}`**: Injected environment variables. Available **only in workflows with `visibility: root`**.
- **`{{ params.NAME }}`**: Local parameters passed to the step or workflow.
- **`{{ step.ID.FIELD }}`**: Typed output from an upstream step.

---

## Agent Steps

Agents are the central intelligence of Kimitsu. They are the only primitive allowed to make LLM calls.

```yaml
- id: triage
  agent: "./agents/triage.agent.yaml" # identity
  params:                             # flat map of inputs for the agent
    message: "{{ params.message }}"
    team: "billing"
  depends_on: [other_step]            # explicit flow dependency
  model:                              # runtime overrides (optional)
    group: frontier
    max_tokens: 1024
```

### Prompt Separation

- `prompt.system`: **Static only**. No `{{ }}` templates.
- `prompt.user`: **Dynamic**. Supports full `{{ }}` interpolation.

---

## Transform Steps

Transforms are deterministic and execute directly in the orchestrator.

```yaml
- id: cleanup
  transform:
    ops:
      - map: { expr: "{ title: title, raw: step.parse.text }" } # JMESPath op
      - filter: { expr: "confidence > `0.5`" }
```

---

## Webhook Steps

Webhooks integrate Kimitsu with the outside world.

```yaml
- id: alert
  webhook:
    url: "{{ env.SLACK_URL }}"       # Root secret resolution
    method: POST
    body:
      status: "critical"
      data: "{{ step.triage.results }}"
```

---

## Workflow Steps

Workflows allow for recursive nesting and modular pipelines.

```yaml
- id: sub-process
  workflow: "./sub/validator.workflow.yaml"
  input:                              # maps parent data to child params
    source: "{{ step.parse.output }}"
  webhooks: execute                   # opt-in to child webhooks
```

---

*Revised April 2026*
