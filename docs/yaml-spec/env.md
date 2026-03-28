# env.yaml

**What it does:** Declares environment-specific configuration — variables injected at runtime and the state store backend. Selected at startup with `--env environments/dev.env.yaml`.

**Filename convention:** `environments/*.env.yaml`

## Annotated Example

```yaml
name: dev                        # environment name — dev | staging | production | etc.
variables:                       # key-value pairs injected as environment variables
  OUTPUT_WEBHOOK_URL: http://localhost:9999/receive
  SOME_API_KEY: "env:REAL_KEY_FROM_SHELL"  # can reference the shell environment
providers:
  - name: anthropic
    type: anthropic              # anthropic | openai | openai-compat
    config:
      api_key: "env:ANTHROPIC_API_KEY"
state:
  driver: sqlite                 # sqlite | postgres
  dsn: /tmp/myproject/ktsu.db # SQLite: file path | Postgres: connection string or env:VAR
```

## Fields

| Field | Type | Required | Description |
|---|---|---|---|
| `name` | string | yes | Environment name (e.g. `dev`, `staging`, `production`) |
| `variables` | object | no | Key-value pairs injected as environment variables at startup |
| `providers` | array | no | LLM provider credentials for this environment |
| `providers[].name` | string | yes | Logical name — matches provider names in `gateway.yaml` |
| `providers[].type` | string | yes | `anthropic` \| `openai` \| `openai-compat` |
| `providers[].config` | object | yes | Provider-specific config (e.g. `api_key: "env:VAR"`) |
| `state.driver` | string | yes | `sqlite` \| `postgres` |
| `state.dsn` | string | yes | SQLite: file path; Postgres: connection string or `env:VAR` |

## Notes

- `variables` entries are injected before any workflow runs; they are available to `env:VAR_NAME` references in server files, webhook URLs, and gateway config.
- Postgres DSN as env var: `dsn: "env:DATABASE_URL"`
