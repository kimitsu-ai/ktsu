# server.yaml (tool-server)

**What it does:** Points to a local MCP tool server — declares its URL, auth, and trust flags. The server itself is an independent MCP process reachable over HTTP/SSE; Kimitsu does not start or manage it. Kiimitsu does not support the `stdio` transport.

**Filename convention:** `servers/*.server.yaml`

## Annotated Example

```yaml
name: wiki-search                # identity — used in logs and error messages
description: "..."               # optional
url: "https://mcp.internal/wiki" # base URL of the MCP server (HTTP/SSE)
auth: "env:WIKI_TOKEN"           # bearer token or env:VAR_NAME; omit if no auth required
```

## Fields

| Field | Type | Required | Description |
|---|---|---|---|
| `name` | string | yes | Identity — used in logs and error messages |
| `description` | string | no | Human-readable |
| `url` | string | yes | Base URL of the MCP server (HTTP/SSE) |
| `auth` | string | no | Bearer token or `env:VAR_NAME`; omit if no auth required |

## Shipped Tool Servers

Kimitsu ships tool servers that run as standard MCP servers on default ports. They are started with `ktsu start <name>` and configured with `.server.yaml` files like any other tool server.

| Server | Default Port | Tools | Description |
|---|---|---|---|
| `kv` | 9100 | `kv_get`, `kv_set`, `kv_delete` | Key-value storage scoped to agent namespace |
| `blob` | 9101 | `blob_get`, `blob_put`, `blob_delete`, `blob_list` | Binary/file storage |
| `log` | 9102 | `log_write`, `log_read`, `log_tail` | Structured run log |
| `memory` | 9103 | `memory_store`, `memory_retrieve`, `memory_search`, `memory_forget` | Semantic vector memory |
| `envelope` | 9104 | `envelope_get`, `envelope_set`, `envelope_append` | Read and write run envelope fields |

## Notes

- Local server files are referenced in agent files by path: `path: servers/wiki-search.server.yaml`
- Additional servers can be declared in `servers.yaml` and referenced in agent files by name only
