# Kimitsu — Workflow Composition, Params, and Environment Scoping
## Design Spec — April 2026

This document covers four tightly related design decisions made in a single session. They form a coherent system and should be implemented together. Each section describes what changes, what the new behavior is, and what existing docs need updating.

---

## Summary of Changes

1. **Workflow step** — a fourth pipeline primitive. A workflow can be a step in another workflow's pipeline, running inline under the parent's `run_id`.
2. **Sub-workflow visibility** — installed hub workflows are `sub-workflow` only. They cannot be invoked directly via the HTTP API. Root workflows are the sole entry points.
3. **Webhook execution in sub-workflows** — webhook steps in sub-workflows are suppressed by default. Execution is opt-in at both the sub-workflow declaration and the parent call site.
4. **Params** — a new value resolution mechanism scoped to a single invocation. Declared on workflows and agents, passed by the caller, resolved with `param:` prefix.
5. **Environment variable scoping** — `env:` references are restricted to root workflows only. Sub-workflows, agents, and server files may not reference environment variables. This is a boot error.
6. **Shipped input/output workflows** — `ktsu/slack-input`, `ktsu/slack-reply`, and similar first-party workflows ship with the binary. They are sub-workflow only and cover common integration patterns.
7. **ktsuhub** — the workflow registry. Workflows are published from GitHub repos via OAuth. Installed via `ktsu hub install`. Cached locally, mounted as additional orchestrator workspaces via `ktsuhub.lock.yaml`.

---

## 1. The Workflow Step — Fourth Pipeline Primitive

### What it is

A `workflow` step invokes another workflow inline as a step in the parent pipeline. The sub-workflow runs under the parent's `run_id`, its output is available to downstream steps, and its cost rolls up to the parent's budget. It is deterministic — no LLM calls, no tools. The orchestrator executes it directly.

### Updated primitive table

| Primitive | LLM | Tools | Executed by |
|---|---|---|---|
| Transform | Never | Never | Orchestrator |
| Agent | Always | Optional | Agent Runtime |
| Webhook | Never | Never | Orchestrator |
| **Workflow** | **Never** | **Never** | **Orchestrator** |

### YAML syntax

```yaml
- id: triage
  workflow: kyle/support-triage        # installed hub workflow: author/name
  # OR
  workflow: ./workflows/local.workflow.yaml   # local workflow by path
  # OR
  workflow: ktsu/slack-input           # shipped first-party workflow

  input:                               # maps parent step outputs to sub-workflow input.schema
    message:    "input.raw_body"       # JMESPath against accumulated step outputs
    user_id:    "input.customer_id"
    channel_id: "slack.channel_id"

  params:                              # passes params to sub-workflow (see Section 4)
    webhook_url: "env:SLACK_WEBHOOK_URL"   # env: resolved here in root context
    username:    "`support-bot`"           # literal string

  webhooks: execute                    # opt-in to sub-workflow webhook execution (see Section 3)
  depends_on: [parse]
```

### Workflow step fields

| Field | Type | Required | Description |
|---|---|---|---|
| `workflow` | string | yes | Sub-workflow reference. Hub: `author/name`. Local: `./path/to/file.workflow.yaml`. Shipped: `ktsu/name` |
| `input` | object | yes if sub-workflow declares required fields | JMESPath expressions mapping accumulated step outputs to the sub-workflow's `input.schema` |
| `params` | object | yes if sub-workflow declares required params | Values passed to the sub-workflow's `params.schema`. May use `env:`, `param:`, literals, or JMESPath |
| `webhooks` | string | no | `execute` or `suppress` (default: `suppress`). See Section 3 |
| `depends_on` | string[] | no | Step IDs to wait for |

### Input mapping and Air-Lock

The `input` block on a workflow step maps parent pipeline data to the sub-workflow's declared `input.schema`. This mapping is validated at boot — not at runtime. Missing required fields, type mismatches, and invalid JMESPath expressions are all boot errors.

The sub-workflow's output becomes the workflow step's output. Downstream steps reference it by step ID exactly as they would any other step:

