# server.yaml (tool-server)

**What it does:** Points to a local MCP tool server — declares its URL, auth, and trust flags. The server itself is an independent MCP process reachable over HTTP/SSE; Kimitsu does not start or manage it. Kiimitsu does not support the `stdio` transport.

**Filename convention:** `servers/*.server.yaml`

## Annotated Example

```yaml
name: wiki-search                # identity — used in logs and error messages
description: "..."               # optional
url: "https://mcp.internal/wiki" # base URL of the MCP server (HTTP/SSE)
auth: "env:WIKI_TOKEN"           # bearer token or env:VAR_NAME; omit if no auth required
params:                          # optional — omit if server needs no configuration
  region:
    description: "AWS region to query"
    default: "us-east-1"
```

## Fields

| Field | Type | Required | Description |
|---|---|---|---|
| `name` | string | yes | Identity — used in logs and error messages |
| `description` | string | no | Human-readable |
| `url` | string | yes | Base URL of the MCP server (HTTP/SSE) |
| `auth` | string | no | Bearer token or `env:VAR_NAME`; omit if no auth required |
| `params` | map | no | Declared parameters passed as MCP initialization config when the runtime connects. Each entry requires `description`; `default` is optional. Params without a default are required. Supports `env:VAR_NAME` in defaults. |
| `params.<name>.description` | string | yes | Human-readable explanation |
| `params.<name>.default` | string | no | Default value. Omit to make the param required. |

`auth` operates at the HTTP transport layer (Authorization header) and is separate from `params`, which are sent as MCP initialization config sent during the `initialize` handshake.

## Shipped Tool Servers

Kimitsu ships tool servers that run as standard MCP servers on default ports. They are started with `ktsu start <name>` and configured with `.server.yaml` files like any other tool server.

| Server | Default Port | Tools | Description |
|---|---|---|---|
| `envelope` | 9104 | `envelope_get`, `envelope_set`, `envelope_append` | Read and write run envelope fields |

## Notes

- Local server files are referenced in agent files by path: `path: servers/wiki-search.server.yaml`
- Additional servers can be declared in `servers.yaml` and referenced in agent files by name only
