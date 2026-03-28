# Kimitsu — Tool Servers
## Architecture & Design Reference — v3

---

## Overview

Tool servers are the atomic unit of Kimitsu. Every capability an agent has comes from a tool server. An agent with no tools can only reason about its inputs and produce output — it cannot call external services, read from storage, or cause side effects.

There are three tiers of tool server references:

| Tier | Declared in | Referenced in agent as | Versioned by |
|---|---|---|---|
| Marketplace | `servers.yaml` | name only (`sentiment-scorer`) | `servers.yaml` |
| Local | `servers/*.server.yaml` | path (`./servers/wiki-search.server.yaml`) | the file itself |
| Built-in | shipped with Kimitsu | name only (`ktsu/kv`) | Kimitsu release |

---

## `servers.yaml` — The Marketplace Manifest

`servers.yaml` is the project-level manifest for all marketplace tool servers. It is the single place where external tool server dependencies are declared and versioned. Agents reference marketplace servers by name only — version and source are resolved here.

```yaml
kind: servers

servers:
  - name:    sentiment-scorer
    source:  "marketplace/sentiment-scorer"
    version: "3.0.1"

  - name:    crm
    source:  "marketplace/acme-crm"
    version: "2.0.0"
```

Local tool server files and built-in servers (`ktsu/*`) are never listed in `servers.yaml`.

---

## Local Tool Server Files

Local tool server files live in `servers/` and are referenced directly by path from agent files. They declare where the MCP server lives and how to authenticate against it. The server itself is an independent MCP process that Kimitsu does not manage or start — it must be running and reachable at the declared URL via HTTP/SSE. Kimitsu does not support the `stdio` transport.

### Fields

```yaml
name: <string>         # identity used in logs and error messages
description: <string>  # human-readable (optional)
url: <string>          # base URL of the MCP server (HTTP/SSE)
auth: <string>         # bearer token or env:VAR_NAME — omit if no auth required
```

### Example

```yaml
name: wiki-search
description: Search the internal wiki by keyword.
url: "https://mcp.internal/wiki"
auth: "env:WIKI_TOKEN"
```

```yaml
name: github
description: GitHub API via MCP.
url: "http://localhost:3001"
auth: "env:GITHUB_TOKEN"
```

A server file with no auth:
```yaml
name: public-search
url: "http://search.internal:8080"
```

### How Agents Reference Tool Servers

Tool servers are declared in the agent file under `servers:`. Each entry provides a path to the `.server.yaml` file and an `access.allowlist` controlling which tools the agent may call on that server.

```yaml
servers:
  - name: wiki-search
    path: servers/wiki-search.server.yaml   # relative to project root
    access:
      allowlist:
        - wiki-search          # exact tool name
  - name: crm
    path: servers/crm.server.yaml
    access:
      allowlist:
        - crm-lookup           # exact name
        - crm-read-*           # prefix wildcard
  - name: kv
    path: servers/kv.server.yaml
    access:
      allowlist:
        - "*"                  # all tools this server exposes
```

The `allowlist` is enforced by the Agent Runtime — the agent only ever sees tools it is permitted to call. If a tool is not on the allowlist, the Agent Runtime blocks the call and informs the agent of what tools are available.

---

## Built-in Tool Servers

Built-in tool servers are first-party MCP servers shipped as standalone Docker images by Kimitsu. They run on the internal network with well-known service names. Stateful built-in servers write to the orchestrator's state store via the orchestrator's internal HTTP API — the orchestrator remains the single writer to the database.

### Unrestricted — Available to All Agents

| Server | Tools | Description |
|---|---|---|
| `ktsu/format@1.0.0` | `format_json`, `format_yaml`, `format_template` | Format data as JSON, YAML, or Go template |
| `ktsu/validate@1.0.0` | `validate_schema`, `validate_json` | Validate data against a JSON Schema or check JSON syntax |
| `ktsu/transform@1.0.0` | `transform_jmespath`, `transform_map`, `transform_filter` | JMESPath operations over structured data |

### Restricted — Pipeline Agents Only

Only pipeline agents (not sub-agents) may declare these. The orchestrator does not include these endpoints in sub-agent invocation payloads.

| Server | Tools | Description |
|---|---|---|
| `ktsu/kv@1.0.0` | `kv_get`, `kv_set`, `kv_delete` | Key-value storage scoped to agent namespace |
| `ktsu/blob@1.0.0` | `blob_get`, `blob_put`, `blob_delete`, `blob_list` | Binary / file storage |
| `ktsu/log@1.0.0` | `log_write`, `log_read`, `log_tail` | Structured run log |
| `ktsu/memory@1.0.0` | `memory_store`, `memory_retrieve`, `memory_search`, `memory_forget` | Semantic vector memory (similarity search) |
| `ktsu/envelope@1.0.0` | `envelope_get`, `envelope_set`, `envelope_append` | Read and write run envelope fields |

**Why `ktsu/envelope` is restricted:** Although envelope reads have no side effects, they expose trigger context that may contain PII or sensitive routing information. Restricting access to pipeline agents prevents sub-agents from leaking this context into downstream tool calls or marketplace tool servers.

