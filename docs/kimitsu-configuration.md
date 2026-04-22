# Kimitsu — Configuration Reference

## Architecture & Design Reference — v4

---

## Project Structure

Kimitsu projects follow a strict directory layout to ensure all components can be resolved by the orchestrator at boot time.

```
my-project/
  gateway.yaml                      # provider registry and model group definitions
  servers.yaml                      # marketplace dependency manifest (optional)

  workflows/                        # Kind: workflow
    support-triage.workflow.yaml
    onboarding.workflow.yaml

  agents/                           # Agent definitions (no Kind field)
    triage.agent.yaml
    summarize.agent.yaml

  servers/                          # Kind: tool-server
    wiki.server.yaml

  environments/                     # Environment overrides
    dev.env.yaml
    prod.env.yaml
```

---

## Workflow Reference

The workflow file defines the pipeline, input schema, and environment variables.

```yaml
kind: workflow
name: "support-triage"          # unique identity for invocation
version: "1.2.0"                # semver versioning
visibility: root                # root (public) | sub-workflow (internal)

params:                         # input data contract
  schema:
    type: object
    required: [message, user_id]
    properties:
      message: { type: string, description: "Raw user input" }
      user_id: { type: string, description: "User ID for lookup" }

env:                            # environment variables (ROOT ONLY)
  - name: SLACK_WEBHOOK_URL     # env var name
    secret: true                # true = masked in logs and envelope
    description: "Slack URL"

pipeline:                       # ordered steps
  - id: parse                   # unique step ID
    agent: ktsu/secure-parser@1.0.0
    params:                     # flat map of agent parameters
      source_field: message     # refers to params.message
    model:                      # optional model overrides
      group: economy
      max_tokens: 512

  - id: triage
    agent: "./agents/triage.agent.yaml"
    depends_on: [parse]         # wait for 'parse' step to complete
    params:
      mode: "detailed"
      context: "{{ step.parse.intent }}" # interpolate from previous step

  - id: notify
    webhook:
      url: "{{ env.SLACK_WEBHOOK_URL }}" # reference root env secret
      method: POST
      body:
        text: "{{ step.triage.summary }}"
    condition: "step.triage.priority == 'high'" # JMESPath condition

model_policy:                   # global cost and timeout settings
  cost_budget_usd: 0.50
  timeout_s: 30
```

---

## Agent Reference

Agents are identified by their filename and do not use a `kind: agent` field.

```yaml
name: triage-agent               # identity for logs
model: standard                  # model group from gateway.yaml
params:                          # parameter contract
  schema:
    type: object
    required: [mode]
    properties:
      mode: { type: string }

prompt:
  system: |                      # MUST BE STATIC (no {{ }} templates)
    You are a triage agent. Analyze inputs and categorize them.
  user: |                        # Supports {{ params.name }} and {{ step.id.field }}
    Process this: {{ params.mode }}
    Previous result: {{ step.parse.summary }}

output:                          # Air-Lock validated output schema
  schema:
    type: object
    required: [category, ktsu_confidence]
    properties:
      category: { type: string }
      ktsu_confidence: { type: number }
```

---

## Variable Substitution Summary

| Purpose | Syntax | Context |
|---|---|---|
| Environment Variable | `{{ env.NAME }}` | Root Workflow only |
| Local Parameters | `{{ params.NAME }}` | All files |
| Step Outputs | `{{ step.ID.FIELD }}` | All pipeline files |

---

*Revised April 2026*
