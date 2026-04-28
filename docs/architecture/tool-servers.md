---
description: "MCP tool server integration: server types, YAML configuration, parameter resolution order, secret propagation rules, tool allowlists, and human-in-the-loop approval flow."
---

# Tool Servers

---

## Overview

Tool servers provide the atomic capabilities of Kimitsu. Every tool an agent uses — from searching a wiki to updating a CRM — is exposed by an independent **Model Context Protocol (MCP)** server.

### Server Types

1.  **Marketplace Servers**: Defined in `servers.yaml` manifest. Referenced by name.
2.  **Local Servers**: Defined in `servers/*.server.yaml`. Referenced by path.

---

## Tool Server Configuration

Every tool server is defined by a YAML file that specifies its URL, authentication, and parameters.

```yaml
# servers/wiki.server.yaml
name: wiki-search
description: "Search internal wiki"
url: "https://mcp.internal/wiki"

auth:                            # optional outbound authentication
  header: X-Api-Key              # custom header name
  scheme: raw                    # "bearer" or "raw"
  secret: "{{ params.api_key }}" # reference a secret parameter (REQUIRED if auth set)

params:                          # parameters passed to the server at initialization
  api_key:
    description: "Cloud API Key"
    secret: true                 # Marks the param as sensitive
  region:
    description: "Cloud Region"
    default: "us-east-1"
```

### Parameter Resolution Order
1.  **Default Value**: Defined in the `.server.yaml` file.
2.  **Agent Override**: Defined in the `agent.yaml` server reference.
3.  **Workflow Override**: Defined in the workflow pipeline step `params` (highest priority).

---

## Variable Substitution

Tool server definitions support the following syntax for parameters:

- **`{{ params.NAME }}`**: References a declared parameter in the `params:` block.
- **`env:VAR_NAME`**: **NO LONGER SUPPORTED**. Secrets must be passed through the parameter chain.

### Secret Propagation Rules

To prevent accidental credential leakage, Kimitsu enforces strict "secret marking":
1. The **Workflow** must declare the environment variable as `secret: true`.
2. The **Agent** must receive it in a parameter marked `secret: true`.
3. The **Server** must receive it in a parameter marked `secret: true`.
4. The **Auth** block must finally use the secret parameter.

---

## Access Control

### Tool Allowlist

Agents are restricted to a specific set of tools on a server.

```yaml
# In an agent file
servers:
  - name: crm
    path: servers/crm.server.yaml
    access:
      allowlist:
        - crm-read-*           # prefix wildcard
        - customer-lookup      # exact name
        - name: delete-record  # object form with approval logic
          require_approval:
            on_reject: fail
```

### Human-in-the-loop (HITL)

Tool calls can be gated on human approval. When a tool with `require_approval` is invoked, the run pauses, and the agent runtime waits for a decision from the orchestrator.

### Human-in-the-Loop Approval Flow

1. **Pause** — When the agent attempts a tool call that matches a `require_approval` rule, the agent loop pauses immediately before execution. The run enters a `pending_approval` state.
2. **Inspect** — The Orchestrator surfaces the pending approval through the HTTP API:
   - `GET /runs/{id}` — returns the run envelope, including the pending tool call and its arguments.
   - `POST /runs/{id}/approve` — approves the call; execution resumes with the tool result.
   - `POST /runs/{id}/reject` — rejects the call; the agent receives a rejection signal.
3. **Resume or recover** — On approval, the tool executes and the agent loop continues normally. On rejection, the agent receives a structured rejection signal and resolves the situation according to `on_reject`: `fail` terminates the step immediately; other values allow the agent to retry, skip, or degrade gracefully.

**Example — requiring approval before any file write:**

```yaml
# In agent.yaml
servers:
  - name: filesystem
    path: servers/filesystem.server.yaml
    access:
      allowlist:
        - fs-read-*
        - name: fs-write-file
          require_approval:
            on_reject: fail
```

---

*Revised April 2026*
