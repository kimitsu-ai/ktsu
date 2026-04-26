# Pipeline Primitives

---

## The Four Primitives

Every step in a Kimitsu pipeline is exactly one of:

1.  **Transform**: Deterministic data shaping ([JMESPath](https://jmespath.org)).
2.  **Agent**: Reasoning and synthesis (LLM).
3.  **Webhook**: Outbound integration (HTTP POST).
4.  **Workflow**: Sub-pipeline composition.

See [Variables](./variables.md) for the template syntax and how env vars, params, and secrets are declared and threaded through the pipeline.

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

Transforms are deterministic and execute directly in the orchestrator. See [Transforms](./transforms.md) for all six operations and a complete chained example.

```yaml
- id: cleanup
  transform:
    ops:
      - filter:
          expr: "confidence > `0.5`"          # keep high-confidence items
      - sort:
          field: "confidence"
          order: "desc"                        # highest confidence first
      - map:
          expr: "{title: title, score: confidence}" # project to final shape
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

## Agent Steps — Fanout

Agent steps support a `for_each` block that runs the step once per array element, concurrently. See [Fanout (for_each)](./fanout.md) for syntax, output shape, and a worked example.

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
