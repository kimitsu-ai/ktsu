# Kimitsu — Tool Servers
## Architecture & Design Reference — v3

---

## Overview

Tool servers are the atomic unit of Kimitsu. Every capability an agent has comes from a tool server. An agent with no tools can only reason about its inputs and produce output — it cannot call external services, read from storage, or cause side effects.

There are two tiers of tool server references:

| Tier | Declared in | Referenced in agent as | Versioned by |
|---|---|---|---|
| Marketplace | `servers.yaml` | name only (`sentiment-scorer`) | `servers.yaml` |
| Local | `servers/*.server.yaml` | path (`servers/wiki-search.server.yaml`) | the file itself |

The shipped tool server (envelope) is a local tool server that ships with the Kimitsu binary. It gets a `.server.yaml` file like any other local server.

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

Local tool server files (`servers/*.server.yaml`) are not listed in `servers.yaml` — they are referenced directly by path in agent files.

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
url: "http://search.internal:5050"
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
  - name: envelope
    path: servers/envelope.server.yaml
    access:
      allowlist:
        - "*"                  # all tools this server exposes
```

The `allowlist` is enforced by the Agent Runtime — the agent only ever sees tools it is permitted to call. If a tool is not on the allowlist, the Agent Runtime blocks the call and informs the agent of what tools are available.

---

## Shipped Tool Servers

Kimitsu ships first-party MCP servers as part of the binary. They are configured with `.server.yaml` files and started with `ktsu start <name>`, exactly like any other local tool server.

The envelope server has a back-channel dependency on the orchestrator — it reads from the state store via the orchestrator's internal HTTP API. This server requires `ORCHESTRATOR_URL` at startup.

### Shipped Servers

| Server | Default Port | Tools | Description |
|---|---|---|---|
| `envelope` | 9104 | `envelope_get`, `envelope_set`, `envelope_append` | Read and write run envelope fields |

---

## Tool Server Access Control

Kimitsu has two distinct access control mechanisms. They operate at different layers and serve different purposes.

### Pipeline-Agent Restriction (Stateful Servers)

The `restricted` field on a tool server controls which agent types may call it. It is enforced by the orchestrator when building invocation payloads.

| Level | Who can call | Used for |
|---|---|---|
| `restricted: false` | All agents including sub-agents | Pure, stateless tools. **Default.** |
| `restricted: true` | Pipeline agents only | Storage, context, anything with side effects or sensitive data |

Enforced at invocation time — the orchestrator only includes restricted tool server endpoints in pipeline agent invocation payloads, never in sub-agent payloads.

A sub-agent that could call envelope would be able to read sensitive context outside the visibility of the pipeline DAG. Restricting it to pipeline agents keeps the data-access surface fully visible in the agent YAML files that appear in the pipeline — auditable without tracing sub-agent chains.

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

#### Tool Call Approval Policies

Any allowlist entry can require explicit human approval before the tool call executes. Instead of a plain string, declare an object with a `require_approval` block:

```yaml
access:
  allowlist:
    - wiki-search                 # plain string — no approval required
    - name: crm-read-*            # object form — no approval (explicit)
    - name: delete-*              # any tool starting with "delete-"
      require_approval:
        on_reject: fail           # "fail" | "recover"
        timeout: 30m              # optional — duration string (e.g. 30m, 2h)
        timeout_behavior: reject  # "fail" | "reject" — what to do when timeout fires
```

When an agent attempts to call a tool that matches a `require_approval` pattern, the Agent Runtime suspends the run and sends a `pending_approval` callback to the Orchestrator. The run does not fail — it waits. A human (or external system) then issues an approval or rejection decision via the Orchestrator's REST API.

**Fields:**

| Field | Values | Description |
|---|---|---|
| `on_reject` | `fail` \| `recover` | `fail` — the step fails immediately. `recover` — the agent receives a rejection message and can try an alternative approach. |
| `timeout` | duration string | Optional. If no decision is received within this duration, `timeout_behavior` applies. |
| `timeout_behavior` | `fail` \| `reject` | Only relevant when `timeout` is set. `fail` — step fails. `reject` — treated as a rejection (respects `on_reject`). |

**Approval REST API** (on the Orchestrator):

```
GET  /approvals                                  # list all pending approvals
GET  /runs/{run_id}/steps/{step_id}/approval     # get approval for a specific step
POST /runs/{run_id}/steps/{step_id}/approval/decide
     Body: {"decision": "approved" | "rejected"}
```

Boot validation rejects `require_approval` blocks with invalid `on_reject` or `timeout_behavior` values.

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

The allowlist narrows the callable surface. Container-level constraints (no network egress, resource limits, execution timeouts) restrict what permitted tools can do with their access. Neither layer alone is a hard sandbox. For sensitive contexts, both are required.

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