```yaml
- id: notify
  webhook:
    body:
      text: "triage.recommendation"   # triage is a workflow step
```

### Execution model

1. Orchestrator resolves the sub-workflow's file from its workspace root.
2. Orchestrator evaluates the `input` block JMESPath expressions against accumulated step outputs, assembles the sub-workflow input payload.
3. Orchestrator validates the assembled input against the sub-workflow's `input.schema`. Failure → step fails immediately.
4. Orchestrator resolves `params` block values (see Section 4).
5. Orchestrator executes the sub-workflow's pipeline inline, under the parent's `run_id`.
6. Sub-workflow steps appear in the parent envelope namespaced under the workflow step ID.
7. The sub-workflow's final step output becomes the workflow step's output, validated by Air-Lock before downstream steps can read it.
8. Cost, tokens, and tool calls from all sub-workflow steps roll up to the parent run totals.

### Envelope structure for workflow steps

Sub-workflow steps appear nested in the parent envelope under the workflow step ID:

```json
{
  "run_id": "run_abc123",
  "steps": {
    "parse": { "status": "ok", "metrics": { ... } },
    "triage": {
      "status": "ok",
      "type": "workflow",
      "workflow": "kyle/support-triage@1.2.0",
      "steps": {
        "parse-inbound": { "status": "ok", "metrics": { ... } },
        "classify":      { "status": "ok", "metrics": { ... } }
      },
      "metrics": {
        "cost_usd":   0.041,
        "tokens_in":  3200,
        "tokens_out": 890,
        "duration_ms": 2400
      }
    },
    "notify": { "status": "ok", "metrics": { ... } }
  }
}
```

### Cycle detection

The orchestrator's boot-time DFS cycle check is extended to cover workflow step references. A workflow that references itself — directly or transitively through sub-workflows — is a boot error.

```
ERROR  Workflow cycle detected.

  Cycle path: support-triage → legal-review → support-triage
  Declared in: workflows/support-triage.workflow.yaml (step "legal-review")

  Workflow steps may not form cycles. Each workflow in the dependency
  tree must be distinct.
```

### Cost budget

Sub-workflows run against the parent's `cost_budget_usd`. A sub-workflow does not declare its own budget when used as a step — the parent owns the budget envelope for the entire run. The orchestrator passes the remaining budget to the sub-workflow's execution context. If the budget is exhausted mid-sub-workflow, the sub-workflow step fails with `budget_exceeded` and the parent run fails.

---

## 2. Sub-Workflow Visibility

### The problem

Hub-installed workflows are third-party code. If they can be invoked directly via `POST /invoke/{workflow}`, they become independent attack surfaces — callable with arbitrary input, bypassing whatever validation and context the parent workflow establishes.

Hub workflows are designed to be composed, not to be entry points. Their `input.schema` assumes upstream context has already been validated by the root workflow. Direct invocation means that context is missing or attacker-controlled.

### The rule

**Hub-installed workflows are `sub-workflow` visibility by default. They cannot be invoked directly via `POST /invoke/{workflow}`. They are only reachable as `workflow` steps in a parent pipeline.**

Local workflows in `./workflows/` are `root` visibility by default — invokable directly.

Shipped first-party workflows (`ktsu/` namespace) are `sub-workflow` visibility always. They are never directly invokable.

### Visibility field

```yaml
kind: workflow
name: support-triage
version: "1.2.0"
visibility: sub-workflow    # root (default for local) | sub-workflow (default for hub/shipped)
```

### How visibility is assigned

| Source | Default visibility | Override allowed |
|---|---|---|
| Local (`./workflows/`) | `root` | Yes — declare `visibility: sub-workflow` to prevent direct invocation |
| Hub-installed (`~/.ktsu/cache/`) | `sub-workflow` | Yes — `ktsu hub install author/name --allow-root` at install time |
| Shipped (`ktsu/` namespace) | `sub-workflow` | No — always sub-workflow, no override |