### KV Scoping

The orchestrator automatically namespaces KV keys under the calling agent's `step_id`. Two agents calling `kv-set` with the same key name do not collide.

### Built-in Tool Server Versioning

Built-in tool servers are versioned and shipped as independent images. When a new image ships `ktsu/kv@2.0.0`, existing workflows referencing `@1.0.0` continue to work during a deprecation window. After the deprecation window, the old version is removed and builds referencing it fail with a clear migration error.

---

## Tool Server Access Control

Kimitsu has two distinct access control mechanisms. They operate at different layers and serve different purposes.

### Pipeline-Agent Restriction (Built-in Servers)

The `restricted` field on a built-in tool server controls which agent types may call it. It is enforced by the orchestrator when building invocation payloads.

| Level | Who can call | Used for |
|---|---|---|
| `restricted: false` | All agents including sub-agents | Pure, stateless tools. **Default.** |
| `restricted: true` | Pipeline agents only | Storage, context, anything with side effects or sensitive data |

Enforced at invocation time — the orchestrator only includes restricted tool server endpoints in pipeline agent invocation payloads, never in sub-agent payloads. Third-party tool servers cannot self-restrict; the `restricted` flag is only meaningful on built-in tool servers managed by Kimitsu.

A sub-agent that could call `ktsu/kv` or `ktsu/envelope` would be able to cause side effects or read sensitive context outside the visibility of the pipeline DAG. Restricting these to pipeline agents keeps the side-effect and data-access surface fully visible in the agent YAML files that appear in the pipeline — auditable without tracing sub-agent chains.

### Tool-Level Access Policy (All Servers)

The `access` block in a tool server file controls which individual tools within a server an agent may call. This applies to all server types — built-in, local, and marketplace. It is enforced by the Agent Runtime's MCP client, not the server itself, which means it applies uniformly regardless of whether the server implements its own restrictions.

#### The `allowlist` Field

Allowlist only — no blocklist. Explicit permit is the only mode. The failure mode of a misconfigured allowlist is safe: it silently permits nothing rather than accidentally permitting something.

```yaml
access:
  allowlist:
    - "crm-lookup"      # exact tool name
    - "crm-read-*"      # prefix wildcard — any tool starting with "crm-read-"
    - "*"               # permit all tools the server exposes
```

**Wildcard rules:**
- `*` alone — permits all tools the server exposes. Equivalent to omitting the `access` block entirely, but explicit about intent.
- `prefix-*` — permits any tool whose name starts with the given prefix.
- Exact names — literal match only.
- Mid-string wildcards (`crm-*-customer`) and regex are not valid. Boot validation rejects them.

Omitting the `access` block entirely is equivalent to `allowlist: ["*"]` — all tools are permitted. For sensitive servers, declaring an explicit allowlist is strongly recommended.

#### Environment Overrides

The allowlist can be overridden per environment via environment variables, without touching the tool server file:

```yaml
access:
  allowlist:
    - "crm-read-*"
  allowlist_env: KTSU_CRM_ALLOWLIST    # comma-separated, overrides allowlist if set
```

Use this to tighten the permitted set in production without modifying shared server files.

#### Enforcement Layers

Tool-level access is enforced at two points, in order:

1. **At invocation setup.** The Agent Runtime calls `tools/list` on the MCP server, then prunes the result against the allowlist before building the agent's context. The agent only ever sees tools it is permitted to call. This happens once per invocation; the pruned list is cached for the invocation lifetime.

2. **At call time.** If an agent attempts to call a tool not on the pruned list, the Agent Runtime blocks the call before it reaches the server and returns a structured error to the agent's reasoning loop:

```json
{
  "error": "tool_not_permitted",
  "tool": "crm-update",
  "message": "This tool is not available in the current execution context.",
  "available": ["crm-lookup", "crm-read-account", "crm-read-invoice"]
}
```

The `available` list is included so the agent can reason about alternatives and pivot without unnecessary escalation. All `tool_not_permitted` events are recorded in `skill_calls` with the error code, making them queryable across runs.

#### Sub-Agent Policy Inheritance

Sub-agents cannot have a broader effective allowlist than their parent. When the orchestrator builds a sub-agent invocation, it resolves the sub-agent's effective server access by intersecting its declared policy with the parent's granted set. Two rules apply:

- **Ungranted server:** If a sub-agent references a server (by endpoint URL) that the parent was not granted, the sub-agent has no access to that server at all. This includes version differences — a sub-agent on `http://api.crm.internal/mcp/v2` cannot access that endpoint if the parent was only granted `http://api.crm.internal/mcp/v1`.
- **Wider allowlist:** If a sub-agent declares a wider allowlist than the parent for a shared server, the effective allowlist is the intersection. The parent's constraint is the ceiling.

Both conditions are caught at boot — they are never silently resolved at runtime. See Boot Validation below for error examples.

#### Security Posture

The allowlist narrows the callable surface. Container-level constraints (no network egress, resource limits, execution timeouts) restrict what permitted tools can do with their access. Neither layer alone is a hard sandbox. For sensitive contexts, both are required. The docs for `ktsu/cli` describe this in detail.

