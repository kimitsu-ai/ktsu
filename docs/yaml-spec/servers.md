# servers.yaml

**What it does:** Declares all marketplace tool server dependencies for the project — names, sources, and pinned versions.

**Filename convention:** `servers.yaml` (project root, singular file)

## Annotated Example

```yaml
kind: servers

servers:
  - name: sentiment-scorer       # logical name — used in agent server path references
    source: "marketplace/sentiment-scorer"  # marketplace path
    version: "3.0.1"             # pinned version

  - name: crm
    source: "marketplace/acme-crm"
    version: "2.0.0"
```

## Fields

| Field | Type | Required | Description |
|---|---|---|---|
| `kind` | string | yes | Must be `servers` |
| `servers` | array | yes | List of marketplace server dependencies |
| `servers[].name` | string | yes | Logical name — used in agent `servers[].path` as the name portion |
| `servers[].source` | string | yes | Marketplace path |
| `servers[].version` | string | yes | Pinned version |

## Notes

- Local tool server files (`servers/*.server.yaml`) and built-in servers (`ktsu/*`) are never listed here.
- Run `ktsu lock` after changing versions to regenerate `ktsu.lock.yaml`.
- Agents reference marketplace servers in their `servers` block by name only (not path).
