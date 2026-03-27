# gateway.yaml

**What it does:** Defines all LLM providers and named model groups. Agents declare a group name — the gateway owns provider selection, routing strategy, and credentials.

**Filename convention:** `gateway.yaml` (project root, singular file, no `kind` field)

## Annotated Example

```yaml
providers:
  - name: anthropic              # logical name — used as prefix in model references
    type: anthropic              # anthropic | openai | openai-compat
    config:
      api_key: "env:ANTHROPIC_API_KEY"  # env var holding the API key

  - name: openai
    type: openai
    config:
      api_key: "env:OPENAI_API_KEY"

model_groups:
  - name: economy                # group name — agents declare this in their model: field
    models:
      - anthropic/claude-haiku-4-5-20251001   # "provider-name/model-id"
    strategy: round_robin        # round_robin | cost_optimized; default: round_robin
    default_temperature: 0.3    # optional
    pricing:                     # optional — for cost tracking
      - model: claude-haiku-4-5-20251001
        input_per_million: 0.25
        output_per_million: 1.25

  - name: standard
    models:
      - anthropic/claude-sonnet-4-6
    strategy: round_robin

  - name: frontier
    models:
      - anthropic/claude-opus-4-6
    strategy: round_robin

  - name: vision
    models:
      - openai/gpt-4o
    strategy: round_robin
```

## Provider Fields

| Field | Type | Required | Description |
|---|---|---|---|
| `providers` | array | yes | List of LLM providers |
| `providers[].name` | string | yes | Logical name — used as prefix in model references (`name/model-id`) |
| `providers[].type` | string | yes | `anthropic` \| `openai` \| `openai-compat` |
| `providers[].config.api_key` | string | yes | API key — use `env:VAR_NAME` to read from environment |

## Model Group Fields

| Field | Type | Required | Description |
|---|---|---|---|
| `model_groups` | array | yes | Named groups that agents reference in their `model:` field |
| `model_groups[].name` | string | yes | Group name |
| `model_groups[].models` | string[] | yes | `provider-name/model-id` strings |
| `model_groups[].strategy` | string | no | `round_robin` \| `cost_optimized`; default: `round_robin` |
| `model_groups[].default_temperature` | number | no | Default temperature for all models in this group |
| `model_groups[].pricing` | array | no | Per-model pricing for cost tracking |
| `model_groups[].pricing[].model` | string | yes | Model ID (without provider prefix) |
| `model_groups[].pricing[].input_per_million` | number | yes | Cost per 1M input tokens (USD) |
| `model_groups[].pricing[].output_per_million` | number | yes | Cost per 1M output tokens (USD) |

## Notes

- In an agent file, `model:` is a plain string: `model: standard`
- In a workflow pipeline step, `model:` is an override block: `model: { group: economy, max_tokens: 512 }`
- `model_policy.group_map` and `model_policy.force_group` on a workflow remap groups at runtime without touching agent files