The `--allow-root` flag at install time writes a flag to `ktsuhub.lock.yaml` and is the only way to make a hub workflow directly invokable. It is intentional and visible — not a YAML field that might be overlooked.

### Enforcement

`POST /invoke/{workflow}` returns 404 for any workflow with `sub-workflow` visibility. The workflow exists in the orchestrator's namespace and is reachable as a step, but the invoke API does not expose it.

`ktsu invoke <workflow>` respects the same rule — it calls the invoke API and gets the same 404.

### Webhook steps cannot target sub-workflows

A `webhook` step that points to the orchestrator's invoke endpoint for a sub-workflow is a boot error. Sub-workflows are not reachable via HTTP — only via workflow step dispatch inside the orchestrator.

---

## 3. Webhook Execution in Sub-Workflows

### The problem

A sub-workflow may declare webhook steps — for example, `ktsu/slack-reply` exists specifically to POST a message to Slack. If sub-workflow webhooks fired automatically, installing any hub workflow could trigger arbitrary HTTP calls the parent pipeline never explicitly authorized.

### The rule

**Webhook steps in sub-workflows are suppressed by default. Execution requires explicit opt-in at both the sub-workflow declaration and the parent call site. Both must agree for a webhook to fire.**

### Sub-workflow declaration

The workflow file declares its webhook execution intent:

```yaml
kind: workflow
name: ktsu/slack-reply
version: "1.0.0"
visibility: sub-workflow
webhooks: execute    # execute | suppress — default: suppress
```

`webhooks: suppress` (default) — no webhook steps fire when this workflow is used as a sub-workflow. Output is returned to the parent only.

`webhooks: execute` — this workflow intends to fire its webhook steps. The parent must also opt in.

### Parent call site opt-in

```yaml
- id: reply
  workflow: ktsu/slack-reply
  webhooks: execute    # parent explicitly authorizes webhook execution
  params:
    webhook_url: "env:SLACK_WEBHOOK_URL"
  input:
    channel_id: "slack.channel_id"
    text:       "triage.recommendation"
  depends_on: [triage]
```

### Resolution table

| Sub-workflow declares | Parent step declares | Webhooks fire? |
|---|---|---|
| `suppress` | *(omitted)* | No |
| `suppress` | `execute` | Boot error — parent tries to authorize what sub-workflow doesn't support |
| `execute` | *(omitted)* | No — parent must explicitly opt in |
| `execute` | `suppress` | No |
| `execute` | `execute` | Yes |

Fail-closed. A webhook only fires if both sides explicitly agree.

### Output is always returned regardless

Webhook execution and output propagation are independent. A sub-workflow with `webhooks: execute` still returns its final step output to the parent. The parent receives both the side effect and the data.

A `ktsu/slack-reply` step returns `{ "sent": true, "status_code": 200 }` to the parent pipeline, which can use that output for logging, conditional branching, or further steps.

### Boot validation

```
ERROR  Webhook execution conflict on workflow step.

  Step:        reply (workflows/support-bot.workflow.yaml)
  Sub-workflow: ktsu/slack-reply@1.0.0
  Problem:     Parent step declares webhooks: execute but sub-workflow
               declares webhooks: suppress. Sub-workflow does not support
               webhook execution.

  Fix: Remove webhooks: execute from the parent step, or use a sub-workflow
       that declares webhooks: execute.
```

---

## 4. Params — Invocation-Scoped Value Resolution

### What params are

Params are named values passed by a caller to a workflow or agent at invocation time. They are resolved with the `param:` prefix anywhere `env:` is currently used. They are scoped to a single invocation — not process-wide, not shared between steps unless explicitly passed.

### Why params exist

Environment variables are process-wide. If a sub-workflow or agent reads `env:SLACK_WEBHOOK_URL` directly, every invocation of that component in the entire process hits the same URL. You cannot instantiate the same sub-workflow twice with different webhook targets. You cannot audit from the root workflow what URLs a hub-installed component will call — it reads silently from the environment.

