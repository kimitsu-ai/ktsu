# env.yaml

**What it does:** Declares environment-specific configuration — variables injected at runtime and the state store backend. Selected at startup with `--env environments/dev.env.yaml`.

**Filename convention:** `environments/*.env.yaml`

## Annotated Example

```yaml
kind: env
name: dev                        # environment name — dev | staging | production | etc.
variables:                       # key-value pairs injected as environment variables
  OUTPUT_WEBHOOK_URL: http://localhost:9999/receive
  SOME_API_KEY: "env:REAL_KEY_FROM_SHELL"  # can reference the shell environment
state:
  driver: sqlite                 # sqlite | postgres
  dsn: /tmp/myproject/kimitsu.db # SQLite: file path | Postgres: connection string or env:VAR
```

## Fields

| Field | Type | Required | Description |
|---|---|---|---|
| `kind` | string | yes | Must be `env` |
| `name` | string | yes | Environment name (e.g. `dev`, `staging`, `production`) |
| `variables` | object | no | Key-value pairs injected as environment variables at startup |
| `state.driver` | string | yes | `sqlite` \| `postgres` |
| `state.dsn` | string | yes | SQLite: file path; Postgres: connection string or `env:VAR` |

## Notes

- `variables` entries are injected before any workflow runs; they are available to `env:VAR_NAME` references in server files, webhook URLs, and gateway config.
- Postgres DSN as env var: `dsn: "env:DATABASE_URL"`
