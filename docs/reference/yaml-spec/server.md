# server.yaml (tool-server)

**What it does:** Defines a connection to an MCP tool server — declares its URL, authentication, and configurable parameters.

**Filename convention:** `servers/*.server.yaml`

## Annotated Example

```yaml
name: wiki-search                # identity — used in logs and error messages
description: "Internal wiki search" # optional
url: "https://mcp.internal/wiki" # base URL of the MCP server (HTTP/SSE)
auth:                            # optional — outbound authentication config
  # The header name to send in the HTTP request
  header: X-Api-Key              # optional; defaults to "Authorization"
  # The auth scheme: "bearer" (prepends "Bearer ") or "raw" (no prefix)
  scheme: raw                    # optional; defaults to "bearer"
  # The secret value. MUST reference a declared secret parameter using {{ params.NAME }}
  secret: "{{ params.api_key }}" # required if auth is present
params:                          # optional — server configuration parameters
  api_key:
    description: "Cloud API key" # description for human/LLM understanding
    secret: true                 # marks this param as sensitive
  region:
    description: "Cloud region"
    default: "us-east-1"         # default value if not provided by agent
```

## Fields

| Field | Type | Required | Description |
|---|---|---|---|
| `name` | string | yes | Identity used in logs. |
| `url` | string | yes | Base URL of the MCP server (HTTP/SSE transport). |
| `auth.header` | string | no | HTTP header name; defaults to `Authorization`. |
| `auth.scheme` | string | no | `"bearer"` or `"raw"`; defaults to `"bearer"`. |
| `auth.secret` | string | yes (if auth pkg) | Value expression. Must use `{{ params.NAME }}` or backtick literal. **No env: allowed**. |
| `params` | map | no | Declared parameters passed as MCP initialization config. |
| `params.<name>.secret` | boolean | no | If true, Kimitsu ensures this value is never logged or exposed in the envelope. |

## Secret Propagation

To protect credentials, Kimitsu enforces strict "secret propagation". A secret must be explicitly marked at every layer of the configuration chain:

1.  **Workflow**: `env` entry must have `secret: true`.
2.  **Agent**: The parameter receiving the value must have `secret: true` in its schema.
3.  **Server**: The parameter receiving the value from the agent must have `secret: true`.
4.  **Auth**: `auth.secret` must reference that secret parameter.

If any link in this chain misses the `secret: true` flag, the run will fail at boot time to prevent accidental credential leakage.

## Notes

- **HTTP Transport**: Kimitsu only supports the HTTP/SSE transport for MCP servers.
- **Initialization**: Params are sent to the server during the standard MCP `initialize` handshake.