Params make all of this explicit. The root workflow is the sole env adapter. It reads `env:` and passes values down as params. Everything below the root uses `param:` and receives values only from its caller.

### Declaring params on a workflow

```yaml
kind: workflow
name: ktsu/slack-reply
version: "1.0.0"
visibility: sub-workflow
webhooks: execute

params:
  schema:
    type: object
    required: [webhook_url]
    properties:
      webhook_url:
        type: string
        description: "Slack incoming webhook URL for the target channel"
      username:
        type: string
        default: "kimitsu"
        description: "Bot display name"
```

### Using params inside a workflow

```yaml
pipeline:
  - id: post
    webhook:
      url: "param:webhook_url"      # param: prefix — resolved from params passed by caller
      method: POST
      body:
        text:     "input.text"
        username: "param:username"
```

### Declaring params on an agent

```yaml
name: crm-lookup-agent
model: standard
params:
  schema:
    type: object
    required: [crm_token]
    properties:
      crm_token:
        type: string
        description: "CRM API bearer token"
servers:
  - name: crm
    path: servers/crm.server.yaml
    auth: "param:crm_token"         # param: in server auth — resolved at invocation time
```

### Declaring params on a server file

```yaml
name: wiki-search
url: "https://mcp.internal/wiki"
auth: "param:wiki_token"            # param: — caller provides the token
```

### Passing params from a workflow step

```yaml
- id: reply
  workflow: ktsu/slack-reply
  params:
    webhook_url: "env:SLACK_WEBHOOK_URL"    # env: resolved here in root workflow context
    username:    "`support-bot`"            # literal string — JMESPath backtick syntax
  input:
    channel_id: "slack.channel_id"
    text:       "triage.recommendation"
```

### Params value sources at the call site

| Syntax | Resolves from |
|---|---|
| `"env:VAR_NAME"` | Process environment — only valid in root workflow context |
| `"param:name"` | Params passed to the current workflow by its caller |
| `"`literal`"` | JMESPath literal string |
| `"step-id.field"` | JMESPath against accumulated step outputs |

### How params flow through the invocation payload

The orchestrator resolves all param values before dispatching any invocation. By the time a payload reaches the Agent Runtime, all `param:` references are resolved to their values. The Agent Runtime never sees param names — only resolved values. This applies to server auth tokens as well: the Agent Runtime injects the resolved auth value when establishing MCP connections.

Params are resolved in this order:
1. Caller's `params` block is evaluated — `env:` references resolved from process environment, JMESPath expressions evaluated against step outputs.
2. Resolved values are validated against the sub-workflow's or agent's `params.schema`. Missing required params and type mismatches are boot errors.
3. Default values from `params.schema` are applied for any omitted optional params.
4. Resolved params are included in the invocation payload. `param:name` references in the sub-workflow, agent, and server files resolve against this payload.

### Boot validation for params

```
ERROR  Missing required param on workflow step.

  Step:        reply (workflows/support-bot.workflow.yaml)
  Sub-workflow: ktsu/slack-reply@1.0.0
  Missing:     webhook_url (required)

  Fix: Add webhook_url to the params block on the workflow step.

ERROR  Unknown param reference.

  File:    agents/triage.agent.yaml
  Line:    auth: "param:crm_api_key"
  Problem: No param named "crm_api_key" is declared in this agent's
           params.schema.

  Fix: Declare crm_api_key in the agent's params.schema, or correct
       the param name.
```

---

## 5. Environment Variable Scoping

### The rule

**`env:` references are permitted only in root workflows. Sub-workflows, agents, and server files may not reference environment variables. This is enforced at boot and is a hard error.**

This is a new invariant. See Section 6 for the full invariant text.

### Where `env:` is currently used and what changes

