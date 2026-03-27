# server.yaml (tool-server)

**What it does:** Points to a local MCP tool server — declares its URL, auth, and trust flags. The server itself is an independent MCP process; Kimitsu does not start or manage it.

**Filename convention:** `servers/*.server.yaml`

## Annotated Example

```yaml
kind: tool-server
name: wiki-search                # identity — used in logs and error messages
description: "..."               # optional
url: "https://mcp.internal/wiki" # base URL of the MCP server
auth: "env:WIKI_TOKEN"           # bearer token or env:VAR_NAME; omit if no auth required
stateful: false                  # true if server causes external side effects (write, mutate, send)
egress: false                    # true if server makes outbound calls to external services
```

## Fields

| Field | Type | Required | Description |
|---|---|---|---|
| `kind` | string | yes | Must be `tool-server` |
| `name` | string | yes | Identity — used in logs and error messages |
| `description` | string | no | Human-readable |
| `url` | string | yes | Base URL of the MCP server |
| `auth` | string | no | Bearer token or `env:VAR_NAME`; omit if no auth required |
| `stateful` | boolean | no | `true` if the server causes external side effects; default: `false` |
| `egress` | boolean | no | `true` if the server makes outbound calls to external services; default: `false` |

## Built-in Tool Servers

Declared in agent files as `ktsu/<name>@<version>` — no `.server.yaml` file required.

### Unrestricted (available to all agents including sub-agents)

| Server | Tools | Description |
|---|---|---|
| `ktsu/format@1.0.0` | `format_json`, `format_yaml`, `format_template` | Format data |
| `ktsu/validate@1.0.0` | `validate_schema`, `validate_json` | Validate against JSON Schema |
| `ktsu/transform@1.0.0` | `transform_jmespath`, `transform_map`, `transform_filter` | JMESPath operations |
| `ktsu/cli@1.0.0` | `jq`, `grep`, `sed`, `awk`, `date`, `wc`, `diff`, `sort`, `uniq`, `cut`, `base64` | Unix CLI tools as typed MCP tools |

### Restricted (pipeline agents only — not available to sub-agents)

| Server | Tools | Description |
|---|---|---|
| `ktsu/kv@1.0.0` | `kv_get`, `kv_set`, `kv_delete` | Key-value storage scoped to agent namespace |
| `ktsu/blob@1.0.0` | `blob_get`, `blob_put`, `blob_delete`, `blob_list` | Binary/file storage |
| `ktsu/log@1.0.0` | `log_write`, `log_read`, `log_tail` | Structured run log |
| `ktsu/memory@1.0.0` | `memory_store`, `memory_retrieve`, `memory_search`, `memory_forget` | Semantic vector memory |
| `ktsu/envelope@1.0.0` | `envelope_get`, `envelope_set`, `envelope_append` | Read and write run envelope fields |

## Notes

- Local server files are referenced in agent files by path: `path: servers/wiki-search.server.yaml`
- Marketplace servers are declared in `servers.yaml` and referenced in agent files by name only
- `stateful` and `egress` are trust signals for operators and marketplace review — not enforced at the network layer
