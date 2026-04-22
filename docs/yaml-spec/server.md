# server.yaml (tool-server)

**What it does:** Points to a local MCP tool server — declares its URL, auth, and trust flags. The server itself is an independent MCP process reachable over HTTP/SSE; Kimitsu does not start or manage it. Kimitsu does not support the `stdio` transport.

**Filename convention:** `servers/*.server.yaml`

## Annotated Example

```yaml
name: wiki-search                # identity — used in logs and error messages
description: "..."               # optional
url: "https://mcp.internal/wiki" # base URL of the MCP server (HTTP/SSE)
auth:                            # optional — omit if no auth required
  header: X-Api-Key              # optional; defaults to "Authorization"
  scheme: raw                    # optional; "raw" (value as-is) or "bearer" (prepend "Bearer "); defaults to "bearer"
  secret: "param:api_key"   # required if auth present; value expression
params:                          # optional — omit if server needs no configuration
  region:
    description: "AWS region to query"
    default: "us-east-1"
```

Minimal bearer token form:

```yaml
name: my-server
url: "https://mcp.internal/my-server"
auth:
  secret: "param:auth_token"
```

## Fields

| Field | Type | Required | Description |
|---|---|---|---|
| `name` | string | yes | Identity — used in logs and error messages |
| `description` | string | no | Human-readable |
| `url` | string | yes | Base URL of the MCP server (HTTP/SSE) |
| `auth` | object | no | Outbound auth config. Omit entirely for unauthenticated servers. |
| `auth.header` | string | no | HTTP header name to set. Defaults to `Authorization`. |
| `auth.scheme` | string | no | `"bearer"` prepends `Bearer ` before the resolved secret; `"raw"` sends the value as-is. Defaults to `"bearer"`. |
| `auth.secret` | string | yes (if auth set) | Value expression for the credential. Supports `param:NAME` and backtick literals. Resolved at run time from the agent's resolved params. |
| `params` | map | no | Declared parameters passed as MCP initialization config when the runtime connects. Each entry requires `description`; `default` is optional. Params without a default are required. |
| `params.<name>.description` | string | yes | Human-readable explanation |
| `params.<name>.default` | string | no | Default value. Omit to make the param required. |

`auth` operates at the HTTP transport layer and is separate from `params`, which are sent as MCP initialization config during the `initialize` handshake.

Server files may not use `env:VAR_NAME` references directly. Use `param:NAME` references, resolved from the agent's resolved params at invocation time. The preferred pattern is to declare a param (e.g. `api_key`) in the server file, pass its value from the workflow step's `params.<name>` block, which reads it from the parent workflow's `env` block (root workflow) or `params` block (sub-workflow).

## Shipped Tool Servers

Kimitsu ships tool servers that run as standard MCP servers on default ports. They are started with `ktsu start <name>` and configured with `.server.yaml` files like any other tool server.

| Server | Default Port | Tools | Description |
|---|---|---|---|
| `envelope` | 9104 | `envelope_get`, `envelope_set`, `envelope_append` | Read and write run envelope fields |

## Notes

- Local server files are referenced in agent files by path: `path: servers/wiki-search.server.yaml`
- Additional servers can be declared in `servers.yaml` and referenced in agent files by name only
