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
  secret: "{{ params.api_key }}" # required if auth present; must reference a secret param
params:                          # optional — omit if server needs no configuration
  api_key:
    description: "API key for the wiki search service"
    secret: true                 # marks this param as a credential — callers must supply a secret source
  region:
    description: "AWS region to query"
    default: "us-east-1"
```

Minimal bearer token form:

```yaml
name: my-server
url: "https://mcp.internal/my-server"
auth:
  secret: "{{ params.auth_token }}"
params:
  auth_token:
    description: "Bearer token for the server"
    secret: true
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
| `auth.secret` | string | yes (if auth set) | `{{ params.NAME }}` reference to a declared secret param, or a backtick literal. The referenced agent param must also be marked `secret: true`. |
| `params` | map | no | Declared parameters passed as MCP initialization config when the runtime connects. Each entry requires `description`; `default` and `secret` are optional. Params without a default are required. |
| `params.<name>.description` | string | yes | Human-readable explanation |
| `params.<name>.default` | string | no | Default value. Omit to make the param required. |
| `params.<name>.secret` | bool | no | `true` marks this param as a credential. The agent param supplying this value must also be `secret: true`, and the workflow env var or param feeding it must be `secret: true`. Omit or `false` for non-sensitive params. |

`auth` operates at the HTTP transport layer and is separate from `params`, which are sent as MCP initialization config during the `initialize` handshake.

## Secret propagation

Secrets must be marked at every layer of the chain — the runtime enforces this end-to-end:

```
workflow env (secret: true)
  → agent param (secret: true)
    → server param (secret: true)
      → auth.secret: "{{ params.name }}"
```

If any link in the chain is not marked secret the run fails at boot with a clear error. This ensures credentials are masked in logs and the run envelope at every layer.

## Notes

- Local server files are referenced in agent files by path: `path: servers/wiki-search.server.yaml`
- Additional servers can be declared in `servers.yaml` and referenced in agent files by name only

## Shipped Tool Servers

Kimitsu ships tool servers that run as standard MCP servers on default ports. They are started with `ktsu start <name>` and configured with `.server.yaml` files like any other tool server.

| Server | Default Port | Tools | Description |
|---|---|---|---|
| `envelope` | 9104 | `envelope_get`, `envelope_set`, `envelope_append` | Read and write run envelope fields |
