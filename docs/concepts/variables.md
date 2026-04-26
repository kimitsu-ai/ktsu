# Variables

Kimitsu uses a `{{ expr }}` template syntax for runtime value injection across all pipeline YAML files. There are three namespaces, each with distinct scoping rules.

---

## Quick Reference

| Syntax | Resolves to | Available in |
|---|---|---|
| `{{ env.NAME }}` | Global environment variable | Root workflow files only |
| `{{ params.NAME }}` | Local parameter | Workflows, agents, servers |
| `{{ step.ID.FIELD }}` | Output of a completed step | Workflow pipeline steps |

In `condition:` fields and transform `expr:` values, use bare JMESPath — no `{{ }}` wrapping. Example: `step.parse.valid == 'true'`.

`prompt.system` in agent files is **static-only** — `{{ }}` is a boot error there. All dynamic content must go in `prompt.user`.

---

## Environment Variables (`env.NAME`)

Environment variables are global values provided at startup.

### Declaration

**In `env.yaml`** — sets the values for the active environment:

```yaml
# environments/prod.env.yaml
name: prod
variables:
  SLACK_WEBHOOK_URL: "https://hooks.slack.com/..."
  LOG_LEVEL: "info"
```

**In a root `workflow.yaml`** — declares which env vars the pipeline needs and whether they are secret:

```yaml
# workflows/support-triage.workflow.yaml
visibility: root

env:
  - name: SLACK_WEBHOOK_URL
    secret: true
    description: "Outbound Slack webhook"
  - name: LOG_LEVEL
```

The `env` block is a declaration of intent — it tells the runtime which variables to inject and how to handle them. Values always come from the active `env.yaml`.

### Usage

`{{ env.NAME }}` is **only valid inside root workflows** (`visibility: root`). Sub-workflows never have direct access to env vars; they receive values via parameters passed from the parent.

```yaml
# In a webhook step inside a root workflow
- id: notify
  webhook:
    url: "{{ env.SLACK_WEBHOOK_URL }}"
    body:
      text: "{{ step.triage.summary }}"
```

---

## Parameters (`params.NAME`)

Parameters are explicitly typed inputs declared at each layer and threaded down through the call chain.

### In Workflows

Workflow params are declared as a JSON Schema and validated when the workflow is invoked:

```yaml
# workflows/support-triage.workflow.yaml
params:
  schema:
    type: object
    required: [message, user_id]
    properties:
      message:  { type: string }
      user_id:  { type: string }
```

Reference them in step definitions with `{{ params.NAME }}`:

```yaml
pipeline:
  - id: parse
    agent: ./agents/parser.agent.yaml
    params:
      source: "{{ params.message }}"
```

### In Agents

Agent params are declared in the same JSON Schema format and populated by the workflow step's `params` map:

```yaml
# agents/parser.agent.yaml
params:
  schema:
    type: object
    required: [source]
    properties:
      source: { type: string }
```

Agents can use `{{ params.NAME }}` in `prompt.user`, `prompt.reflect`, and server `params` overrides. They cannot use `{{ env.NAME }}` — credentials must be passed as params from the workflow.

### In Servers

Server params are declared with an optional `default` and a `secret` flag. Values are provided by the agent that uses the server:

```yaml
# servers/wiki.server.yaml
params:
  api_key:
    description: "API key for the wiki service"
    secret: true
  region:
    description: "Deployment region"
    default: "us-east-1"
```

The agent passes values into the server at the step level:

```yaml
# In agent.yaml
servers:
  - name: wiki-search
    path: servers/wiki.server.yaml
    params:
      region: "eu-west"
      api_key: "{{ params.api_key }}"   # forwarded from the agent's own params
```

### In Sub-Workflows

Use the `input` field on a workflow step to map parent data into the sub-workflow's `params`:

```yaml
- id: validate
  workflow: ./sub/validator.workflow.yaml
  input:
    source: "{{ step.parse.output }}"
    channel: "support"
```

---

## Step Outputs (`step.ID.FIELD`)

Step outputs are automatically available once a step completes. Reference them in any downstream step:

```yaml
- id: alert
  webhook:
    url: "{{ env.SLACK_WEBHOOK_URL }}"
    body:
      category: "{{ step.triage.category }}"
      priority:  "{{ step.triage.priority }}"
```

Ordering is inferred from `{{ step.ID.FIELD }}` references. Use `depends_on` only when you need an explicit dependency on a step whose output you don't directly reference.

---

## Secrets

Mark a value `secret: true` at every layer it passes through. The runtime enforces this chain at boot time — a gap anywhere causes a startup error.

This enforcement is bidirectional: if a param is declared `secret: true`, any value passed into it must also be declared secret at its source. Passing a non-secret value into a secret param is a runtime error. The orchestrator redacts secret values from logs and the run envelope regardless of where they appear.

```
env.yaml  ──▶  workflow env block  ──▶  agent params.schema  ──▶  server params
(value)         secret: true             secret: true               secret: true
```

**Example end-to-end:**

```yaml
# 1. env.yaml — value source
variables:
  WIKI_API_KEY: "sk-..."

# 2. workflow.yaml — declare secret env var, pass to agent as secret param
env:
  - name: WIKI_API_KEY
    secret: true

pipeline:
  - id: search
    agent: ./agents/searcher.agent.yaml
    params:
      api_key: "{{ env.WIKI_API_KEY }}"   # env → param

# 3. agent.yaml — declare param as secret, forward to server
params:
  schema:
    properties:
      api_key: { type: string, secret: true }

servers:
  - path: servers/wiki.server.yaml
    params:
      api_key: "{{ params.api_key }}"     # param → server param

# 4. server.yaml — declare server param as secret, use in auth
params:
  api_key:
    secret: true
auth:
  scheme: raw
  secret: "{{ params.api_key }}"
```

Secret values are scrubbed from logs and the run envelope at every layer where `secret: true` is set.

### Debugging Secret Propagation Failures

The runtime validates the secret chain at boot time. The three most common errors are:

**`Error: secret param must use env: source`**
Triggered when a workflow step passes a plain string literal or a `param:` reference that is not declared `secret: true` into an agent param that is marked `secret: true`. Fix: ensure the source declaration includes `secret: true` at every layer back to `env.yaml`.

**`Error: server param is secret but agent param is not marked secret`**
Triggered when a server's `params` block declares a param `secret: true`, but the agent param feeding it via `{{ params.NAME }}` lacks `secret: true` in its schema. Fix: add `secret: true` to the corresponding property in the agent's `params.schema`.

**`Error: env var not set`**
Triggered when an `env:VAR_NAME` source references a variable that is absent from the active `env.yaml` (or the process environment). Fix: add the variable to the correct `env.yaml` file and ensure it is selected at startup.

**Correct vs. broken chain — side by side:**

```yaml
# CORRECT: secret: true at every layer
# workflow.yaml
env:
  - name: API_KEY
    secret: true          # ✓ declared secret at env level
pipeline:
  - id: call
    agent: ./agents/caller.agent.yaml
    params:
      api_key: "{{ env.API_KEY }}"

# caller.agent.yaml
params:
  schema:
    properties:
      api_key: { type: string, secret: true }   # ✓ marked secret
servers:
  - path: servers/svc.server.yaml
    params:
      api_key: "{{ params.api_key }}"

# svc.server.yaml
params:
  api_key:
    secret: true          # ✓ marked secret

---

# BROKEN: secret: true missing on agent param → boot error
# caller.agent.yaml
params:
  schema:
    properties:
      api_key: { type: string }   # ✗ missing secret: true
                                  # Error: server param is secret but agent param is not marked secret
```

---

*Revised April 2026*
