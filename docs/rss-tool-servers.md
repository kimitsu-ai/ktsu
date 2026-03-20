# RSS — Tool Servers
## Architecture & Design Reference — v3

---

## Overview

Tool servers are the atomic unit of RSS. Every capability an agent has comes from a tool server. An agent with no tools can only reason about its inputs and produce output — it cannot call external services, read from storage, or cause side effects.

There are three tiers of tool server references:

| Tier | Declared in | Referenced in agent as | Versioned by |
|---|---|---|---|
| Marketplace | `servers.yaml` | name only (`sentiment-scorer`) | `servers.yaml` |
| Local | `servers/*.server.yaml` | path (`./servers/wiki-search.server.yaml`) | the file itself |
| Built-in | shipped with RSS | name only (`rss/kv`) | RSS release |

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

Local tool server files and built-in servers (`rss/*`) are never listed in `servers.yaml`.

---

## Local Tool Server Files

Local tool server files live in `servers/` and are referenced directly by path from agent files. They declare where the server lives, how to authenticate, and the typed interface contract for each tool the server exposes.

### Fields

```yaml
kind: tool-server
name        # identity
version     # semver
description # human-readable
server      # MCP server URL
auth        # authentication — always env:VAR_NAME
egress      # true if the server makes outbound calls
stateful    # true if the server causes external side effects
access      # optional — allowlist of permitted tool names
tools       # list of tools with typed interface contracts
changelog
```

### Single-Tool Example

```yaml
kind: tool-server
name: "wiki-search"
version: "2.1.0"
description: "Search the internal wiki by keyword. Returns ranked results."

server: "https://mcp.internal/wiki"
auth:   "env:WIKI_TOKEN"

egress:   true
stateful: false

tools:
  - name: wiki-search
    description: "Search the wiki by keyword."
    input:
      query:       { type: string,  required: true }
      max_results: { type: integer, default: 5 }
    output:
      results: { type: array, items: { type: string } }
      total:   { type: integer }

changelog:
  "2.1.0": "Added max_results param. Improved ranking."
  "2.0.0": "Breaking: output is now array not string."
  "1.0.0": "Initial release."
```

### Multi-Tool Example

```yaml
kind: tool-server
name: "crm"
version: "1.0.0"
description: "CRM read and write operations."

server: "https://api.crm.internal/mcp"
auth:   "env:CRM_KEY"

egress:   true
stateful: true

access:
  allowlist:
    - "crm-lookup"
    - "crm-read-*"

tools:
  - name: crm-lookup
    description: "Look up a customer record by ID."
    input:
      customer_id: { type: string, required: true }
    output:
      name: { type: string }
      tier: { type: string }

  - name: crm-update
    description: "Update a customer record field."
    input:
      customer_id: { type: string, required: true }
      field:       { type: string, required: true }
      value:       { type: string, required: true }
    output:
      success: { type: boolean }

changelog:
  "1.0.0": "Initial release."
```

### How Agents Reference Tool Servers

```yaml
tools:
  - sentiment-scorer                      # marketplace — resolved via servers.yaml
  - crm                                   # marketplace — resolved via servers.yaml
  - "./servers/wiki-search.server.yaml"   # local — resolved by path
  - rss/kv                                # built-in — always available
```

---

## Built-in Tool Servers

Built-in tool servers are first-party MCP servers shipped as standalone Docker images by RSS. They run on the internal network with well-known service names. Stateful built-in servers write to the orchestrator's state store via the orchestrator's internal HTTP API — the orchestrator remains the single writer to the database.

### Unrestricted — Available to All Agents

| Server | Tools | Description |
|---|---|---|
| `rss/format@1.0.0` | `format` | Format data as JSON, markdown, CSV |
| `rss/validate@1.0.0` | `validate` | Validate data against a JSON schema |
| `rss/transform@1.0.0` | `transform` | Map / filter / reduce over structured data |

### Restricted — Pipeline Agents Only

Only pipeline agents (not sub-agents) may declare these. The orchestrator does not include these endpoints in sub-agent invocation payloads.

| Server | Tools | Description |
|---|---|---|
| `rss/kv@1.0.0` | `kv-get`, `kv-set`, `kv-delete`, `kv-list` | Key-value storage scoped to agent namespace |
| `rss/blob@1.0.0` | `blob-put`, `blob-get`, `blob-delete` | Binary / file storage |
| `rss/log@1.0.0` | `log-append` | Append-only structured run log |
| `rss/memory@1.0.0` | `memory-store`, `memory-search` | Semantic vector memory (similarity search) |
| `rss/envelope@1.0.0` | `envelope-get-inlet`, `envelope-get-run` | Read run context and trigger metadata (read-only) |

**Why `rss/envelope` is restricted:** Although envelope reads have no side effects, they expose trigger context that may contain PII or sensitive routing information. Restricting access to pipeline agents prevents sub-agents from leaking this context into downstream tool calls or marketplace tool servers.

### KV Scoping

The orchestrator automatically namespaces KV keys under the calling agent's `step_id`. Two agents calling `kv-set` with the same key name do not collide.

### Built-in Tool Server Versioning

Built-in tool servers are versioned and shipped as independent images. When a new image ships `rss/kv@2.0.0`, existing workflows referencing `@1.0.0` continue to work during a deprecation window. After the deprecation window, the old version is removed and builds referencing it fail with a clear migration error.

---

## Tool Server Access Control

RSS has two distinct access control mechanisms. They operate at different layers and serve different purposes.

### Pipeline-Agent Restriction (Built-in Servers)