| Location | Current | New |
|---|---|---|
| Root workflow webhook `url` | `env:SLACK_WEBHOOK_URL` | Unchanged — root workflows may use `env:` |
| Root workflow params block | — | `env:` is how root workflows read env vars and pass them as params |
| Sub-workflow webhook `url` | `env:SLACK_WEBHOOK_URL` | **Boot error** — use `param:` instead |
| Agent server `auth` | `env:WIKI_TOKEN` | **Boot error** — use `param:` instead |
| Local server file `auth` | `env:WIKI_TOKEN` | **Boot error** — use `param:` instead |
| Gateway `api_key` | `env:ANTHROPIC_API_KEY` | Unchanged — gateway is root infrastructure, not a workflow component |
| Env config `state.dsn` | `env:DATABASE_URL` | Unchanged — env config is root infrastructure |

### The resolution context per file type

| File type | `env:` | `param:` | `input:` |
|---|---|---|---|
| Root workflow | Yes | Yes (from invoke params) | Yes |
| Sub-workflow | **No — boot error** | Yes | Yes |
| Agent | **No — boot error** | Yes | Yes (via envelope) |
| Server file | **No — boot error** | Yes | No |
| Gateway config | Yes | No | No |
| Env config | Yes | No | No |

### Migration pattern

Before (agent server auth using env directly):
```yaml
servers:
  - name: crm
    path: servers/crm.server.yaml
    # crm.server.yaml contains: auth: "env:CRM_TOKEN"
```

After (env read at root, passed as param):
```yaml
# Root workflow:
- id: triage
  agent: ./agents/triage.agent.yaml
  agent_params:
    crm_token: "env:CRM_TOKEN"    # env: read here at root

# triage.agent.yaml:
params:
  schema:
    required: [crm_token]
    properties:
      crm_token: { type: string }
servers:
  - name: crm
    path: servers/crm.server.yaml
    auth: "param:crm_token"       # param: resolved from agent params

# crm.server.yaml:
name: crm
url: "https://mcp.internal/crm"
auth: "param:crm_token"           # param: — no env: reference
```

### Boot error for env violations

```
ERROR  Environment variable reference outside root workflow context.

  File:    ~/.ktsu/cache/kyle/support-triage@1.2.0/agents/triage.agent.yaml
  Line:    auth: "env:CRM_TOKEN"
  Context: hub-installed sub-workflow kyle/support-triage

  Sub-workflows, agents, and server files may not reference environment
  variables directly. Declare a param and pass the value from the root
  workflow.

  See: https://kimitsu.ai/docs/params

ERROR  Environment variable reference outside root workflow context.

  File:    servers/crm.server.yaml
  Line:    auth: "env:CRM_TOKEN"
  Context: referenced by agent agents/triage.agent.yaml

  Server files may not reference environment variables directly.
  Use auth: "param:<name>" and pass the token from the calling agent's
  params block.
```

### Invariant 14 — updated text

**Old:** "All secrets are indirected. Credentials are always `env:VAR_NAME`. Secrets never appear in YAML files. LLM provider keys live only in the LLM Gateway."

**New:** "All secrets are indirected. Secrets never appear in YAML files. In root workflows, credentials are referenced as `env:VAR_NAME` and passed to sub-components as params. In sub-workflows, agents, and server files, credentials are received as `param:name` — never read from the environment directly. LLM provider keys live only in the LLM Gateway."

---

## 6. New and Updated Invariants

The following invariants are added to `kimitsu-invariants.md`. Invariant 25 is updated. Invariant 14 is updated (see Section 5 above).

### Updated — Invariant 25

**Old:** "There are exactly three pipeline primitives. Transform, agent, webhook."

**New:** "There are exactly four pipeline primitives. Transform, agent, webhook, workflow. No other step types exist. If logic requires LLM reasoning, it is an agent. If it is deterministic data shaping, it is a transform. If it needs to call an external HTTP endpoint, it is a webhook. If it delegates to another workflow's full pipeline, it is a workflow step."

### New — Invariant 34

**34. The root workflow is the sole environment adapter.** `env:` references are permitted only in root workflows. Sub-workflows, agents, and server files may not reference environment variables. All values they need from the environment are passed explicitly as params by the root workflow. This is enforced at boot. An `env:` reference in a sub-workflow, agent, or server file is a boot error regardless of whether it appears in a hub-installed or local file.

