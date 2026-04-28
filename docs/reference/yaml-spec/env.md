---
description: "env.yaml spec: environment-specific variables, optional provider overrides, and state store backend — used to keep dev/prod differences out of workflow definitions."
---

# env.yaml

**What it does:** Declares environment-specific configuration, such as global environment variables and the state store backend. Selected at startup using the `--env` flag (e.g., `ktsu start --env environments/dev.env.yaml`).

**Filename convention:** `environments/*.env.yaml`

## Annotated Example

```yaml
name: dev                        # logical environment name (dev, prod, etc.)
variables:                       # global key-value pairs available as environment variables
  API_BASE_URL: "https://api.dev.local"
  LOG_LEVEL: "debug"
providers:                       # optional — environment-specific provider overrides
  - name: anthropic
    type: anthropic
    config:
      api_key: "YOUR_DEV_KEY"    # credentials for this environment
state:                           # configuration for the run state store
  driver: sqlite                 # sqlite | postgres
  dsn: "ktsu_dev.db"             # SQLite file path or Postgres connection string
```

## Fields

| Field | Type | Required | Description |
|---|---|---|---|
| `name` | string | yes | Identity for the environment. |
| `variables` | object | no | Key-value mapping for global environment variables. |
| `providers` | array | no | List of LLM providers specific to this environment. |
| `state.driver` | string | yes | Persistence backend (`sqlite` or `postgres`). |
| `state.dsn` | string | yes | Connection string or file path for the state driver. |

## Notes

- **Initial Context**: Variables defined here are injected before any workflows are loaded or executed.
- **Variable Syntax**: `env.yaml` does not support `{{ env.NAME }}` or `env:NAME` syntax. All values must be provided as literals.
- **Portability**: Different `.env.yaml` files are used to toggle between local development and production infrastructure without changing workflow definitions.
