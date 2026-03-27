# YAML Spec Docs Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Create `docs/yaml-spec/` — a token-efficient, per-kind YAML reference for coding agents.

**Architecture:** Seven markdown files: one index and one per YAML kind. Each kind file uses an annotated YAML block (inline `#` comments) followed by a field reference table. No conceptual prose.

**Tech Stack:** Markdown only — no code, no tests, no dependencies.

---

## File Map

| Create | Purpose |
|---|---|
| `docs/yaml-spec/index.md` | Navigation — one paragraph per kind, filename convention, link |
| `docs/yaml-spec/workflow.md` | `kind: workflow` — pipeline definition |
| `docs/yaml-spec/agent.md` | Agent YAML files (no kind field) |
| `docs/yaml-spec/server.md` | `kind: tool-server` — local MCP server pointer |
| `docs/yaml-spec/servers.md` | `kind: servers` — marketplace manifest |
| `docs/yaml-spec/gateway.md` | `gateway.yaml` — provider registry and model groups |
| `docs/yaml-spec/env.md` | `kind: env` — environment config |

---

### Task 1: Create index.md

**Files:**
- Create: `docs/yaml-spec/index.md`

- [ ] **Step 1: Write the file**

Create `docs/yaml-spec/index.md` with this exact content:

```markdown
# Kimitsu YAML Spec

Reference for all Kimitsu YAML file kinds. One file per kind — annotated example + field table. No conceptual explanations.

| File | Kind | Filename Convention | Description |
|---|---|---|---|
| [workflow.md](workflow.md) | `workflow` | `workflows/*.workflow.yaml` | Pipeline definition — steps, input schema, model policy |
| [agent.md](agent.md) | *(none)* | `agents/*.agent.yaml` | LLM agent — model, system prompt, tool servers, output schema |
| [server.md](server.md) | `tool-server` | `servers/*.server.yaml` | Local MCP tool server pointer — URL, auth, trust flags |
| [servers.md](servers.md) | `servers` | `servers.yaml` | Marketplace dependency manifest — names, sources, versions |
| [gateway.md](gateway.md) | *(none)* | `gateway.yaml` | LLM provider registry and named model group definitions |
| [env.md](env.md) | `env` | `environments/*.env.yaml` | Environment config — variables and state store backend |

## Project Directory Layout

```
my-project/
  servers.yaml                      # marketplace dependency manifest (kind: servers)
  gateway.yaml                      # provider registry and model groups

  workflows/
    support-triage.workflow.yaml    # kind: workflow

  servers/                          # local tool server files (kind: tool-server)
    wiki-search.server.yaml

  agents/
    triage.agent.yaml               # no kind field

  environments/
    dev.env.yaml                    # kind: env
    production.env.yaml

  ktsu.lock.yaml                    # auto-generated — do not edit
```
```

- [ ] **Step 2: Verify**

Confirm the file exists at `docs/yaml-spec/index.md`.

---

### Task 2: Create workflow.md

**Files:**
- Create: `docs/yaml-spec/workflow.md`

- [ ] **Step 1: Write the file**

Create `docs/yaml-spec/workflow.md` with this exact content:

````markdown
# workflow.yaml

**What it does:** Defines a pipeline — input schema, ordered steps, and model cost policy.

**Filename convention:** `workflows/*.workflow.yaml`

## Annotated Example

```yaml
kind: workflow
name: support-triage            # unique name — used in POST /invoke/{name}
version: "1.2.0"                # semver string
description: "..."              # optional

input:
  schema:                       # JSON Schema — validated on invoke; 422 on failure
    type: object
    required: [message, user_id]
    properties:
      message:    { type: string }
      user_id:    { type: string }
      channel_id: { type: string }

pipeline:

  # ── Agent step ────────────────────────────────────────────────────────────
  - id: parse                   # unique step ID — used in depends_on and body references
    agent: ktsu/secure-parser@1.0.0   # built-in: ktsu/<name>@<ver>
                                      # local:    ./agents/foo.agent.yaml  (optional @<ver>)
    params:                     # parameters for built-in agents only
      source_field: message
      extract:
        intent: { type: string, enum: [billing, technical, legal, other] }
    model:                      # optional — overrides agent file's model group
      group: economy            # model group name from gateway.yaml
      max_tokens: 512
    # no depends_on — receives workflow input

  - id: triage
    agent: ./agents/triage.agent.yaml
    depends_on: [parse]         # step IDs this step waits for
    confidence_threshold: 0.7  # min ktsu_confidence; agent output schema must declare ktsu_confidence

  # ── Agent step with fanout ────────────────────────────────────────────────
  - id: enrich
    agent: ./agents/enricher.agent.yaml
    depends_on: [triage]
    for_each:
      from: triage.tickets       # JMESPath — must resolve to an array
      max_items: 20              # optional — truncates array before fanout
      concurrency: 4             # optional — max parallel invocations (default: unbounded)
    # output is always: { "results": [...] } — one entry per item in original order

  # ── Transform step ────────────────────────────────────────────────────────
  - id: merge-reviews
    transform:
      inputs:
        - from: legal-review     # upstream step IDs — depends_on derived automatically
        - from: risk-review
      ops:                       # applied sequentially; each op receives output of previous
        - merge: [legal-review, risk-review]          # concat arrays or deep-merge objects
        - deduplicate: { field: ticket_id }           # first occurrence wins
        - filter:      { expr: "confidence > `0.7`" } # JMESPath; removes falsy items
        - sort:        { field: confidence, order: desc }
        - map:         { expr: "{ id: ticket_id, score: confidence }" }
        - flatten:     {}                             # one level of array nesting
    output:
      schema:                    # Air-Lock validated
        type: array
        items: { type: object }

  # ── Webhook step ──────────────────────────────────────────────────────────
  - id: notify
    webhook:
      url: "env:SLACK_WEBHOOK_URL"   # env:VAR_NAME resolved at runtime
      method: POST                    # default: POST
      body:
        text:    "triage.summary"     # JMESPath against merged step outputs
        channel: "input.channel_id"   # workflow input fields are under "input"
        source:  "`kimitsu`"          # literal string — JMESPath backtick syntax
      timeout_s: 10                   # default: 30
    condition: "triage.category == 'billing'"  # JMESPath; if falsy → skipped (not failed)
    depends_on: [triage]

model_policy:
  cost_budget_usd: 0.50          # circuit breaker — rejects LLM calls when exhausted
  group_map:
    frontier: standard           # selectively remaps group names for this workflow
  force_group: local             # overrides ALL group declarations (useful for local dev)
```

## Top-Level Fields

| Field | Type | Required | Description |
|---|---|---|---|
| `kind` | string | yes | Must be `workflow` |
| `name` | string | yes | Unique workflow name; used in `POST /invoke/{name}` |
| `version` | string | yes | Semver string |
| `description` | string | no | Human-readable description |
| `input.schema` | JSON Schema | yes | Validated against the invoke request body; 422 on failure |
| `pipeline` | array | yes | Ordered list of pipeline steps |
| `model_policy.cost_budget_usd` | number | no | LLM cost circuit breaker for the run |
| `model_policy.group_map` | object | no | Remaps model group names; keys and values are group names |
| `model_policy.force_group` | string | no | Overrides ALL model group declarations to this group |

## Agent Step Fields

| Field | Type | Required | Description |
|---|---|---|---|
| `id` | string | yes | Unique step ID; referenced in `depends_on` and output expressions |
| `agent` | string | yes | Built-in: `ktsu/<name>@<ver>`; local: `./agents/foo.agent.yaml` (optional `@<ver>`) |
| `depends_on` | string[] | no | Step IDs to wait for; omit to receive workflow input |
| `confidence_threshold` | number | no | Min `ktsu_confidence` required; agent output schema must declare `ktsu_confidence` |
| `params` | object | no | Parameters for built-in agents (e.g. `ktsu/secure-parser`) |
| `model.group` | string | no | Overrides agent file's model group |
| `model.max_tokens` | number | no | Token limit override |
| `for_each.from` | string | no | JMESPath resolving to an array; runs agent once per item |
| `for_each.max_items` | number | no | Truncates array before fanout |
| `for_each.concurrency` | number | no | Max parallel invocations; default: unbounded |

## Transform Step Fields

| Field | Type | Required | Description |
|---|---|---|---|
| `id` | string | yes | Unique step ID |
| `transform.inputs` | array | yes | Upstream step references; `depends_on` is derived automatically |
| `transform.inputs[].from` | string | yes | Step ID of upstream step |
| `transform.ops` | array | yes | Ordered op list; each op receives the output of the previous |
| `output.schema` | JSON Schema | yes | Air-Lock validated |

### Transform Ops

| Op | Arguments | Description |
|---|---|---|
| `merge` | `[step-id, ...]` | Concatenates arrays or deep-merges objects (later wins on key conflict) |
| `sort` | `{ field, order }` | Sorts array by JMESPath field; `order`: `asc` \| `desc` (default: `asc`) |
| `filter` | `{ expr }` | Removes items where JMESPath `expr` is falsy |
| `map` | `{ expr }` | Projects each item to a new shape via JMESPath |
| `flatten` | `{}` | Flattens one level of array nesting |
| `deduplicate` | `{ field }` | Removes duplicates by key field; first occurrence wins |

## Webhook Step Fields

| Field | Type | Required | Description |
|---|---|---|---|
| `id` | string | yes | Unique step ID |
| `webhook.url` | string | yes | Destination URL; `env:VAR_NAME` resolved at runtime |
| `webhook.method` | string | no | HTTP method; default: `POST` |
| `webhook.body` | object | no | Key-value map; values are JMESPath against merged step outputs |
| `webhook.timeout_s` | number | no | Request timeout in seconds; default: 30 |
| `condition` | string | no | JMESPath; if falsy step is `skipped` (not failed) |
| `depends_on` | string[] | no | Step IDs to wait for |

## Notes

- Workflow input is available to all steps as `input`; reference as `input.<field>` in JMESPath expressions and `inputs.input.<field>` inside agent system prompts.
- Fanout step output is always `{ "results": [...] }` in original array order.
- Transform `depends_on` is derived from `inputs[].from` — do not declare it separately.
- Webhook non-2xx or network error → step fails immediately; no retry.
- Webhook `condition` false → step is `skipped`; downstream steps still run.
````

- [ ] **Step 2: Verify**

Confirm the file exists at `docs/yaml-spec/workflow.md`.

---

### Task 3: Create agent.md

**Files:**
- Create: `docs/yaml-spec/agent.md`

- [ ] **Step 1: Write the file**

Create `docs/yaml-spec/agent.md` with this exact content:

````markdown
# agent.yaml

**What it does:** Defines an LLM agent — its model group, system prompt, tool servers, optional sub-agents, and typed output schema.

**Filename convention:** `agents/*.agent.yaml` — no `kind` field.

## Annotated Example

```yaml
# No kind field
name: triage-agent               # identity — used in logs and metrics
description: "..."               # optional
model: standard                  # model group name from gateway.yaml
max_turns: 10                    # max reasoning turns before forced conclusion; default: 10
system: |
  You are a triage agent. The full pipeline envelope is provided as JSON input.
  Reference upstream step outputs as inputs.<step-id>.<field>.
  Workflow input fields are under inputs.input.<field>.

servers:                         # omit entirely for a toolless agent
  - name: wiki-search            # logical name — used in logs
    path: servers/wiki-search.server.yaml  # relative to project root
    access:
      allowlist:
        - wiki-search            # exact tool name
        - crm-read-*             # prefix wildcard — any tool starting with "crm-read-"
        - "*"                    # all tools this server exposes
      allowlist_env: KTSU_WIKI_ALLOWLIST  # optional — env var (comma-separated) overrides allowlist if set

agents:                          # sub-agents this agent may invoke (optional)
  - name: summarizer             # logical name
    path: agents/summarizer.agent.yaml  # relative to project root

output:
  schema:                        # JSON Schema — Air-Lock validated before downstream steps read it
    type: object
    required: [category, priority, ktsu_confidence]
    properties:
      category:        { type: string, enum: [billing, technical, legal] }
      priority:        { type: string, enum: [low, medium, high] }
      ktsu_confidence: { type: number, minimum: 0, maximum: 1 }   # reserved
      ktsu_flags:      { type: array, items: { type: string } }    # reserved
      ktsu_rationale:  { type: string }                            # reserved
```

## Fields

| Field | Type | Required | Description |
|---|---|---|---|
| `name` | string | yes | Identity — used in logs and metrics |
| `description` | string | no | Human-readable description |
| `model` | string | yes | Model group name from `gateway.yaml` |
| `max_turns` | number | no | Max reasoning turns before forced conclusion; default: 10 |
| `system` | string | yes | System prompt; reference upstream outputs as `inputs.<step-id>.<field>` |
| `servers` | array | no | Tool servers this agent may call; omit for toolless agent |
| `servers[].name` | string | yes | Logical name — used in logs |
| `servers[].path` | string | yes | Path to `.server.yaml` file, relative to project root |
| `servers[].access.allowlist` | string[] | yes | Permitted tools: exact name, `prefix-*`, or `*` |
| `servers[].access.allowlist_env` | string | no | Env var (comma-separated) that overrides `allowlist` if set |
| `agents` | array | no | Sub-agents this agent may invoke |
| `agents[].name` | string | yes | Logical name |
| `agents[].path` | string | yes | Path to `.agent.yaml` file, relative to project root |
| `output.schema` | JSON Schema | yes | Air-Lock validated before downstream steps can read this agent's output |

## Reserved Output Fields (`ktsu_` prefix)

| Field | Type | Description |
|---|---|---|
| `ktsu_confidence` | number (0–1) | Required when a pipeline step declares `confidence_threshold` |
| `ktsu_flags` | string[] | Arbitrary warning or flag strings |
| `ktsu_rationale` | string | Agent's reasoning trace |
| `ktsu_injection_attempt` | boolean | Set by `ktsu/secure-parser` if prompt injection detected |
| `ktsu_low_quality` | boolean | Set by `ktsu/secure-parser` if input quality is poor |

Any `ktsu_` field not in this list is a boot error.

## Built-in Agents

| Agent | Description |
|---|---|
| `ktsu/secure-parser@1.0.0` | Toolless hardened parser for untrusted text; parameterized by `params.extract` schema |

### `ktsu/secure-parser` params

```yaml
- id: parse
  agent: ktsu/secure-parser@1.0.0
  params:
    source_field: message      # which workflow input field contains the raw text
    extract:
      intent:
        type: string
        enum: [billing, technical, legal, other]
        description: "What the user is asking for"
  model:
    group: economy
    max_tokens: 512
```

Output always includes `ktsu_injection_attempt`, `ktsu_confidence`, `ktsu_low_quality`, and `ktsu_flags` in addition to declared `extract` fields.

## Notes

- A toolless agent (no `servers` block) has no tools to exploit — recommended as the first pipeline step when handling raw user input.
- Allowlist wildcards: `*` (all tools), `prefix-*` (prefix match). Mid-string wildcards are a boot error.
- Sub-agents cannot access servers not granted to their parent, and cannot have a wider allowlist than the parent for shared servers. Both conditions are caught at boot.
- Sub-agents do not appear in the pipeline DAG and their cost rolls up to the parent step.
````

- [ ] **Step 2: Verify**

Confirm the file exists at `docs/yaml-spec/agent.md`.

---

### Task 4: Create server.md

**Files:**
- Create: `docs/yaml-spec/server.md`

- [ ] **Step 1: Write the file**

Create `docs/yaml-spec/server.md` with this exact content:

````markdown
# server.yaml (tool-server)

**What it does:** Points to a local MCP tool server — declares its URL, auth, and trust flags. The server itself is an independent MCP process; Kimitsu does not start or manage it.

**Filename convention:** `servers/*.server.yaml`

## Annotated Example

```yaml
kind: tool-server
name: wiki-search                # identity — used in logs and error messages
description: "..."               # optional
url: "https://mcp.internal/wiki" # base URL of the MCP server
auth: "env:WIKI_TOKEN"           # bearer token or env:VAR_NAME; omit if no auth required
stateful: false                  # true if server causes external side effects (write, mutate, send)
egress: false                    # true if server makes outbound calls to external services
```

## Fields

| Field | Type | Required | Description |
|---|---|---|---|
| `kind` | string | yes | Must be `tool-server` |
| `name` | string | yes | Identity — used in logs and error messages |
| `description` | string | no | Human-readable |
| `url` | string | yes | Base URL of the MCP server |
| `auth` | string | no | Bearer token or `env:VAR_NAME`; omit if no auth required |
| `stateful` | boolean | no | `true` if the server causes external side effects; default: `false` |
| `egress` | boolean | no | `true` if the server makes outbound calls to external services; default: `false` |

## Built-in Tool Servers

Declared in agent files as `ktsu/<name>@<version>` — no `.server.yaml` file required.

### Unrestricted (available to all agents including sub-agents)

| Server | Tools | Description |
|---|---|---|
| `ktsu/format@1.0.0` | `format_json`, `format_yaml`, `format_template` | Format data |
| `ktsu/validate@1.0.0` | `validate_schema`, `validate_json` | Validate against JSON Schema |
| `ktsu/transform@1.0.0` | `transform_jmespath`, `transform_map`, `transform_filter` | JMESPath operations |
| `ktsu/cli@1.0.0` | `jq`, `grep`, `sed`, `awk`, `date`, `wc`, `diff`, `sort`, `uniq`, `cut`, `base64` | Unix CLI tools as typed MCP tools |

### Restricted (pipeline agents only — not available to sub-agents)

| Server | Tools | Description |
|---|---|---|
| `ktsu/kv@1.0.0` | `kv_get`, `kv_set`, `kv_delete` | Key-value storage scoped to agent namespace |
| `ktsu/blob@1.0.0` | `blob_get`, `blob_put`, `blob_delete`, `blob_list` | Binary/file storage |
| `ktsu/log@1.0.0` | `log_write`, `log_read`, `log_tail` | Structured run log |
| `ktsu/memory@1.0.0` | `memory_store`, `memory_retrieve`, `memory_search`, `memory_forget` | Semantic vector memory |
| `ktsu/envelope@1.0.0` | `envelope_get`, `envelope_set`, `envelope_append` | Read and write run envelope fields |

## Notes

- Local server files are referenced in agent files by path: `path: servers/wiki-search.server.yaml`
- Marketplace servers are declared in `servers.yaml` and referenced in agent files by name only
- `stateful` and `egress` are trust signals for operators and marketplace review — not enforced at the network layer
````

- [ ] **Step 2: Verify**

Confirm the file exists at `docs/yaml-spec/server.md`.

---

### Task 5: Create servers.md

**Files:**
- Create: `docs/yaml-spec/servers.md`

- [ ] **Step 1: Write the file**

Create `docs/yaml-spec/servers.md` with this exact content:

````markdown
# servers.yaml

**What it does:** Declares all marketplace tool server dependencies for the project — names, sources, and pinned versions.

**Filename convention:** `servers.yaml` (project root, singular file)

## Annotated Example

```yaml
kind: servers

servers:
  - name: sentiment-scorer       # logical name — used in agent server path references
    source: "marketplace/sentiment-scorer"  # marketplace path
    version: "3.0.1"             # pinned version

  - name: crm
    source: "marketplace/acme-crm"
    version: "2.0.0"
```

## Fields

| Field | Type | Required | Description |
|---|---|---|---|
| `kind` | string | yes | Must be `servers` |
| `servers` | array | yes | List of marketplace server dependencies |
| `servers[].name` | string | yes | Logical name — used in agent `servers[].path` as the name portion |
| `servers[].source` | string | yes | Marketplace path |
| `servers[].version` | string | yes | Pinned version |

## Notes

- Local tool server files (`servers/*.server.yaml`) and built-in servers (`ktsu/*`) are never listed here.
- Run `ktsu lock` after changing versions to regenerate `ktsu.lock.yaml`.
- Agents reference marketplace servers in their `servers` block by name only (not path).
````

- [ ] **Step 2: Verify**

Confirm the file exists at `docs/yaml-spec/servers.md`.

---

### Task 6: Create gateway.md

**Files:**
- Create: `docs/yaml-spec/gateway.md`

- [ ] **Step 1: Write the file**

Create `docs/yaml-spec/gateway.md` with this exact content:

````markdown
# gateway.yaml

**What it does:** Defines all LLM providers and named model groups. Agents declare a group name — the gateway owns provider selection, routing strategy, and credentials.

**Filename convention:** `gateway.yaml` (project root, singular file, no `kind` field)

## Annotated Example

```yaml
providers:
  - name: anthropic              # logical name — used as prefix in model references
    type: anthropic              # anthropic | openai | openai-compat
    config:
      api_key_env: ANTHROPIC_API_KEY  # env var holding the API key

  - name: openai
    type: openai
    config:
      api_key_env: OPENAI_API_KEY

model_groups:
  - name: economy                # group name — agents declare this in their model: field
    models:
      - anthropic/claude-haiku-4-5-20251001   # "provider-name/model-id"
    strategy: round_robin        # round_robin | cost_optimized; default: round_robin
    default_temperature: 0.3    # optional
    pricing:                     # optional — for cost tracking
      - model: claude-haiku-4-5-20251001
        input_per_million: 0.25
        output_per_million: 1.25

  - name: standard
    models:
      - anthropic/claude-sonnet-4-6
    strategy: round_robin

  - name: frontier
    models:
      - anthropic/claude-opus-4-6
    strategy: round_robin

  - name: vision
    models:
      - openai/gpt-4o
    strategy: round_robin
```

## Provider Fields

| Field | Type | Required | Description |
|---|---|---|---|
| `providers` | array | yes | List of LLM providers |
| `providers[].name` | string | yes | Logical name — used as prefix in model references (`name/model-id`) |
| `providers[].type` | string | yes | `anthropic` \| `openai` \| `openai-compat` |
| `providers[].config.api_key_env` | string | yes | Env var holding the API key |

## Model Group Fields

| Field | Type | Required | Description |
|---|---|---|---|
| `model_groups` | array | yes | Named groups that agents reference in their `model:` field |
| `model_groups[].name` | string | yes | Group name |
| `model_groups[].models` | string[] | yes | `provider-name/model-id` strings |
| `model_groups[].strategy` | string | no | `round_robin` \| `cost_optimized`; default: `round_robin` |
| `model_groups[].default_temperature` | number | no | Default temperature for all models in this group |
| `model_groups[].pricing` | array | no | Per-model pricing for cost tracking |
| `model_groups[].pricing[].model` | string | yes | Model ID (without provider prefix) |
| `model_groups[].pricing[].input_per_million` | number | yes | Cost per 1M input tokens (USD) |
| `model_groups[].pricing[].output_per_million` | number | yes | Cost per 1M output tokens (USD) |

## Notes

- In an agent file, `model:` is a plain string: `model: standard`
- In a workflow pipeline step, `model:` is an override block: `model: { group: economy, max_tokens: 512 }`
- `model_policy.group_map` and `model_policy.force_group` on a workflow remap groups at runtime without touching agent files
````

- [ ] **Step 2: Verify**

Confirm the file exists at `docs/yaml-spec/gateway.md`.

---

### Task 7: Create env.md

**Files:**
- Create: `docs/yaml-spec/env.md`

- [ ] **Step 1: Write the file**

Create `docs/yaml-spec/env.md` with this exact content:

````markdown
# env.yaml

**What it does:** Declares environment-specific configuration — variables injected at runtime and the state store backend. Selected at startup with `--env environments/dev.env.yaml`.

**Filename convention:** `environments/*.env.yaml`

## Annotated Example

```yaml
kind: env
name: dev                        # environment name — dev | staging | production | etc.
variables:                       # key-value pairs injected as environment variables
  OUTPUT_WEBHOOK_URL: http://localhost:9999/receive
  SOME_API_KEY: "env:REAL_KEY_FROM_SHELL"  # can reference the shell environment
state:
  driver: sqlite                 # sqlite | postgres
  dsn: /tmp/myproject/kimitsu.db # SQLite: file path | Postgres: connection string or env:VAR
```

## Fields

| Field | Type | Required | Description |
|---|---|---|---|
| `kind` | string | yes | Must be `env` |
| `name` | string | yes | Environment name (e.g. `dev`, `staging`, `production`) |
| `variables` | object | no | Key-value pairs injected as environment variables at startup |
| `state.driver` | string | yes | `sqlite` \| `postgres` |
| `state.dsn` | string | yes | SQLite: file path; Postgres: connection string or `env:VAR` |

## Notes

- `variables` entries are injected before any workflow runs; they are available to `env:VAR_NAME` references in server files, webhook URLs, and gateway config.
- Postgres DSN as env var: `dsn: "env:DATABASE_URL"`
````

- [ ] **Step 2: Verify**

Confirm the file exists at `docs/yaml-spec/env.md`.

---

### Task 8: Commit

- [ ] **Step 1: Stage and commit**

```bash
git add docs/yaml-spec/
git add docs/superpowers/specs/2026-03-26-yaml-spec-docs-design.md
git commit -m "docs: add docs/yaml-spec/ — per-kind YAML reference for coding agents"
```

Expected: commit succeeds, working tree clean.
