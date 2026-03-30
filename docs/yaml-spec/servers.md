# servers.yaml

**What it does:** Declares a collection of tool server definitions in a single manifest file. Servers declared here can be referenced in agent files by name only (no path).

**Filename convention:** `servers.yaml` (project root, singular file)

## Annotated Example

```yaml
servers:
  - name: sentiment-scorer       # logical name — used in agent server path references
    description: "Scores text sentiment"
    url: "https://mcp.internal/sentiment"
    auth: "env:SENTIMENT_TOKEN"  # bearer token or env:VAR_NAME; omit if no auth required

  - name: crm
    url: "https://mcp.internal/crm"
    auth: "env:CRM_TOKEN"
```

## Fields

| Field | Type | Required | Description |
|---|---|---|---|
| `servers` | array | yes | List of tool server definitions |
| `servers[].name` | string | yes | Logical name — used in agent `servers[].path` as the name portion |
| `servers[].description` | string | no | Human-readable description |
| `servers[].url` | string | yes | Base URL of the MCP server |
| `servers[].auth` | string | no | Bearer token or `env:VAR_NAME`; omit if no auth required |

## Notes

- Local tool server files (`servers/*.server.yaml`) are not listed here — they are referenced directly by path in agent files.
- Agents reference servers from this manifest by name only (no path).
