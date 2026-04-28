---
description: "servers.yaml spec: shared manifest for marketplace/platform MCP servers — name, URL, auth. No {{ env }} support; all values must be literals."
---

# servers.yaml

**What it does:** Declares a collection of shared tool server definitions in a single manifest file. This is typically used for marketplace or internal platform servers.

**Filename convention:** `servers.yaml` (usually in project root)

## Annotated Example

```yaml
servers:
  - name: sentiment-scorer       # logical name — used in agent server path references
    description: "Scores text sentiment"
    url: "https://mcp.internal/sentiment"
    auth:                        # structured auth config
      header: X-Api-Key
      scheme: raw
      secret: "your-token-literal" # Must be a literal in this manifest

  - name: crm
    url: "https://mcp.internal/crm"
    auth:
      secret: "your-crm-token"
```

## Fields

| Field | Type | Required | Description |
|---|---|---|---|
| `servers` | array | yes | List of shared tool server definitions. |
| `servers[].name` | string | yes | Identity used in agent `servers[].path`. |
| `servers[].url` | string | yes | Base URL of the MCP server. |
| `servers[].auth` | object | no | Optional authentication configuration. |

## Notes

- **Variable Syntax**: `servers.yaml` does not support `{{ env.NAME }}` or `env:NAME` syntax. All values must be provided as literals.
- **Local Files**: Servers defined in `servers/*.server.yaml` are not listed here. They are used for project-specific tools.