The `restricted` field on a built-in tool server controls which agent types may call it. It is enforced by the orchestrator when building invocation payloads.

| Level | Who can call | Used for |
|---|---|---|
| `restricted: false` | All agents including sub-agents | Pure, stateless tools. **Default.** |
| `restricted: true` | Pipeline agents only | Storage, context, anything with side effects or sensitive data |

Enforced at invocation time — the orchestrator only includes restricted tool server endpoints in pipeline agent invocation payloads, never in sub-agent payloads. Third-party tool servers cannot self-restrict; the `restricted` flag is only meaningful on built-in tool servers managed by RSS.

A sub-agent that could call `rss/kv` or `rss/envelope` would be able to cause side effects or read sensitive context outside the visibility of the pipeline DAG. Restricting these to pipeline agents keeps the side-effect and data-access surface fully visible in the agent YAML files that appear in the pipeline — auditable without tracing sub-agent chains.

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
  allowlist_env: RSS_CRM_ALLOWLIST    # comma-separated, overrides allowlist if set
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

The allowlist narrows the callable surface. Container-level constraints (no network egress, resource limits, execution timeouts) restrict what permitted tools can do with their access. Neither layer alone is a hard sandbox. For sensitive contexts, both are required. The docs for `rss/cli` describe this in detail.

---

## `rss/cli` — CLI Tool Server

`rss/cli` is a built-in tool server that wraps standard Unix CLI tools as typed MCP tools. Agents call CLI utilities the same way they call any other tool — over HTTP via MCP, same protocol, same mental model.

### Why a Server, Not Direct Invocation

Calling CLI tools directly from an agent reasoning loop has no interface contract, no typed inputs, no audit trail, and no access policy. Wrapping them as a tool server gives you all of that for free: typed inputs validated before execution, every call recorded in `skill_calls`, allowlist enforcement by the Agent Runtime, and container-level isolation.

### Standard Image

`rss/cli@1.0.0` ships with a curated set of tools covering the most common pipeline needs:

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
kind: tool-server
name: "cli"
version: "1.0.0"
description: "Standard Unix CLI tools."

server: "http://rss-cli:8080"
auth:   none
egress: false
stateful: false

access:
  allowlist:
    - "jq"
    - "date"
    - "wc"

tools:
  - name: jq
    description: "Filter and transform a JSON string using a jq expression."
    input:
      json:   { type: string, required: true }
      filter: { type: string, required: true }
    output:
      result: { type: string }
      error:  { type: string }

  - name: date
    description: "Format or compute a date."
    input:
      format: { type: string, required: true }
      input:  { type: string }           # ISO 8601 — defaults to now if omitted
    output:
      result: { type: string }

  - name: wc
    description: "Count lines, words, or bytes in a string."
    input:
      input: { type: string, required: true }
      mode:  { type: string, enum: [lines, words, bytes], default: lines }
    output:
      count: { type: integer }
```

And in Docker Compose:

```yaml
  rss-cli:
    image: rss/cli:1.0.0
    # no ORCHESTRATOR_URL — cli is stateless, no back-channel needed
    # no network egress by default
```

### Custom CLI Image

To add tools not in the standard image, extend `rss/cli` as a base:

```dockerfile
# Dockerfile
FROM rss/cli:1.0.0
RUN apt-get update && apt-get install -y \
    imagemagick \
    ghostscript \
    pandoc
```

Then declare a local tool server file pointing at your custom image and listing the additional tools:

```yaml
# servers/cli-custom.server.yaml
kind: tool-server
name: "cli-custom"
version: "1.0.0"
description: "Extended CLI tools — image and document processing."

server: "http://cli-custom:8080"
auth:   none
egress: false
stateful: false

access:
  allowlist:
    - "imagemagick-convert"
    - "ghostscript-compress"
    - "pandoc-convert"

tools:
  - name: imagemagick-convert
    description: "Convert or resize an image."
    input:
      input_path:  { type: string, required: true }
      output_path: { type: string, required: true }
      resize:      { type: string }          # e.g. "800x600"
      format:      { type: string }          # e.g. "png", "jpg"
    output:
      success:     { type: boolean }
      output_path: { type: string }

  - name: ghostscript-compress
    description: "Compress a PDF using Ghostscript."
    input:
      input_path:  { type: string, required: true }
      output_path: { type: string, required: true }
      quality:     { type: string, enum: [screen, ebook, printer, prepress], default: ebook }
    output:
      success:       { type: boolean }
      size_before:   { type: integer }
      size_after:    { type: integer }

  - name: pandoc-convert
    description: "Convert a document between formats using Pandoc."
    input:
      input:        { type: string, required: true }
      from_format:  { type: string, required: true }
      to_format:    { type: string, required: true }
    output:
      result: { type: string }

changelog:
  "1.0.0": "Initial release."
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

`rss/cli` and custom CLI images run with the following constraints enforced at the container level:

- **No network egress.** The container has no outbound internet access. Tools that make network calls (curl, wget) are not included in the standard image and should not be added to custom images unless explicitly needed and declared with `egress: true`.
- **Read-only filesystem.** The container filesystem is read-only except for a tightly scoped scratch directory mounted at `/tmp/rss-scratch`. Tool outputs that need to persist between calls should be written to `rss/blob` via the parent agent.
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

Tool servers that make outbound calls to external services must declare `egress: true`. This is an operator signal — it tells the person deploying the server that it needs outbound network access. RSS does not enforce this at the network layer since it does not manage tool server deployment.

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
