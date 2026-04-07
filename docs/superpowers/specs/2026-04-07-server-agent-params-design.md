# Server & Agent Params Design

**Date:** 2026-04-07

## Problem

Agents and tool servers today have no mechanism for parameterization. Agent system prompts are static strings. Tool servers have no way to receive per-invocation configuration (e.g. a memory server's namespace). This makes it impossible to share a single agent or server definition across workflows that need different runtime behavior.

## Solution Overview

Introduce a top-level `params` block on both `agent.yaml` and `server.yaml`. Params are named, described, and optionally have defaults. Values are resolved at workflow step level and flow into two distinct runtime mechanisms:

- **Agent params** → interpolated into `prompt.system` at invocation time
- **Server params** → passed as MCP initialization config when the runtime connects to the server

These two uses are independent. Params are a first-class concept on both file types, not owned by the prompt. `auth` remains a separate field on `server.yaml` — it operates at the HTTP transport layer (Authorization header) and is not a param.

---

## Schema Changes

### `agent.yaml`

`system:` moves from a top-level field into a `prompt:` block. `params:` is added as a new top-level block.

```yaml
name: chat
model: standard
max_turns: 10

params:
  persona:
    description: "The persona the agent adopts"
    default: "helpful assistant"
  domain:
    description: "The subject matter domain"   # no default — required

prompt:
  system: |
    You are a {{persona}} assistant focused on {{domain}}.
    The full pipeline envelope is provided as JSON in the first user message.

output:
  schema:
    type: object
    required: [reply]
    properties:
      reply: { type: string }
```

### `server.yaml`

`params:` is added as a new top-level block. Server files have no `prompt:` block — server params are not LLM-facing.

```yaml
name: memory
description: "Scoped memory store"
url: "http://localhost:9200"
auth: "env:MEMORY_TOKEN"

params:
  namespace:
    description: "The memory namespace to scope all reads and writes"
    default: "global"
```

---

## Param Declaration

Each param is an object with:

| Field | Required | Description |
|---|---|---|
| `description` | yes | Human-readable explanation of what the param controls |
| `default` | no | Default value used when no value is provided in the resolution chain. If absent, the param is required. |

Param values (including defaults) may use `env:VAR_NAME` syntax to resolve from the environment at runtime — consistent with how `auth` works today.

```yaml
params:
  api_key:
    description: "API key for the upstream service"
    default: "env:MY_SERVICE_API_KEY"
```

---

## Value Resolution

### Agent params

Values are set in the workflow step under `params.agent.*`:

```yaml
# workflow.yaml
- id: chat
  agent: agents/chat.agent.yaml
  params:
    agent:
      persona: "customer support rep"
      domain: "billing"
```

Resolution order (last wins):
1. Default declared in `agent.yaml`
2. `params.agent.*` in the workflow step

### Server params

Values are set in the agent's server reference and optionally overridden per workflow step:

```yaml
# agent.yaml — agent-level value for the server param
servers:
  - name: memory
    path: servers/memory.server.yaml
    params:
      namespace: "default-ns"
    access:
      allowlist: ["*"]
```

```yaml
# workflow.yaml — per-step override
- id: recall
  agent: agents/recall.agent.yaml
  params:
    server:
      memory:
        namespace: "env:USER_NAMESPACE"
```

Resolution order (last wins):
1. Default declared in `server.yaml`
2. `params` on the server reference in `agent.yaml`
3. `params.server.<name>.*` in the workflow step

---

## Prompt Interpolation

`prompt.system` in `agent.yaml` may reference declared params using `{{param_name}}` syntax. Interpolation is performed by the orchestrator before the system prompt is sent to the runtime, using the fully resolved param values.

Server files have no `prompt:` block — their params are passed exclusively as MCP initialization config and are never interpolated into any prompt.

---

## MCP Initialization Config

When the runtime connects to a tool server, resolved server params are passed as configuration in the MCP `initialize` request. The server uses these to scope its behavior (e.g. selecting the correct namespace for memory operations).

`auth` is separate — it is passed as an HTTP `Authorization` header and is not included in MCP initialization config.

---

## Boot Validation Rules

| Rule | Error type |
|---|---|
| `{{param}}` in `prompt.system` not declared in `params` | boot error |
| Required param (no `default`) not satisfied anywhere in resolution chain | boot error |
| `params.server.<name>` references a server name not declared in `agent.yaml` servers | boot error |
| `params.agent.*` key not declared in `agent.yaml` `params` | boot error |
| `env:VAR_NAME` param value references an unset environment variable | boot error |

---

## Breaking Changes

- `system:` in `agent.yaml` moves to `prompt.system`. All existing agent files must be updated.
- Built-in agent params (e.g. `secure-parser`'s `source_field`, `extract`) move from flat `params.*` to `params.agent.*` in workflow steps. All existing workflow files using built-in agent params must be updated.

---

## Affected Docs

- `docs/yaml-spec/agent.md` — update `system:` field, add `params:` and `prompt:` blocks, update `servers[].params`
- `docs/yaml-spec/server.md` — add `params:` block
- `docs/yaml-spec/workflow.md` — update `params` field with `agent`/`server` namespacing
- `docs/kimitsu-tool-servers.md` — add server params and MCP initialization config section
- `examples/hello/` — update agent and server yaml files to use new schema; add params usage to demonstrate the feature
