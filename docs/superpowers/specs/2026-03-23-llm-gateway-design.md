# LLM Gateway Design

**Date:** 2026-03-23
**Status:** Approved

---

## Overview

The LLM Gateway is a first-party HTTP service that sits between the Agent Runtime and upstream LLM providers. It normalizes provider-specific wire formats, routes invocations to the correct provider based on model group configuration, calculates cost, and returns a uniform response. It is stateless with respect to runs and budgets — all cost rollup and budget enforcement happens in the orchestrator.

The gateway exposes a single Kimitsu-native HTTP contract. Any conforming proxy can replace it. Litellm and similar proxies work as *provider backends* (configured as `type: openai`) without replacing the gateway contract itself.

---

## HTTP Contract

### `POST /invoke`

**Request:**

```json
{
  "run_id":      "abc123",
  "step_id":     "classify",
  "group":       "fast",
  "messages": [
    {"role": "system",    "content": "You are a classifier."},
    {"role": "user",      "content": "Classify this email: ..."}
  ],
  "max_tokens":  1024,
  "temperature": 0.2
}
```

| Field | Type | Required | Notes |
|---|---|---|---|
| `run_id` | string | yes | Metadata for logging and budget tracking; not forwarded to the LLM |
| `step_id` | string | yes | Metadata for logging; not forwarded to the LLM |
| `group` | string | yes | Model group name from `gateway.yaml`; gateway resolves provider and model |
| `messages` | array | yes | Conversation turns with `role` and `content`; system role embedded as a message |
| `max_tokens` | int | yes | Output (completion) token cap; input tokens are determined by message length |
| `temperature` | float | no | Sampling temperature; falls back to model group `default_temperature` if omitted. Use a JSON null or omit the field entirely to use the default. |

`max_tokens` limits *output* tokens only. Input token count is determined by the content of `messages`. The caller is responsible for staying within the model's context window.

**Response (success — HTTP 200):**

```json
{
  "content":        "This email is a billing inquiry.",
  "model_resolved": "openai/gpt-4o-mini",
  "tokens_in":      312,
  "tokens_out":     87,
  "cost_usd":       0.000052
}
```

**Response (error — HTTP status varies, see Error Taxonomy):**

All error responses have the same shape. `retryable` is always present:

```json
{
  "error":     "provider_error",
  "message":   "upstream returned 429: rate limit exceeded",
  "retryable": true
}
```

```json
{
  "error":     "budget_exceeded",
  "message":   "upstream billing limit reached",
  "retryable": false
}
```

```json
{
  "error":     "no_models_available",
  "message":   "no models configured in group \"fast\"",
  "retryable": false
}
```

### `GET /health`

Returns `{"status": "ok"}`. Used by orchestrator and runtime for liveness checks.

---

## Error Taxonomy

| Error | HTTP Status | Retryable | Meaning |
|---|---|---|---|
| `provider_error` + `retryable: true` | 502 | Yes | Upstream 429 rate limit or 5xx transient error |
| `provider_error` + `retryable: false` | 502 | No | Upstream 4xx bad request or auth failure |
| `budget_exceeded` | 402 | No | Upstream billing hard limit reached (e.g. OpenAI `billing_hard_limit_reached`, Anthropic credit exhaustion) |
| `no_models_available` | 503 | No | The resolved group's model list is empty after config load (v1 does not implement per-model health tracking or circuit breaking) |
| `unknown_group` | 400 | No | Requested group name not found in gateway config |
| `invalid_model_config` | 500 | No | Operator configuration error — e.g. missing required `base_url` for an OpenAI provider |
| `provider_not_registered` | 500 | No | A model group references a provider name that was not registered at startup |

Provider adapters are responsible for translating provider-native error signals into this normalized shape. `budget_exceeded` is distinct from a rate limit — it is a hard stop, not a transient condition, and must not be retried.

---

## Internal Architecture

```
internal/gateway/
  gateway.go           — Gateway struct, startup, provider registration
  server.go            — HTTP server, route registration
  dispatcher.go        — resolves group → provider, applies strategy
  providers/
    provider.go        — Provider interface
    openai/
      provider.go      — OpenAI-compatible (covers litellm, Azure OpenAI, etc.)
    anthropic/
      provider.go      — Anthropic native API
  pricing/
    pricing.go         — cost_usd calculation from token counts + config
```

### Provider Interface

```go
// InvokeRequest is the normalized request the dispatcher sends to a provider.
// Temperature is a pointer so nil means "use model group default" vs 0.0 explicitly.
type InvokeRequest struct {
    RunID       string    `json:"run_id"`
    StepID      string    `json:"step_id"`
    Group       string    `json:"group"`
    Model       string    `json:"model"`        // resolved by dispatcher before calling provider
    Messages    []Message `json:"messages"`
    MaxTokens   int       `json:"max_tokens"`
    Temperature *float64  `json:"temperature,omitempty"`
}

type InvokeResponse struct {
    Content       string  `json:"content"`
    ModelResolved string  `json:"model_resolved"`
    TokensIn      int     `json:"tokens_in"`
    TokensOut     int     `json:"tokens_out"`
    CostUSD       float64 `json:"cost_usd"`
}

type Provider interface {
    Name() string
    Invoke(ctx context.Context, req InvokeRequest) (InvokeResponse, error)
}
```

### Dispatcher

