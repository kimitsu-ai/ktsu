# gateway.yaml

**What it does:** Defines all LLM providers and named model groups. The gateway centralizes provider selection, routing strategy, and credentials, allowing agents to reference models by logical group names.

**Filename convention:** `gateway.yaml` (usually in project root)

## Annotated Example

```yaml
providers:
  - name: anthropic              # logical name — used as prefix in model references
    type: anthropic              # anthropic | openai | openai-compat
    config:
      # API keys should be literals or passed via shell environment
      api_key: "YOUR_ANTHROPIC_KEY" # required — provider API key
  
  - name: openai
    type: openai
    config:
      api_key: "YOUR_OPENAI_KEY"

model_groups:
  - name: economy                # group name — referenced in agent and workflow model: fields
    models:
      - anthropic/claude-3-haiku-20240307 # provider-name/model-id
    strategy: round_robin        # optional — round_robin | cost_optimized; default: round_robin
    default_temperature: 0.3     # optional — default temperature for this group
    pricing:                     # optional — used for cost tracking circuit breakers
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

### Providers

| Field | Type | Required | Description |
|---|---|---|---|
| `name` | string | yes | Logical name used as a prefix (e.g., `anthropic/`). |
| `type` | string | yes | `anthropic`, `openai`, or `openai-compat`. |
| `config` | map | yes | Provider-specific config. Most require an `api_key`. |

### Model Groups

| Field | Type | Required | Description |
|---|---|---|---|
| `name` | string | yes | Group identity (e.g., `economy`, `frontier`). |
| `models` | string[] | yes | Array of `provider-name/model-id` strings. |
| `strategy` | string | no | Selection strategy. Defaults to `round_robin`. |
| `default_temperature` | number | no | Default temperature override for the group. |
| `pricing` | array | no | Per-model pricing definitions for cost monitoring. |

## Notes

- **Variable Syntax**: `gateway.yaml` does not support `{{ env.NAME }}` or `env:NAME` syntax. All values must be provided as literals.
- **Provider Normalization**: The Gateway normalizes provider-specific error codes and response formats into a unified Kimitsu contract.