### New — Invariant 35

**35. Hub-installed and shipped workflows are sub-workflow only by default.** A workflow installed via `ktsu hub install` or shipped in the `ktsu/` namespace cannot be invoked directly via `POST /invoke/{workflow}`. It is only reachable as a `workflow` step in a parent pipeline. This restriction exists because installed workflows are third-party code — direct invocability would make them independent attack surfaces callable with arbitrary input, bypassing the validation and context the root workflow establishes. Local workflows in `./workflows/` are root-invokable by default.

### New — Invariant 36

**36. Sub-workflow webhook execution is opt-in at both ends.** Webhook steps in a sub-workflow are suppressed by default. For a webhook to fire, the sub-workflow must declare `webhooks: execute` and the parent workflow step must also declare `webhooks: execute`. If either side does not agree, the webhook is suppressed. A parent trying to authorize webhook execution on a sub-workflow that declares `webhooks: suppress` is a boot error. This ensures side effects from sub-workflows are always visible in and authorized by the root pipeline.

### New — Invariant 37

**37. Params are the only mechanism for passing invocation-scoped values down the dependency tree.** A workflow step, agent step, or server file that needs a value varying by caller declares it as a param. The caller passes it explicitly. There is no ambient value resolution below the root workflow — only what is explicitly passed. This makes the data flow of every invocation fully auditable from the root workflow file.

### New — Invariant 38

**38. Workflow steps are orchestrator-executed and zero-cost by definition.** A workflow step burns no LLM tokens directly. Cost accumulates only from agent steps within the sub-workflow's pipeline, rolled up to the parent run totals. The orchestrator executes workflow step dispatch inline — no Agent Runtime involvement for the dispatch itself.

---

## 7. Shipped Input/Output Workflows

### Concept

Shipped workflows are first-party workflow files distributed with the Kimitsu binary. They cover common integration patterns — validating and normalizing inbound webhooks, posting replies, handling platform-specific protocol details. They are always `sub-workflow` visibility and cannot be overridden to `root`.

They live in the `ktsu/` workflow namespace, parallel to the `ktsu/` tool server namespace. Both signal first-party origin and always-available without install.

### Shipped workflow list (initial)

| Workflow | Description |
|---|---|
| `ktsu/slack-input` | Validates Slack webhook signature, normalizes event payload to standard fields |
| `ktsu/slack-reply` | Posts a reply to a Slack channel. Requires `webhook_url` param |
| `ktsu/telegram-input` | Validates Telegram webhook, normalizes update payload |
| `ktsu/telegram-reply` | Sends a Telegram message. Requires `bot_token` and `chat_id` params |
| `ktsu/discord-input` | Validates Discord interaction, normalizes payload |
| `ktsu/discord-reply` | Posts a Discord message. Requires `webhook_url` param |
| `ktsu/webhook-input` | Generic inbound webhook normalizer — validates signature if secret provided, passes body through |

### Shipped workflow conventions

- Always `visibility: sub-workflow`
- No `env:` references — all external values come through params
- No agent steps unless essential — shipped input workflows use only transforms and toolless agents for validation
- Output schema is stable and semver-versioned — downstream steps can rely on it

### Example — full Slack bot pipeline

This shows the complete composition pattern using shipped workflows and an installed sub-workflow:

```yaml
kind: workflow
name: support-bot
version: "1.0.0"

input:
  schema:
    type: object
    required: [body, headers]
    properties:
      body:    { type: object }
      headers: { type: object }

pipeline:
  - id: slack
    workflow: ktsu/slack-input
    params:
      signing_secret: "env:SLACK_SIGNING_SECRET"
    input:
      body:    "input.body"
      headers: "input.headers"

  - id: normalize
    transform:
      inputs:
        - from: slack
      ops:
        - map:
            expr: "{ message: text, user_id: user_id, channel_id: channel_id }"
    output:
      schema:
        type: object
        required: [message, user_id, channel_id]
        properties:
          message:    { type: string }
          user_id:    { type: string }
          channel_id: { type: string }

  - id: triage
    workflow: kyle/support-triage
    params:
      crm_token: "env:CRM_TOKEN"
    input:
      message:    "normalize.message"
      user_id:    "normalize.user_id"
      channel_id: "normalize.channel_id"
    depends_on: [normalize]

  - id: reply
    workflow: ktsu/slack-reply
    webhooks: execute
    params:
      webhook_url: "env:SLACK_WEBHOOK_URL"
      username:    "`support-bot`"
    input:
      channel_id: "slack.channel_id"
      text:       "triage.recommendation"
    depends_on: [triage]

model_policy:
  cost_budget_usd: 0.50
```