---

## `ktsu/cli` — CLI Tool Server

`ktsu/cli` is a built-in tool server that wraps standard Unix CLI tools as typed MCP tools. Agents call CLI utilities the same way they call any other tool — over HTTP/SSE via MCP, same protocol, same mental model.

### Why a Server, Not Direct Invocation

Calling CLI tools directly from an agent reasoning loop has no interface contract, no typed inputs, no audit trail, and no access policy. Wrapping them as a tool server gives you all of that for free: typed inputs validated before execution, every call recorded in `skill_calls`, allowlist enforcement by the Agent Runtime, and container-level isolation.

### Standard Image

`ktsu/cli@1.0.0` ships with a curated set of tools covering the most common pipeline needs:

| Tool | Description |
|---|---|
| `jq` | Filter and transform JSON |
| `grep` | Search text by pattern |
| `sed` | Stream text editing |
| `awk` | Field extraction and text processing |
| `date` | Date formatting and arithmetic |
| `wc` | Word, line, and byte counting |
| `diff` | Compare two text inputs |
| `sort` | Sort lines |
| `uniq` | Deduplicate adjacent lines |
| `cut` | Extract fields by delimiter |
| `base64` | Encode and decode base64 |
| `jq` | JSON filter and transform |

Each tool is exposed as a named MCP tool with typed inputs — the server never accepts a raw shell command string. The agent provides values for the declared input fields; the server constructs the full command internally. No shell interpolation, no passthrough of arbitrary strings.

### Referencing the Standard Image

```yaml
# servers/cli.server.yaml
name: cli
description: Standard Unix CLI tools.
url: "http://ktsu-cli:8080"
```

Then in your agent, reference it with an allowlist for just the tools you need:

```yaml
servers:
  - name: cli
    path: servers/cli.server.yaml
    access:
      allowlist: [jq, date, wc]
```

And in Docker Compose:

```yaml
  ktsu-cli:
    image: ktsu/cli:1.0.0
    # no ORCHESTRATOR_URL — cli is stateless, no back-channel needed
    # no network egress by default
```

### Custom CLI Image

To add tools not in the standard image, extend `ktsu/cli` as a base:

```dockerfile
# Dockerfile
FROM ktsu/cli:1.0.0
RUN apt-get update && apt-get install -y \
    imagemagick \
    ghostscript \
    pandoc
```

Then declare a local tool server file pointing at your custom image:

```yaml
# servers/cli-custom.server.yaml
name: cli-custom
description: Extended CLI tools — image and document processing.
url: "http://cli-custom:8080"
```

And in Docker Compose:

```yaml
  cli-custom:
    image: myproject/cli-custom:1.0.0
    build:
      context: ./docker/cli-custom
```

The agent references `"./servers/cli-custom.server.yaml"` exactly like any other local tool server. No new concepts.

### Container Constraints

`ktsu/cli` and custom CLI images run with the following constraints enforced at the container level:

- **No network egress.** The container has no outbound internet access. Tools that make network calls (curl, wget) are not included in the standard image and should not be added to custom images unless explicitly needed and declared with `egress: true`.
- **Read-only filesystem.** The container filesystem is read-only except for a tightly scoped scratch directory mounted at `/tmp/ktsu-scratch`. Tool outputs that need to persist between calls should be written to `ktsu/blob` via the parent agent.
- **Execution timeout.** Each tool call has a maximum execution time (default: 30s). Runaway processes are killed and the tool call fails with `execution_timeout`.
- **Resource limits.** CPU and memory limits are set at the container level via Docker or the orchestrator's container runtime configuration.

The allowlist narrows which tools can be called. The container constraints limit what those tools can do. Both layers are required — neither alone is sufficient for a sensitive deployment.

---

## Marketplace & Trust Model

### The `stateful` Field

Tool servers that cause external side effects (writing to a database, sending an email, modifying a CRM record) must declare `stateful: true`. This is a trust signal for operators and the marketplace review process, not an enforcement mechanism at the network level.

1. **Auditability.** The lockfile shows every `stateful: true` tool server in the resolved tree.
2. **Marketplace review.** Tool servers published to the marketplace must declare `stateful` accurately. Misdeclaration is a policy violation subject to removal.

### The `egress` Field

Tool servers that make outbound calls to external services must declare `egress: true`. This is an operator signal — it tells the person deploying the server that it needs outbound network access. Kimitsu does not enforce this at the network layer since it does not manage tool server deployment.

### The Two Fields are Orthogonal

| | `stateful: false` | `stateful: true` |
|---|---|---|
| `egress: false` | Pure read-only, internal only (wiki-search) | Internal write (internal CRM updater) |
| `egress: true` | External read-only (web search) | External write (slack-reply, webhook-fire) |

### Marketplace Publishing Requirements

- Tool servers must declare `stateful` accurately.
- Tool servers must declare `egress` accurately.
- Tool servers must expose at least one tool with a valid typed interface.
- The marketplace runs the tool server's declared interface against a test harness before publishing.

---

*Revised from design session — March 2026*