```go
type Dispatcher struct { /* unexported fields */ }

func NewDispatcher(cfg *config.GatewayConfig, providers map[string]providers.Provider) *Dispatcher

// Dispatch resolves the model group, selects a provider/model, calls the provider,
// calculates cost, and returns the normalized response.
func (d *Dispatcher) Dispatch(ctx context.Context, req DispatchRequest) (providers.InvokeResponse, error)

type DispatchRequest struct {
    RunID       string
    StepID      string
    Group       string
    Messages    []providers.Message
    MaxTokens   int
    Temperature *float64  // nil = use group default
}
```

Dispatch steps:

1. Look up group by name in gateway config; return `unknown_group` error if not found
2. Apply selection strategy (`round_robin`, `cost_optimized`) to pick a `provider/model` from the group's model list; return `no_models_available` if list is empty
3. Split `provider/model` string on first `/` to get provider name and model ID
4. Look up the `Provider` implementation by provider name
5. Resolve temperature: use request value if set, else use group `default_temperature`
6. Call `Provider.Invoke` with resolved model ID, messages, max_tokens, temperature
7. Calculate `cost_usd` using the group pricing table (see Cost Calculation)
8. Return `InvokeResponse` with `model_resolved` set to the full `provider/model` string

### Provider Registration

Providers are instantiated from config at startup and registered by name. Adding a new provider type requires implementing the `Provider` interface and registering it — nothing else changes.

```go
providers := map[string]providers.Provider{
    "openai":    openai.New(cfg),
    "anthropic": anthropic.New(cfg),
}
dispatcher := NewDispatcher(gatewayCfg, providers)
```

If a required provider cannot be initialized (missing API key env var, bad config), the gateway refuses to start.

---

## Configuration (`gateway.yaml`)

```yaml
providers:
  - name: openai
    type: openai
    config:
      base_url: https://api.openai.com/v1  # required; gateway refuses to start if missing
      api_key_env: OPENAI_API_KEY          # all config values are strings

  - name: litellm
    type: openai                          # litellm speaks OpenAI-compatible format
    config:
      base_url: http://litellm:4000
      api_key_env: LITELLM_API_KEY

  - name: anthropic
    type: anthropic
    config:
      api_key_env: ANTHROPIC_API_KEY

model_groups:
  - name: fast
    models:
      - openai/gpt-4o-mini
    strategy: round_robin
    default_temperature: 0.2
    pricing:
      - model: gpt-4o-mini               # model ID only, no provider prefix
        input_per_million:  0.15
        output_per_million: 0.60

  - name: powerful
    models:
      - anthropic/claude-opus-4-6
    strategy: round_robin
    default_temperature: 0.7
    pricing:
      - model: claude-opus-4-6           # model ID only, no provider prefix
        input_per_million:  15.00
        output_per_million: 75.00
```

Models in a group are specified as `provider_name/model_id`. The provider name must match a named entry in `providers`. All provider `config` values are strings.

The `pricing` entries use the model ID only (no provider prefix), matching the model ID after stripping the `provider/` prefix from the group's model list entry. The dispatcher strips the provider prefix before doing the pricing lookup.

The Go config structs require two additions to `config/types.go` and two additions to `internal/gateway/providers/provider.go`:

```go
type ModelGroupConfig struct {
    Name               string          `yaml:"name"`
    Models             []string        `yaml:"models"`
    Strategy           string          `yaml:"strategy"`
    DefaultTemperature float64         `yaml:"default_temperature,omitempty"`
    Pricing            []PricingConfig `yaml:"pricing"`
}

type PricingConfig struct {
    Model             string  `yaml:"model"`
    InputPerMillion   float64 `yaml:"input_per_million"`
    OutputPerMillion  float64 `yaml:"output_per_million"`
}
```

`internal/gateway/providers/provider.go` — extend `InvokeRequest` with two new fields:

```go
type InvokeRequest struct {
    RunID       string    `json:"run_id"`
    StepID      string    `json:"step_id"`
    Group       string    `json:"group"`
    Model       string    `json:"model"`             // resolved by dispatcher before calling provider
    Messages    []Message `json:"messages"`
    MaxTokens   int       `json:"max_tokens"`
    Temperature *float64  `json:"temperature,omitempty"` // nil = use group default
}
```

---

## Cost Calculation

The gateway calculates `cost_usd` from:
- `tokens_in` × `input_per_million` / 1,000,000
- `tokens_out` × `output_per_million` / 1,000,000

Pricing is defined per model in the model group config. The gateway does not enforce budgets — it reports cost accurately per call. Budget tracking and enforcement against `ModelPolicy.CostBudgetUSD` is the orchestrator's responsibility, accumulated across all steps in a run.

---

## End-to-End Flow

1. Orchestrator dispatches agent step → Agent Runtime
2. Agent Runtime constructs `messages` from agent system prompt + pipeline step inputs
3. Agent Runtime `POST /invoke` → LLM Gateway with `run_id`, `step_id`, `group`, `messages`, `max_tokens`
4. Dispatcher resolves group → selects model via strategy
5. Provider adapter translates to provider wire format, POSTs to upstream LLM
6. Provider returns content + usage tokens
7. Gateway calculates `cost_usd`, returns `InvokeResponse`
8. Agent Runtime receives content + token/cost metadata, continues agent loop
9. On loop completion, Agent Runtime reports accumulated `tokens_in`, `tokens_out`, `cost_usd` to Orchestrator as part of step result

The gateway is stateless per-run. Each call is independent. Cost accumulation across turns of a multi-turn agent loop happens in the Agent Runtime before reporting to the orchestrator.

---

## Out of Scope (v1)

- Tool/function definitions in the invoke request (Agent Runtime handles MCP tool calls separately)
- Streaming responses
- Per-run budget enforcement at the gateway level
- Request/response logging or audit trail (future concern)