This root workflow reads four environment variables (`SLACK_SIGNING_SECRET`, `CRM_TOKEN`, `SLACK_WEBHOOK_URL`). Every sub-workflow receives only what it explicitly needs via params. Nothing below the root touches the environment.

---

## 8. ktsuhub — Registry

### Overview

ktsuhub is the workflow registry. Authors publish workflows from GitHub repositories via OAuth. Users install them via `ktsu hub install`. Installed workflows are cached locally and mounted as additional workspaces by the orchestrator.

### `ktsuhub.yaml` — repo manifest

Every published repo requires this file at the repo root.

```yaml
workflows:
  - name: support-triage
    description: "Triages inbound support tickets by category, priority, and urgency"
    version: "1.2.0"
    path: workflows/support-triage.workflow.yaml    # explicit path — no auto-discovery
    tags: [support, triage, nlp]

  - name: onboarding-flow
    description: "Automated customer onboarding pipeline with CRM sync"
    version: "0.4.0"
    path: workflows/onboarding.workflow.yaml
    tags: [crm, onboarding]
```

Each entry is independently versioned, starred, and installed on the hub. A single repo may publish multiple workflows.

### `ktsuhub.lock.yaml` — installed workflow lock file

Written to the project root by `ktsu hub install`. Read by `ktsu start orchestrator` to mount installed workflows as additional workspaces.

```yaml
installed:
  - name: support-triage
    author: kyle
    version: "1.2.0"
    source: "https://github.com/kyle/kimitsu-workflows"
    ref: "v1.2.0"
    sha: "a3f9c12d8e4b1f7c2a9d0e5b3c6f8a1d2e4b7c9a"
    cache: "~/.ktsu/cache/kyle/support-triage@1.2.0"
    installed_at: "2026-04-09T09:11:44Z"
    mutable: false
    allow_root: false
```

### Multi-workspace orchestrator

The orchestrator natively supports multiple workspace roots. Each workspace is a project root. Workflow names must be unique across all workspaces — duplicates are a boot error.

`ktsu start orchestrator` automatically reads `ktsuhub.lock.yaml` at the project root if present and mounts each `cache` path as an additional workspace. Pass `--no-hub-lock` to suppress.

Additional workspaces can also be passed explicitly:

```bash
ktsu start orchestrator --workspace ~/shared-workflows
```

Path resolution: agents and server files in an installed workflow resolve relative to their cache directory, not the local project root.

### `ktsu hub` commands

```bash
ktsu hub login                                    # GitHub OAuth — required before publish
ktsu hub install kyle/support-triage              # from registry, latest
ktsu hub install kyle/support-triage@1.2.0        # pinned version
ktsu hub install github.com/kyle/workflows        # direct git repo
ktsu hub install github.com/kyle/workflows@v1.2.0 # direct git, pinned tag
ktsu hub install github.com/kyle/workflows@main   # branch — mutable, flagged in lock
ktsu hub install github.com/kyle/workflows@a3f9c12 # commit SHA — fully pinned
ktsu hub install https://gitlab.com/org/repo      # any git remote
ktsu hub update                                   # re-resolve mutable entries
ktsu hub update --latest                          # update all to latest versions
ktsu hub publish                                  # trigger manual re-publish
ktsu hub search "support triage"                  # search registry
ktsu hub search nlp --tag support
```

