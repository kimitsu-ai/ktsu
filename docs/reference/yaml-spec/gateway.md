# gateway.yaml

**What it does:** Defines all LLM providers and named model groups. The gateway centralizes provider selection, routing strategy, and credentials, allowing agents to reference models by logical group names.

**Filename convention:** `gateway.yaml` (usually in project root)

## Annotated Example

```yaml
env:
  - name: ANTHROPIC_API_KEY   # env var name to read from process environment
    secret: true              # true = masked in logs
    description: "Anthropic API key"
  - name: OPENAI_API_KEY
    secret: true
    default: "sk-unused"      # optional — used when env var is not set

providers:
  - name: anthropic            # logical name — used as prefix in model references
    type: anthropic            # anthropic | openai
    config:
      api_key: "{{ env.ANTHROPIC_API_KEY }}"  # required — resolved from env: section

  - name: openai
    type: openai
    config:
      base_url: "https://api.openai.com/v1"
      api_key: "{{ env.OPENAI_API_KEY }}"

model_groups:
  - name: economy              # group name — referenced in agent and workflow model: fields
    models:
      - anthropic/claude-3-haiku-20240307  # provider-name/model-id
    strategy: round_robin      # optional — round_robin | cost_optimized; default: round_robin
    default_temperature: 0.3   # optional — default temperature for this group
    pricing:                   # optional — used for cost tracking circuit breakers
      - model: claude-3-haiku-20240307
        input_per_million: 0.25
        output_per_million: 1.25

  - name: standard
    models:
      - anthropic/claude-3-5-sonnet-20240620
    strategy: round_robin

  - name: frontier
    models:
      - anthropic/claude-3-opus-20240229
    strategy: round_robin
```

## Fields

### Env

Declares environment variables available for substitution in provider config values. All `{{ env.VAR }}` references in the file must be declared here. The gateway fails fast at startup if a required var is not set.

| Field | Type | Required | Description |
|---|---|---|---|
| `name` | string | yes | Environment variable name (read from process environment). |
| `secret` | bool | no | If `true`, the value is masked in logs. |
| `default` | string | no | Fallback value used when the env var is not set. |
| `description` | string | no | Human-readable description. |

### Providers

| Field | Type | Required | Description |
|---|---|---|---|
| `name` | string | yes | Logical name used as a prefix (e.g., `anthropic/`). |
| `type` | string | yes | `anthropic` or `openai`. |
| `config` | map | yes | Provider-specific config. Supports `{{ env.VAR }}` substitution. |

### Model Groups

| Field | Type | Required | Description |
|---|---|---|---|
| `name` | string | yes | Group identity (e.g., `economy`, `frontier`). |
| `models` | string[] | yes | Array of `provider-name/model-id` strings. |
| `strategy` | string | no | Selection strategy. Defaults to `round_robin`. |
| `default_temperature` | number | no | Default temperature override for the group. |
| `pricing` | array | no | Per-model pricing definitions for cost monitoring. |

## Variable Substitution

Provider config values support `{{ env.VAR_NAME }}` substitution. All referenced variables must be declared in the root `env:` section.

```yaml
env:
  - name: MY_API_KEY
    secret: true

providers:
  - name: my-provider
    type: openai
    config:
      api_key: "{{ env.MY_API_KEY }}"
      base_url: "https://api.example.com/v1"
```

The gateway resolves all env vars at startup:
1. Reads the value from the process environment (`os.Getenv`).
2. Falls back to `default` if the env var is not set.
3. Fails with a clear error if neither is available.

## Notes

- **Provider Normalization**: The Gateway normalizes provider-specific error codes and response formats into a unified Kimitsu contract.
