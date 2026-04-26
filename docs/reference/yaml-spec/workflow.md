# workflow.yaml

**What it does:** Defines a pipeline — input schema, ordered steps, and model cost policy.

**Filename convention:** `workflows/*.workflow.yaml`

## Annotated Example

```yaml
kind: workflow
name: support-triage            # unique name — used in POST /invoke/{name}
version: "1.2.0"                # semver string
description: "Triage workflow"  # optional
visibility: root                # "root" (public API) | "sub-workflow" (internal only)

params:
  schema:                       # JSON Schema for workflow inputs
    type: object
    required: [message, user_id]
    properties:
      message:    { type: string, description: "Raw support ticket text" }
      user_id:    { type: string, description: "Unique ID of the reporter" }

env:                            # declared environment variables (ROOT ONLY)
  - name: SLACK_WEBHOOK_URL     # env var name
    secret: true                # true = masked in logs and envelope
    description: "Slack URL"    # optional

invoke:                         # authentication for POST /invoke/{name}
  auth:
    header: X-Api-Secret        # header to read
    scheme: raw                 # "raw" | "bearer"
    secret: "{{ env.SECRET }}"  # value expression resolved at request time

pipeline:
  # ── Agent step ────────────────────────────────────────────────────────────
  - id: parse                   # unique step ID
    agent: ktsu/parser@1.0.0    # built-in or local path
    params:                     # flat map of params passed to the agent
      source: "{{ params.message }}" # reference a workflow param
      mode: "high-accuracy"     # literal value
    model:                      # optional overrides
      group: economy
      max_tokens: 512

  # ── Workflow step ─────────────────────────────────────────────────────────
  - id: notify
    workflow: ./sub/slack.yaml  # path to another workflow
    input:                      # maps parent values to sub-workflow params
      channel: "support"
      text: "{{ step.parse.summary }}"
    webhooks: execute           # "execute" | "suppress" (default)

  # ── Transform step ────────────────────────────────────────────────────────
  - id: merge
    depends_on: [parse]         # explicit dependency
    transform:
      ops:
        - map: { expr: "{ id: id, text: body }" } # JMESPath expression
    output:
      schema: { type: array }

  # ── Webhook step ──────────────────────────────────────────────────────────
  - id: post-result
    webhook:
      url: "{{ env.SLACK_WEBHOOK_URL }}" # env reference (root only)
      method: POST
      body:
        result: "{{ step.merge.result }}"
    condition: "step.parse.valid == `true`" # JMESPath condition

model_policy:                   # global cost and timeout settings
  cost_budget_usd: 0.50
  timeout_s: 60
```

## Fields

| Field | Type | Required | Description |
|---|---|---|---|
| `kind` | string | yes | Must be `workflow`. |
| `name` | string | yes | Unique identity. |
| `visibility` | string | no | `"root"` (default) allows direct invocation. `"sub-workflow"` restricts to internal calls. **Note: visibility enforcement is not yet implemented. This field is parsed and reserved for future use — declaring it has no runtime effect today. See [GitHub issue #13](https://github.com/kimitsu-ai/ktsu/issues/13).** |
| `env` | array | no | **ROOT ONLY**. Declares env vars available to the pipeline. |
| `params.schema`| object | yes | JSON Schema for input validation. |
| `pipeline` | array | yes | Ordered list of steps. |
| `pipeline[].id` | string | yes | Unique ID for the step. |
| `pipeline[].agent` | string | no | Agent reference. |
| `pipeline[].workflow` | string | no | Workflow reference. |
| `pipeline[].input` | map | no | Input mapping for sub-workflows. |
| `pipeline[].params`| map | no | Flat param map for agents. |
| `pipeline[].depends_on`| array | no | Explicit step dependencies. |
| `pipeline[].condition` | string | no | JMESPath condition evaluated by the Orchestrator against the completed preceding step's output before the next step runs. The agent is never involved. If the condition evaluates to false, the step is skipped (`status: skipped`, `output: {"skipped": true, "reason": "condition_false"}`). |

## Variable Substitution

Workflows use `{{ expr }}` for string interpolation. Use the following namespaces:

- **`{{ params.NAME }}`**: Accesses workflow input parameters.
- **`{{ env.NAME }}`**: Accesses environment variables (**Root workflows only**).
- **`{{ step.ID.FIELD }}`**: Accesses output from a successfully completed step.

### JMESPath Context

In `condition:` or transform `expr:`, use bare [JMESPath](https://jmespath.org) (no `{{ }}`).
- Example: `step.parse.category == 'billing'`

## Notes

- **Flat Params**: Parameters for agents are now a flat map. Do not use the nested `params.agent` or `params.server` structure in the workflow file.
- **Workflow Inputs**: Use the `input` field to pass data into sub-workflows.
- **Secrets**: Mark env vars as `secret: true` to ensure they are scrubbed from logs and the run envelope.