All remote installs require `ktsuhub.yaml` at the repo root.

### GitHub integration

ktsuhub uses GitHub OAuth for publish operations. This prevents publishing from repos the authenticated user does not own. On connect, ktsuhub registers a webhook on the repo. On push to the default branch, ktsuhub re-reads `ktsuhub.yaml` and updates all declared workflows automatically.

### Hub workflow visibility enforcement

Hub-installed workflows are `sub-workflow` by default. `POST /invoke/{workflow}` returns 404 for any sub-workflow. To allow direct invocation of a hub workflow:

```bash
ktsu hub install kyle/support-triage --allow-root
```

This writes `allow_root: true` to `ktsuhub.lock.yaml` and is the only mechanism to override sub-workflow visibility on a hub install.

---

## 9. `ktsu workflow tree` — New CLI Command

Walks a workflow file and emits the full dependency tree. Used by ktsuhub to determine what to bundle into a zip download.

```bash
ktsu workflow tree workflows/support-triage.workflow.yaml
```

```
workflows/support-triage.workflow.yaml
├── agents/triage.agent.yaml
│   ├── servers/wiki-search.server.yaml
│   └── servers/crm.server.yaml
├── agents/legal.agent.yaml
│   └── servers/crm.server.yaml
├── agents/consolidator.agent.yaml
└── gateway.yaml
```

```bash
ktsu workflow tree workflows/support-triage.workflow.yaml --json
```

```json
[
  "workflows/support-triage.workflow.yaml",
  "agents/triage.agent.yaml",
  "servers/wiki-search.server.yaml",
  "servers/crm.server.yaml",
  "agents/legal.agent.yaml",
  "agents/consolidator.agent.yaml",
  "gateway.yaml"
]
```

Paths are deduplicated and relative to `--project-dir` (default: `.`).

---

## 10. Documents to Update

The following existing documents need updating to reflect this spec. Changes are additive unless noted.

### `kimitsu-invariants.md`
- Update invariant 14 (secrets/env scoping — see Section 5)
- Update invariant 25 (four primitives — see Section 6)
- Add invariants 34, 35, 36, 37, 38 (see Section 6)

### `kimitsu-pipeline-primitives.md`
- Add workflow step section (syntax, fields, execution model, failure semantics, metrics)
- Update primitive table (four primitives)
- Add shipped workflow section

### `workflow.md` (YAML spec)
- Add workflow step fields to the pipeline step reference
- Add `visibility` field to top-level fields
- Add `webhooks` field to top-level fields
- Add `params.schema` to top-level fields
- Add `param:` prefix to the value resolution notes
- Update webhook step fields — note `env:` only valid in root workflow context

### `agent.md` (YAML spec)
- Add `params.schema` field
- Add `param:` prefix to server `auth` field description
- Note that `env:` in agent files is a boot error

### `server.md` (YAML spec)
- Update `auth` field description — `param:name` is the correct pattern, `env:` is a boot error

### `cli-reference.md`
- Add `ktsu hub` command group
- Add `ktsu workflow tree` command
- Update `ktsu start orchestrator` flags (`--workspace`, `--no-hub-lock`)
- Update `ktsu validate` flags (`--workspace`, `--no-hub-lock`)
- Update `ktsu new project` scaffold (adds `ktsuhub.yaml`)
- Update `ktsu lock` note (distinguish from `ktsuhub.lock.yaml`)
- Add `KTSU_CACHE_DIR` to environment variables table

### `kimitsu-configuration.md`
- Add params resolution section
- Add env scoping section with migration pattern
- Update boot sequence (step 7 — add env scoping check across all referenced files)
- Add boot error examples for env violations and params mismatches

### `kimitsu-overview.md`
- Update primitive table (four primitives)
- Update differentiation section (workflow composition, params scoping, env restriction)

### New documents to create
- `ktsuhub.md` — registry spec (use `ktsuhub-spec.md` from this session)
- Update `cli-reference.md` — use `cli-reference-updated.md` from this session

---

*April 2026*