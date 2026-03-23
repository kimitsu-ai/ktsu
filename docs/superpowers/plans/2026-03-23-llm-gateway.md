# LLM Gateway Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement the LLM Gateway service — a stateless HTTP proxy that routes agent invocations to upstream LLM providers (OpenAI-compatible and Anthropic), normalizes responses, and calculates cost.

**Architecture:** A `Dispatcher` resolves the model group from config, selects a provider/model via strategy, and delegates to a typed `Provider` implementation. Providers translate to the upstream wire format, call the real API, and return a normalized response. The HTTP server decodes the request, calls the dispatcher, and maps errors to the correct HTTP status codes.

**Tech Stack:** Go stdlib (`net/http`, `encoding/json`), `net/http/httptest` for testing upstream providers. No external mocking libraries.

---

## File Map

| File | Action | Responsibility |
|---|---|---|
| `internal/config/types.go` | Modify | Add `DefaultTemperature`, `Pricing []PricingConfig` to `ModelGroupConfig`; add `PricingConfig` struct |
| `internal/gateway/providers/provider.go` | Modify | Add `Model string`, `Temperature *float64` to `InvokeRequest`; add `GatewayError` type |
| `internal/gateway/pricing/pricing.go` | Create | Pure cost calculation from token counts + pricing config |
| `internal/gateway/pricing/pricing_test.go` | Create | Tests for cost calculation |
| `internal/gateway/dispatcher.go` | Create | Group resolution, strategy, provider dispatch, cost attachment |
| `internal/gateway/dispatcher_test.go` | Create | Tests for dispatcher logic using a fake provider |
| `internal/gateway/providers/openai/provider.go` | Create | OpenAI-compatible HTTP provider |
| `internal/gateway/providers/openai/provider_test.go` | Create | Tests using httptest server |
| `internal/gateway/providers/anthropic/provider.go` | Create | Anthropic native API provider |
| `internal/gateway/providers/anthropic/provider_test.go` | Create | Tests using httptest server |
| `internal/gateway/server.go` | Modify | Implement `POST /invoke` handler |
| `internal/gateway/gateway.go` | Modify | Wire providers and dispatcher at startup |

---

## Task 1: Update config types

**Files:**
- Modify: `internal/config/types.go`

- [ ] **Step 1: Add `DefaultTemperature` and `Pricing` to `ModelGroupConfig`, add `PricingConfig`**

Open `internal/config/types.go`. Replace the existing `ModelGroupConfig` struct and add `PricingConfig` below it:

```go
type ModelGroupConfig struct {
	Name               string          `yaml:"name"`
	Models             []string        `yaml:"models"`
	Strategy           string          `yaml:"strategy"` // round_robin, cost_optimized
	DefaultTemperature float64         `yaml:"default_temperature,omitempty"`
	Pricing            []PricingConfig `yaml:"pricing"`
}

type PricingConfig struct {
	Model            string  `yaml:"model"`
	InputPerMillion  float64 `yaml:"input_per_million"`
	OutputPerMillion float64 `yaml:"output_per_million"`
}
```

- [ ] **Step 2: Verify existing tests still pass**

```bash
cd /home/kyle/repositories/kimitsu-ai/ktsu && go test ./internal/config/...
```
Expected: all pass.

- [ ] **Step 3: Commit**

```bash
git add internal/config/types.go
git commit -m "feat(config): add DefaultTemperature and Pricing to ModelGroupConfig"
```

---

## Task 2: Update provider types and add GatewayError

**Files:**
- Modify: `internal/gateway/providers/provider.go`

- [ ] **Step 1: Write failing test for GatewayError**

Create `internal/gateway/providers/provider_test.go`:

```go
package providers_test

import (
	"errors"
	"testing"

	"github.com/kimitsu-ai/ktsu/internal/gateway/providers"
)

func TestGatewayError_implements_error(t *testing.T) {
	err := &providers.GatewayError{
		Type:      "provider_error",
		Message:   "upstream failed",
		Retryable: true,
	}
	var gwErr *providers.GatewayError
	if !errors.As(err, &gwErr) {
		t.Fatal("expected GatewayError to satisfy errors.As")
	}
	if gwErr.Error() != "upstream failed" {
		t.Fatalf("expected 'upstream failed', got %q", gwErr.Error())
	}
}
```

- [ ] **Step 2: Run to verify it fails**

```bash
cd /home/kyle/repositories/kimitsu-ai/ktsu && go test ./internal/gateway/providers/
```
Expected: compile error — `GatewayError` not defined.

- [ ] **Step 3: Update `provider.go`**

Replace the full content of `internal/gateway/providers/provider.go`:

```go
package providers

import "context"

// Message is a single turn in a conversation.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// InvokeRequest is the normalized request the dispatcher sends to a provider.
// Temperature is a pointer so nil means "use model group default" vs 0.0 explicitly.
type InvokeRequest struct {
	RunID       string    `json:"run_id"`
	StepID      string    `json:"step_id"`
	Group       string    `json:"group"`
	Model       string    `json:"model"` // resolved by dispatcher before calling provider
	Messages    []Message `json:"messages"`
	MaxTokens   int       `json:"max_tokens"`
	Temperature *float64  `json:"temperature,omitempty"` // nil = use group default
}

// InvokeResponse is the normalized response returned to the caller.
type InvokeResponse struct {
	Content       string  `json:"content"`
	ModelResolved string  `json:"model_resolved"`
	TokensIn      int     `json:"tokens_in"`
	TokensOut     int     `json:"tokens_out"`
	CostUSD       float64 `json:"cost_usd"`
}

// GatewayError is a typed error returned by providers and the dispatcher.
// The HTTP handler maps Type to the correct HTTP status code.
type GatewayError struct {
	Type      string // "provider_error", "budget_exceeded", "no_models_available", "unknown_group"
	Message   string
	Retryable bool
}

func (e *GatewayError) Error() string { return e.Message }

// Provider is the interface all LLM provider adapters must implement.
type Provider interface {
	Name() string
	Invoke(ctx context.Context, req InvokeRequest) (InvokeResponse, error)
}
```

- [ ] **Step 4: Run to verify the test passes**

```bash
cd /home/kyle/repositories/kimitsu-ai/ktsu && go test ./internal/gateway/providers/
```
Expected: PASS.

- [ ] **Step 5: Verify nothing else broke**

```bash
cd /home/kyle/repositories/kimitsu-ai/ktsu && go build ./...
```
Expected: builds with no errors.

- [ ] **Step 6: Commit**

```bash
git add internal/gateway/providers/provider.go internal/gateway/providers/provider_test.go
git commit -m "feat(gateway): update InvokeRequest types and add GatewayError"
```

---

## Task 3: Pricing package

**Files:**
- Create: `internal/gateway/pricing/pricing.go`
- Create: `internal/gateway/pricing/pricing_test.go`

- [ ] **Step 1: Write the failing test**

Create `internal/gateway/pricing/pricing_test.go`:

```go
package pricing_test

import (
	"testing"

	"github.com/kimitsu-ai/ktsu/internal/config"
	"github.com/kimitsu-ai/ktsu/internal/gateway/pricing"
)

func TestCalculateCost_basic(t *testing.T) {
	p := config.PricingConfig{
		Model:            "gpt-4o-mini",
		InputPerMillion:  0.15,
		OutputPerMillion: 0.60,
	}
	// 1,000,000 input tokens at $0.15/million = $0.15
	// 1,000,000 output tokens at $0.60/million = $0.60
	got := pricing.CalculateCost(1_000_000, 1_000_000, p)
	want := 0.75
	if got != want {
		t.Fatalf("want %f, got %f", want, got)
	}
}

func TestCalculateCost_partial(t *testing.T) {
	p := config.PricingConfig{
		InputPerMillion:  0.15,
		OutputPerMillion: 0.60,
	}
	// 312 input tokens: 312 * 0.15 / 1_000_000 = 0.0000468
	// 87 output tokens: 87 * 0.60 / 1_000_000  = 0.0000522
	// total: 0.000099
	got := pricing.CalculateCost(312, 87, p)
	want := 312*0.15/1_000_000 + 87*0.60/1_000_000
	if got != want {
		t.Fatalf("want %f, got %f", want, got)
	}
}

func TestCalculateCost_zero(t *testing.T) {
	p := config.PricingConfig{
		InputPerMillion:  0.15,
		OutputPerMillion: 0.60,
	}
	got := pricing.CalculateCost(0, 0, p)
	if got != 0 {
		t.Fatalf("want 0, got %f", got)
	}
}

func TestLookupPricing_found(t *testing.T) {
	configs := []config.PricingConfig{
		{Model: "gpt-4o-mini", InputPerMillion: 0.15, OutputPerMillion: 0.60},
		{Model: "gpt-4o", InputPerMillion: 2.50, OutputPerMillion: 10.00},
	}
	got, ok := pricing.LookupPricing("gpt-4o-mini", configs)
	if !ok {
		t.Fatal("expected to find pricing for gpt-4o-mini")
	}
	if got.InputPerMillion != 0.15 {
		t.Fatalf("wrong pricing returned")
	}
}

func TestLookupPricing_not_found(t *testing.T) {
	configs := []config.PricingConfig{
		{Model: "gpt-4o-mini", InputPerMillion: 0.15, OutputPerMillion: 0.60},
	}
	_, ok := pricing.LookupPricing("unknown-model", configs)
	if ok {
		t.Fatal("expected not found for unknown model")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

```bash
cd /home/kyle/repositories/kimitsu-ai/ktsu && go test ./internal/gateway/pricing/
```
Expected: compile error — package does not exist.

- [ ] **Step 3: Implement pricing**

Create `internal/gateway/pricing/pricing.go`:

```go
package pricing

import "github.com/kimitsu-ai/ktsu/internal/config"

// CalculateCost returns the USD cost for a call given token counts and pricing config.
func CalculateCost(tokensIn, tokensOut int, p config.PricingConfig) float64 {
	return float64(tokensIn)*p.InputPerMillion/1_000_000 +
		float64(tokensOut)*p.OutputPerMillion/1_000_000
}

// LookupPricing finds the pricing config for a model ID (no provider prefix).
func LookupPricing(modelID string, configs []config.PricingConfig) (config.PricingConfig, bool) {
	for _, p := range configs {
		if p.Model == modelID {
			return p, true
		}
	}
	return config.PricingConfig{}, false
}
```

- [ ] **Step 4: Run to verify tests pass**

```bash
cd /home/kyle/repositories/kimitsu-ai/ktsu && go test ./internal/gateway/pricing/
```
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/gateway/pricing/
git commit -m "feat(gateway): add pricing package with cost calculation and lookup"
```

---

## Task 4: Dispatcher

**Files:**
- Create: `internal/gateway/dispatcher.go`
- Create: `internal/gateway/dispatcher_test.go`

- [ ] **Step 1: Write failing tests**

Create `internal/gateway/dispatcher_test.go`:

```go
package gateway_test

import (
	"context"
	"errors"
	"testing"

	"github.com/kimitsu-ai/ktsu/internal/config"
	gw "github.com/kimitsu-ai/ktsu/internal/gateway"
	"github.com/kimitsu-ai/ktsu/internal/gateway/providers"
)

// fakeProvider is a test double that returns a configurable response.
type fakeProvider struct {
	name     string
	response providers.InvokeResponse
	err      error
	lastReq  providers.InvokeRequest
}

func (f *fakeProvider) Name() string { return f.name }
func (f *fakeProvider) Invoke(_ context.Context, req providers.InvokeRequest) (providers.InvokeResponse, error) {
	f.lastReq = req
	return f.response, f.err
}

func gatewayConfig() *config.GatewayConfig {
	return &config.GatewayConfig{
		Providers: []config.ProviderConfig{
			{Name: "openai", Type: "openai"},
		},
		ModelGroups: []config.ModelGroupConfig{
			{
				Name:               "fast",
				Models:             []string{"openai/gpt-4o-mini"},
				Strategy:           "round_robin",
				DefaultTemperature: 0.2,
				Pricing: []config.PricingConfig{
					{Model: "gpt-4o-mini", InputPerMillion: 0.15, OutputPerMillion: 0.60},
				},
			},
			{
				Name:     "empty",
				Models:   []string{},
				Strategy: "round_robin",
			},
		},
	}
}

func TestDispatcher_unknown_group(t *testing.T) {
	fp := &fakeProvider{name: "openai"}
	d := gw.NewDispatcher(gatewayConfig(), map[string]providers.Provider{"openai": fp})
	_, err := d.Dispatch(context.Background(), gw.DispatchRequest{Group: "nonexistent"})
	var gwErr *providers.GatewayError
	if !errors.As(err, &gwErr) {
		t.Fatalf("expected GatewayError, got %T: %v", err, err)
	}
	if gwErr.Type != "unknown_group" {
		t.Fatalf("expected unknown_group, got %q", gwErr.Type)
	}
}

func TestDispatcher_no_models_available(t *testing.T) {
	fp := &fakeProvider{name: "openai"}
	d := gw.NewDispatcher(gatewayConfig(), map[string]providers.Provider{"openai": fp})
	_, err := d.Dispatch(context.Background(), gw.DispatchRequest{Group: "empty"})
	var gwErr *providers.GatewayError
	if !errors.As(err, &gwErr) {
		t.Fatalf("expected GatewayError, got %T", err)
	}
	if gwErr.Type != "no_models_available" {
		t.Fatalf("expected no_models_available, got %q", gwErr.Type)
	}
}

func TestDispatcher_resolves_model_and_provider(t *testing.T) {
	fp := &fakeProvider{
		name:     "openai",
		response: providers.InvokeResponse{Content: "hello", TokensIn: 10, TokensOut: 5},
	}
	d := gw.NewDispatcher(gatewayConfig(), map[string]providers.Provider{"openai": fp})
	resp, err := d.Dispatch(context.Background(), gw.DispatchRequest{
		RunID:     "r1",
		StepID:    "s1",
		Group:     "fast",
		Messages:  []providers.Message{{Role: "user", Content: "hi"}},
		MaxTokens: 100,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Provider receives model ID without prefix
	if fp.lastReq.Model != "gpt-4o-mini" {
		t.Fatalf("expected model gpt-4o-mini, got %q", fp.lastReq.Model)
	}
	// Response has full provider/model string
	if resp.ModelResolved != "openai/gpt-4o-mini" {
		t.Fatalf("expected model_resolved openai/gpt-4o-mini, got %q", resp.ModelResolved)
	}
}

func TestDispatcher_uses_group_default_temperature_when_request_nil(t *testing.T) {
	fp := &fakeProvider{
		name:     "openai",
		response: providers.InvokeResponse{Content: "hi"},
	}
	d := gw.NewDispatcher(gatewayConfig(), map[string]providers.Provider{"openai": fp})
	_, err := d.Dispatch(context.Background(), gw.DispatchRequest{
		Group:       "fast",
		Temperature: nil, // should fall back to group default 0.2
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fp.lastReq.Temperature == nil {
		t.Fatal("expected temperature to be set on provider request")
	}
	if *fp.lastReq.Temperature != 0.2 {
		t.Fatalf("expected 0.2, got %f", *fp.lastReq.Temperature)
	}
}

func TestDispatcher_uses_request_temperature_when_set(t *testing.T) {
	fp := &fakeProvider{
		name:     "openai",
		response: providers.InvokeResponse{Content: "hi"},
	}
	d := gw.NewDispatcher(gatewayConfig(), map[string]providers.Provider{"openai": fp})
	temp := 0.9
	_, err := d.Dispatch(context.Background(), gw.DispatchRequest{
		Group:       "fast",
		Temperature: &temp,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fp.lastReq.Temperature == nil || *fp.lastReq.Temperature != 0.9 {
		t.Fatalf("expected request temperature 0.9 to be forwarded")
	}
}

func TestDispatcher_calculates_cost(t *testing.T) {
	fp := &fakeProvider{
		name:     "openai",
		response: providers.InvokeResponse{Content: "hi", TokensIn: 1_000_000, TokensOut: 1_000_000},
	}
	d := gw.NewDispatcher(gatewayConfig(), map[string]providers.Provider{"openai": fp})
	resp, err := d.Dispatch(context.Background(), gw.DispatchRequest{Group: "fast"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// 1M input @ $0.15 + 1M output @ $0.60 = $0.75
	if resp.CostUSD != 0.75 {
		t.Fatalf("expected cost 0.75, got %f", resp.CostUSD)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

```bash
cd /home/kyle/repositories/kimitsu-ai/ktsu && go test ./internal/gateway/
```
Expected: compile error — `NewDispatcher`, `DispatchRequest` not defined.

- [ ] **Step 3: Implement dispatcher**

Create `internal/gateway/dispatcher.go`:

```go
package gateway

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/kimitsu-ai/ktsu/internal/config"
	"github.com/kimitsu-ai/ktsu/internal/gateway/pricing"
	"github.com/kimitsu-ai/ktsu/internal/gateway/providers"
)

// DispatchRequest is the internal request type used by the dispatcher.
// It is also used directly for JSON decoding of the HTTP /invoke request body.
type DispatchRequest struct {
	RunID       string             `json:"run_id"`
	StepID      string             `json:"step_id"`
	Group       string             `json:"group"`
	Messages    []providers.Message `json:"messages"`
	MaxTokens   int                `json:"max_tokens"`
	Temperature *float64           `json:"temperature,omitempty"`
}

// Dispatcher resolves model groups, selects a provider, and dispatches invocations.
type Dispatcher struct {
	cfg       *config.GatewayConfig
	providers map[string]providers.Provider
	counters  map[string]*atomic.Uint64 // per-group round_robin counters
}

// NewDispatcher creates a Dispatcher with the given config and provider map.
func NewDispatcher(cfg *config.GatewayConfig, provs map[string]providers.Provider) *Dispatcher {
	counters := make(map[string]*atomic.Uint64, len(cfg.ModelGroups))
	for _, g := range cfg.ModelGroups {
		var c atomic.Uint64
		counters[g.Name] = &c
	}
	return &Dispatcher{cfg: cfg, providers: provs, counters: counters}
}

// Dispatch resolves the group, selects a model, calls the provider, and calculates cost.
func (d *Dispatcher) Dispatch(ctx context.Context, req DispatchRequest) (providers.InvokeResponse, error) {
	// 1. Find group
	var group *config.ModelGroupConfig
	for i := range d.cfg.ModelGroups {
		if d.cfg.ModelGroups[i].Name == req.Group {
			group = &d.cfg.ModelGroups[i]
			break
		}
	}
	if group == nil {
		return providers.InvokeResponse{}, &providers.GatewayError{
			Type:      "unknown_group",
			Message:   fmt.Sprintf("model group %q not found", req.Group),
			Retryable: false,
		}
	}

	// 2. Select model via strategy
	if len(group.Models) == 0 {
		return providers.InvokeResponse{}, &providers.GatewayError{
			Type:      "no_models_available",
			Message:   fmt.Sprintf("no models configured in group %q", req.Group),
			Retryable: false,
		}
	}
	model := d.selectModel(group)

	// 3. Split "provider/model"
	providerName, modelID, ok := strings.Cut(model, "/")
	if !ok {
		return providers.InvokeResponse{}, fmt.Errorf("invalid model entry %q: expected provider/model", model)
	}

	// 4. Look up provider
	prov, ok := d.providers[providerName]
	if !ok {
		return providers.InvokeResponse{}, fmt.Errorf("provider %q not registered", providerName)
	}

	// 5. Resolve temperature
	temp := req.Temperature
	if temp == nil {
		t := group.DefaultTemperature
		temp = &t
	}

	// 6. Call provider
	provReq := providers.InvokeRequest{
		RunID:       req.RunID,
		StepID:      req.StepID,
		Group:       req.Group,
		Model:       modelID,
		Messages:    req.Messages,
		MaxTokens:   req.MaxTokens,
		Temperature: temp,
	}
	resp, err := prov.Invoke(ctx, provReq)
	if err != nil {
		return providers.InvokeResponse{}, err
	}

	// 7. Calculate cost
	if pc, found := pricing.LookupPricing(modelID, group.Pricing); found {
		resp.CostUSD = pricing.CalculateCost(resp.TokensIn, resp.TokensOut, pc)
	}

	// 8. Set model_resolved
	resp.ModelResolved = model
	return resp, nil
}

// selectModel picks a model from the group using the configured strategy.
// round_robin and cost_optimized both use round_robin in v1.
func (d *Dispatcher) selectModel(group *config.ModelGroupConfig) string {
	counter := d.counters[group.Name]
	idx := counter.Add(1) - 1
	return group.Models[int(idx)%len(group.Models)]
}
```

- [ ] **Step 4: Run to verify tests pass**

```bash
cd /home/kyle/repositories/kimitsu-ai/ktsu && go test ./internal/gateway/
```
Expected: all dispatcher tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/gateway/dispatcher.go internal/gateway/dispatcher_test.go
git commit -m "feat(gateway): implement dispatcher with group resolution and round_robin strategy"
```

---

## Task 5: OpenAI provider

**Files:**
- Create: `internal/gateway/providers/openai/provider.go`
- Create: `internal/gateway/providers/openai/provider_test.go`

**OpenAI wire format:**

Request to `POST {base_url}/chat/completions`:
```json
{"model":"gpt-4o-mini","messages":[...],"max_tokens":1024,"temperature":0.2}
```

Response:
```json
{"choices":[{"message":{"content":"..."}}],"usage":{"prompt_tokens":312,"completion_tokens":87}}
```

Error response (for budget detection):
```json
{"error":{"code":"billing_hard_limit_reached","message":"..."}}
```

- [ ] **Step 1: Write failing tests**

Create `internal/gateway/providers/openai/provider_test.go`:

```go
package openai_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kimitsu-ai/ktsu/internal/gateway/providers"
	"github.com/kimitsu-ai/ktsu/internal/gateway/providers/openai"
)

func defaultRequest() providers.InvokeRequest {
	temp := 0.2
	return providers.InvokeRequest{
		RunID:  "r1",
		StepID: "s1",
		Model:  "gpt-4o-mini",
		Messages: []providers.Message{
			{Role: "system", Content: "You are helpful."},
			{Role: "user", Content: "Hello"},
		},
		MaxTokens:   100,
		Temperature: &temp,
	}
}

func okResponse() map[string]interface{} {
	return map[string]interface{}{
		"choices": []map[string]interface{}{
			{"message": map[string]string{"content": "Hi there"}},
		},
		"usage": map[string]int{"prompt_tokens": 312, "completion_tokens": 87},
	}
}

func TestOpenAIProvider_success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(okResponse())
	}))
	defer srv.Close()

	resp, err := openai.New(srv.URL, "test-key").Invoke(context.Background(), defaultRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "Hi there" {
		t.Errorf("content: want 'Hi there', got %q", resp.Content)
	}
	if resp.TokensIn != 312 || resp.TokensOut != 87 {
		t.Errorf("tokens: want in=312 out=87, got in=%d out=%d", resp.TokensIn, resp.TokensOut)
	}
}

func TestOpenAIProvider_sends_model_and_params(t *testing.T) {
	var body map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(okResponse())
	}))
	defer srv.Close()

	openai.New(srv.URL, "key").Invoke(context.Background(), defaultRequest())
	if body["model"] != "gpt-4o-mini" {
		t.Errorf("model: want gpt-4o-mini, got %v", body["model"])
	}
	if body["max_tokens"].(float64) != 100 {
		t.Errorf("max_tokens: want 100, got %v", body["max_tokens"])
	}
	if body["temperature"].(float64) != 0.2 {
		t.Errorf("temperature: want 0.2, got %v", body["temperature"])
	}
}

func TestOpenAIProvider_sends_auth_header(t *testing.T) {
	var authHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(okResponse())
	}))
	defer srv.Close()

	openai.New(srv.URL, "sk-test").Invoke(context.Background(), defaultRequest())
	if authHeader != "Bearer sk-test" {
		t.Errorf("auth: want 'Bearer sk-test', got %q", authHeader)
	}
}

func TestOpenAIProvider_rate_limit_retryable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]string{"code": "rate_limit_exceeded", "message": "slow down"},
		})
	}))
	defer srv.Close()

	_, err := openai.New(srv.URL, "key").Invoke(context.Background(), defaultRequest())
	var gwErr *providers.GatewayError
	if !errors.As(err, &gwErr) {
		t.Fatalf("expected GatewayError, got %T: %v", err, err)
	}
	if gwErr.Type != "provider_error" || !gwErr.Retryable {
		t.Errorf("want retryable provider_error, got type=%q retryable=%v", gwErr.Type, gwErr.Retryable)
	}
}

func TestOpenAIProvider_billing_hard_limit_budget_exceeded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]string{"code": "billing_hard_limit_reached", "message": "limit hit"},
		})
	}))
	defer srv.Close()

	_, err := openai.New(srv.URL, "key").Invoke(context.Background(), defaultRequest())
	var gwErr *providers.GatewayError
	if !errors.As(err, &gwErr) {
		t.Fatalf("expected GatewayError")
	}
	if gwErr.Type != "budget_exceeded" || gwErr.Retryable {
		t.Errorf("want non-retryable budget_exceeded, got type=%q retryable=%v", gwErr.Type, gwErr.Retryable)
	}
}

func TestOpenAIProvider_insufficient_quota_budget_exceeded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]string{"code": "insufficient_quota", "message": "quota"},
		})
	}))
	defer srv.Close()

	_, err := openai.New(srv.URL, "key").Invoke(context.Background(), defaultRequest())
	var gwErr *providers.GatewayError
	if !errors.As(err, &gwErr) {
		t.Fatalf("expected GatewayError")
	}
	if gwErr.Type != "budget_exceeded" {
		t.Errorf("want budget_exceeded, got %q", gwErr.Type)
	}
}

func TestOpenAIProvider_5xx_retryable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
	}))
	defer srv.Close()

	_, err := openai.New(srv.URL, "key").Invoke(context.Background(), defaultRequest())
	var gwErr *providers.GatewayError
	if !errors.As(err, &gwErr) {
		t.Fatalf("expected GatewayError")
	}
	if gwErr.Type != "provider_error" || !gwErr.Retryable {
		t.Errorf("want retryable provider_error")
	}
}

func TestOpenAIProvider_4xx_not_retryable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error": map[string]string{"code": "invalid_request", "message": "bad"},
		})
	}))
	defer srv.Close()

	_, err := openai.New(srv.URL, "key").Invoke(context.Background(), defaultRequest())
	var gwErr *providers.GatewayError
	if !errors.As(err, &gwErr) {
		t.Fatalf("expected GatewayError")
	}
	if gwErr.Type != "provider_error" || gwErr.Retryable {
		t.Errorf("want non-retryable provider_error")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

```bash
cd /home/kyle/repositories/kimitsu-ai/ktsu && go test ./internal/gateway/providers/openai/
```
Expected: compile error — package does not exist.

- [ ] **Step 3: Implement OpenAI provider**

Create `internal/gateway/providers/openai/provider.go`:

```go
package openai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/kimitsu-ai/ktsu/internal/gateway/providers"
)

// Provider implements providers.Provider for OpenAI-compatible APIs.
type Provider struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

// New creates an OpenAI-compatible provider.
// baseURL should be the root URL (e.g. "https://api.openai.com/v1").
func New(baseURL, apiKey string) *Provider {
	return &Provider{baseURL: baseURL, apiKey: apiKey, client: &http.Client{}}
}

func (p *Provider) Name() string { return "openai" }

func (p *Provider) Invoke(ctx context.Context, req providers.InvokeRequest) (providers.InvokeResponse, error) {
	body := map[string]interface{}{
		"model":      req.Model,
		"messages":   req.Messages,
		"max_tokens": req.MaxTokens,
	}
	if req.Temperature != nil {
		body["temperature"] = *req.Temperature
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return providers.InvokeResponse{}, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.baseURL+"/chat/completions", bytes.NewReader(bodyBytes))
	if err != nil {
		return providers.InvokeResponse{}, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return providers.InvokeResponse{}, &providers.GatewayError{
			Type:      "provider_error",
			Message:   fmt.Sprintf("http request failed: %v", err),
			Retryable: true,
		}
	}
	defer resp.Body.Close()
	respBytes, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return providers.InvokeResponse{}, p.parseError(resp.StatusCode, respBytes)
	}

	var oaiResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(respBytes, &oaiResp); err != nil {
		return providers.InvokeResponse{}, fmt.Errorf("decode response: %w", err)
	}
	if len(oaiResp.Choices) == 0 {
		return providers.InvokeResponse{}, fmt.Errorf("empty choices in response")
	}

	return providers.InvokeResponse{
		Content:   oaiResp.Choices[0].Message.Content,
		TokensIn:  oaiResp.Usage.PromptTokens,
		TokensOut: oaiResp.Usage.CompletionTokens,
	}, nil
}

// parseError translates an upstream error response into a GatewayError.
func (p *Provider) parseError(statusCode int, body []byte) *providers.GatewayError {
	var errResp struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	json.Unmarshal(body, &errResp)

	code := errResp.Error.Code
	msg := errResp.Error.Message
	if msg == "" {
		msg = fmt.Sprintf("upstream returned status %d", statusCode)
	}

	// Billing hard limits are not retryable
	if code == "billing_hard_limit_reached" || code == "insufficient_quota" {
		return &providers.GatewayError{Type: "budget_exceeded", Message: msg, Retryable: false}
	}

	// 429 rate limit and 5xx are retryable
	retryable := statusCode == 429 || statusCode >= 500
	return &providers.GatewayError{
		Type:      "provider_error",
		Message:   fmt.Sprintf("upstream returned %d: %s", statusCode, msg),
		Retryable: retryable,
	}
}
```

- [ ] **Step 4: Run to verify tests pass**

```bash
cd /home/kyle/repositories/kimitsu-ai/ktsu && go test ./internal/gateway/providers/openai/
```
Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/gateway/providers/openai/
git commit -m "feat(gateway): implement OpenAI-compatible provider"
```

---

## Task 6: Anthropic provider

**Files:**
- Create: `internal/gateway/providers/anthropic/provider.go`
- Create: `internal/gateway/providers/anthropic/provider_test.go`

**Anthropic wire format:**

Request to `POST {base_url}/v1/messages`:
```json
{
  "model": "claude-opus-4-6",
  "max_tokens": 1024,
  "temperature": 0.7,
  "system": "You are helpful.",
  "messages": [{"role":"user","content":"Hello"}]
}
```
Headers: `x-api-key: <key>`, `anthropic-version: 2023-06-01`

Response:
```json
{"content":[{"type":"text","text":"Hi"}],"usage":{"input_tokens":10,"output_tokens":5}}
```

Credit exhaustion: 4xx with `{"type":"error","error":{"type":"authentication_error","message":"credit balance is too low"}}`

- [ ] **Step 1: Write failing tests**

Create `internal/gateway/providers/anthropic/provider_test.go`:

```go
package anthropic_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kimitsu-ai/ktsu/internal/gateway/providers"
	"github.com/kimitsu-ai/ktsu/internal/gateway/providers/anthropic"
)

func defaultRequest() providers.InvokeRequest {
	temp := 0.7
	return providers.InvokeRequest{
		Model: "claude-opus-4-6",
		Messages: []providers.Message{
			{Role: "system", Content: "You are helpful."},
			{Role: "user", Content: "Hello"},
		},
		MaxTokens:   100,
		Temperature: &temp,
	}
}

func TestAnthropicProvider_success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"content": []map[string]string{{"type": "text", "text": "Hi there"}},
			"usage":   map[string]int{"input_tokens": 10, "output_tokens": 5},
		})
	}))
	defer srv.Close()

	resp, err := anthropic.New(srv.URL, "test-key").Invoke(context.Background(), defaultRequest())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "Hi there" {
		t.Errorf("content: want 'Hi there', got %q", resp.Content)
	}
	if resp.TokensIn != 10 || resp.TokensOut != 5 {
		t.Errorf("tokens: want in=10 out=5, got in=%d out=%d", resp.TokensIn, resp.TokensOut)
	}
}

func TestAnthropicProvider_extracts_system_message(t *testing.T) {
	var body map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"content": []map[string]string{{"type": "text", "text": "ok"}},
			"usage":   map[string]int{"input_tokens": 1, "output_tokens": 1},
		})
	}))
	defer srv.Close()

	anthropic.New(srv.URL, "key").Invoke(context.Background(), defaultRequest())

	// system should be a top-level field, not in messages
	if body["system"] != "You are helpful." {
		t.Errorf("system field: want 'You are helpful.', got %v", body["system"])
	}
	msgs := body["messages"].([]interface{})
	for _, m := range msgs {
		msg := m.(map[string]interface{})
		if msg["role"] == "system" {
			t.Error("system message should not appear in messages array")
		}
	}
}

func TestAnthropicProvider_sends_required_headers(t *testing.T) {
	var version, apiKey string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		version = r.Header.Get("anthropic-version")
		apiKey = r.Header.Get("x-api-key")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"content": []map[string]string{{"type": "text", "text": "ok"}},
			"usage":   map[string]int{"input_tokens": 1, "output_tokens": 1},
		})
	}))
	defer srv.Close()

	anthropic.New(srv.URL, "sk-ant-test").Invoke(context.Background(), defaultRequest())
	if version != "2023-06-01" {
		t.Errorf("anthropic-version: want 2023-06-01, got %q", version)
	}
	if apiKey != "sk-ant-test" {
		t.Errorf("x-api-key: want sk-ant-test, got %q", apiKey)
	}
}

func TestAnthropicProvider_credit_exhaustion_is_budget_exceeded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"type": "error",
			"error": map[string]string{
				"type":    "permission_error",
				"message": "credit balance is too low to access the API",
			},
		})
	}))
	defer srv.Close()

	_, err := anthropic.New(srv.URL, "key").Invoke(context.Background(), defaultRequest())
	var gwErr *providers.GatewayError
	if !errors.As(err, &gwErr) {
		t.Fatalf("expected GatewayError, got %T", err)
	}
	if gwErr.Type != "budget_exceeded" || gwErr.Retryable {
		t.Errorf("want non-retryable budget_exceeded, got type=%q retryable=%v", gwErr.Type, gwErr.Retryable)
	}
}

func TestAnthropicProvider_529_overload_retryable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(529)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"type": "error",
			"error": map[string]string{
				"type":    "overloaded_error",
				"message": "overloaded",
			},
		})
	}))
	defer srv.Close()

	_, err := anthropic.New(srv.URL, "key").Invoke(context.Background(), defaultRequest())
	var gwErr *providers.GatewayError
	if !errors.As(err, &gwErr) {
		t.Fatalf("expected GatewayError")
	}
	if gwErr.Type != "provider_error" || !gwErr.Retryable {
		t.Errorf("want retryable provider_error for 529")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

```bash
cd /home/kyle/repositories/kimitsu-ai/ktsu && go test ./internal/gateway/providers/anthropic/
```
Expected: compile error — package does not exist.

- [ ] **Step 3: Implement Anthropic provider**

Create `internal/gateway/providers/anthropic/provider.go`:

```go
package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/kimitsu-ai/ktsu/internal/gateway/providers"
)

// Provider implements providers.Provider for the Anthropic native API.
type Provider struct {
	baseURL string
	apiKey  string
	client  *http.Client
}

// New creates an Anthropic provider. baseURL defaults to "https://api.anthropic.com" if empty.
func New(baseURL, apiKey string) *Provider {
	if baseURL == "" {
		baseURL = "https://api.anthropic.com"
	}
	return &Provider{baseURL: baseURL, apiKey: apiKey, client: &http.Client{}}
}

func (p *Provider) Name() string { return "anthropic" }

func (p *Provider) Invoke(ctx context.Context, req providers.InvokeRequest) (providers.InvokeResponse, error) {
	// Anthropic requires system prompt as a top-level field, not in messages.
	var systemPrompt string
	var msgs []providers.Message
	for _, m := range req.Messages {
		if m.Role == "system" {
			systemPrompt = m.Content
		} else {
			msgs = append(msgs, m)
		}
	}

	body := map[string]interface{}{
		"model":      req.Model,
		"messages":   msgs,
		"max_tokens": req.MaxTokens,
	}
	if systemPrompt != "" {
		body["system"] = systemPrompt
	}
	if req.Temperature != nil {
		body["temperature"] = *req.Temperature
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return providers.InvokeResponse{}, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		p.baseURL+"/v1/messages", bytes.NewReader(bodyBytes))
	if err != nil {
		return providers.InvokeResponse{}, fmt.Errorf("create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return providers.InvokeResponse{}, &providers.GatewayError{
			Type:      "provider_error",
			Message:   fmt.Sprintf("http request failed: %v", err),
			Retryable: true,
		}
	}
	defer resp.Body.Close()
	respBytes, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return providers.InvokeResponse{}, p.parseError(resp.StatusCode, respBytes)
	}

	var antResp struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(respBytes, &antResp); err != nil {
		return providers.InvokeResponse{}, fmt.Errorf("decode response: %w", err)
	}

	var content string
	for _, block := range antResp.Content {
		if block.Type == "text" {
			content = block.Text
			break
		}
	}

	return providers.InvokeResponse{
		Content:   content,
		TokensIn:  antResp.Usage.InputTokens,
		TokensOut: antResp.Usage.OutputTokens,
	}, nil
}

func (p *Provider) parseError(statusCode int, body []byte) *providers.GatewayError {
	var errResp struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	json.Unmarshal(body, &errResp)

	msg := errResp.Error.Message
	if msg == "" {
		msg = fmt.Sprintf("upstream returned status %d", statusCode)
	}

	// Detect credit exhaustion by message content
	if strings.Contains(strings.ToLower(msg), "credit balance") ||
		strings.Contains(strings.ToLower(msg), "credit limit") {
		return &providers.GatewayError{Type: "budget_exceeded", Message: msg, Retryable: false}
	}

	// 429, 529, and 5xx are retryable
	retryable := statusCode == 429 || statusCode == 529 || statusCode >= 500
	return &providers.GatewayError{
		Type:      "provider_error",
		Message:   fmt.Sprintf("upstream returned %d: %s", statusCode, msg),
		Retryable: retryable,
	}
}
```

- [ ] **Step 4: Run to verify tests pass**

```bash
cd /home/kyle/repositories/kimitsu-ai/ktsu && go test ./internal/gateway/providers/anthropic/
```
Expected: all tests PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/gateway/providers/anthropic/
git commit -m "feat(gateway): implement Anthropic provider"
```

---

## Task 7: HTTP server handler

**Files:**
- Modify: `internal/gateway/server.go`

- [ ] **Step 1: Write failing tests**

Create `internal/gateway/server_test.go`:

```go
package gateway_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/kimitsu-ai/ktsu/internal/config"
	gw "github.com/kimitsu-ai/ktsu/internal/gateway"
	"github.com/kimitsu-ai/ktsu/internal/gateway/providers"
)

// fakeDispatcher lets tests control dispatch responses.
type fakeDispatcher struct {
	resp providers.InvokeResponse
	err  error
}

func (f *fakeDispatcher) Dispatch(_ context.Context, _ gw.DispatchRequest) (providers.InvokeResponse, error) {
	return f.resp, f.err
}

func gatewayWithDispatcher(d gw.Dispatchable) *gw.Gateway {
	cfg := gw.Config{
		GatewayConfig: &config.GatewayConfig{},
		Port:          0,
	}
	return gw.NewWithDispatcher(cfg, d)
}

func invokeBody(group string) []byte {
	b, _ := json.Marshal(map[string]interface{}{
		"run_id":     "r1",
		"step_id":    "s1",
		"group":      group,
		"messages":   []map[string]string{{"role": "user", "content": "hi"}},
		"max_tokens": 100,
	})
	return b
}

func TestServer_invoke_success(t *testing.T) {
	d := &fakeDispatcher{resp: providers.InvokeResponse{
		Content:       "hello",
		ModelResolved: "openai/gpt-4o-mini",
		TokensIn:      10,
		TokensOut:     5,
		CostUSD:       0.001,
	}}
	g := gatewayWithDispatcher(d)
	srv := httptest.NewServer(g.Handler())
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/invoke", "application/json", bytes.NewReader(invokeBody("fast")))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	var body providers.InvokeResponse
	json.NewDecoder(resp.Body).Decode(&body)
	if body.Content != "hello" {
		t.Errorf("content: want 'hello', got %q", body.Content)
	}
	if body.CostUSD != 0.001 {
		t.Errorf("cost: want 0.001, got %f", body.CostUSD)
	}
}

func TestServer_invoke_unknown_group_returns_400(t *testing.T) {
	d := &fakeDispatcher{err: &providers.GatewayError{Type: "unknown_group", Message: "not found", Retryable: false}}
	g := gatewayWithDispatcher(d)
	srv := httptest.NewServer(g.Handler())
	defer srv.Close()

	resp, _ := http.Post(srv.URL+"/invoke", "application/json", bytes.NewReader(invokeBody("bad")))
	if resp.StatusCode != 400 {
		t.Errorf("want 400, got %d", resp.StatusCode)
	}
	var body map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&body)
	if body["error"] != "unknown_group" {
		t.Errorf("error field: want unknown_group, got %v", body["error"])
	}
	if body["retryable"] != false {
		t.Errorf("retryable should be false")
	}
}

func TestServer_invoke_budget_exceeded_returns_402(t *testing.T) {
	d := &fakeDispatcher{err: &providers.GatewayError{Type: "budget_exceeded", Message: "limit hit", Retryable: false}}
	g := gatewayWithDispatcher(d)
	srv := httptest.NewServer(g.Handler())
	defer srv.Close()

	resp, _ := http.Post(srv.URL+"/invoke", "application/json", bytes.NewReader(invokeBody("fast")))
	if resp.StatusCode != 402 {
		t.Errorf("want 402, got %d", resp.StatusCode)
	}
}

func TestServer_invoke_no_models_available_returns_503(t *testing.T) {
	d := &fakeDispatcher{err: &providers.GatewayError{Type: "no_models_available", Message: "empty", Retryable: false}}
	g := gatewayWithDispatcher(d)
	srv := httptest.NewServer(g.Handler())
	defer srv.Close()

	resp, _ := http.Post(srv.URL+"/invoke", "application/json", bytes.NewReader(invokeBody("fast")))
	if resp.StatusCode != 503 {
		t.Errorf("want 503, got %d", resp.StatusCode)
	}
}

func TestServer_invoke_provider_error_retryable_returns_502(t *testing.T) {
	d := &fakeDispatcher{err: &providers.GatewayError{Type: "provider_error", Message: "upstream failed", Retryable: true}}
	g := gatewayWithDispatcher(d)
	srv := httptest.NewServer(g.Handler())
	defer srv.Close()

	resp, _ := http.Post(srv.URL+"/invoke", "application/json", bytes.NewReader(invokeBody("fast")))
	if resp.StatusCode != 502 {
		t.Errorf("want 502, got %d", resp.StatusCode)
	}
	var body map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&body)
	if body["retryable"] != true {
		t.Errorf("retryable should be true")
	}
}

func TestServer_health(t *testing.T) {
	g := gatewayWithDispatcher(&fakeDispatcher{})
	srv := httptest.NewServer(g.Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/health")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("want 200, got %d", resp.StatusCode)
	}
}

func TestServer_invoke_bad_json_returns_400(t *testing.T) {
	g := gatewayWithDispatcher(&fakeDispatcher{})
	srv := httptest.NewServer(g.Handler())
	defer srv.Close()

	resp, _ := http.Post(srv.URL+"/invoke", "application/json", bytes.NewReader([]byte("not json")))
	if resp.StatusCode != 400 {
		t.Errorf("want 400, got %d", resp.StatusCode)
	}
}
```

- [ ] **Step 2: Run to verify it fails**

```bash
cd /home/kyle/repositories/kimitsu-ai/ktsu && go test ./internal/gateway/
```
Expected: compile errors — `Dispatchable` interface, `NewWithDispatcher`, `Handler()` not defined.

- [ ] **Step 3: Implement server and update gateway**

First, add a `Dispatchable` interface and `Handler` method. Update `internal/gateway/server.go`:

```go
package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"strconv"

	"github.com/kimitsu-ai/ktsu/internal/gateway/providers"
)

// Dispatchable is the interface the server uses for dispatch — allows test doubles.
type Dispatchable interface {
	Dispatch(ctx context.Context, req DispatchRequest) (providers.InvokeResponse, error)
}

type server struct {
	g          *Gateway
	dispatcher Dispatchable
	mux        *http.ServeMux
}

func newServer(g *Gateway, d Dispatchable) *server {
	s := &server{g: g, dispatcher: d, mux: http.NewServeMux()}
	s.routes()
	return s
}

func (s *server) routes() {
	s.mux.HandleFunc("GET /health", s.handleHealth)
	s.mux.HandleFunc("POST /invoke", s.handleInvoke)
}

func (s *server) Handler() http.Handler { return s.mux }

func (s *server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

func (s *server) handleInvoke(w http.ResponseWriter, r *http.Request) {
	var req DispatchRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "invalid JSON: "+err.Error(), false)
		return
	}

	resp, err := s.dispatcher.Dispatch(r.Context(), req)
	if err != nil {
		var gwErr *providers.GatewayError
		if errors.As(err, &gwErr) {
			status := gatewayErrorStatus(gwErr.Type)
			writeError(w, status, gwErr.Type, gwErr.Message, gwErr.Retryable)
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", err.Error(), false)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(resp)
}

func gatewayErrorStatus(errType string) int {
	switch errType {
	case "unknown_group":
		return http.StatusBadRequest // 400
	case "budget_exceeded":
		return http.StatusPaymentRequired // 402
	case "no_models_available":
		return http.StatusServiceUnavailable // 503
	default:
		return http.StatusBadGateway // 502 for provider_error
	}
}

func writeError(w http.ResponseWriter, status int, errType, message string, retryable bool) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"error":     errType,
		"message":   message,
		"retryable": retryable,
	})
}

func (s *server) serve(ctx context.Context) error {
	port := s.g.cfg.Port
	if port == 0 {
		port = 8081
	}
	addr := net.JoinHostPort(s.g.cfg.Host, strconv.Itoa(port))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	log.Printf("gateway listening on %s", addr)
	srv := &http.Server{Handler: s.mux}
	go func() {
		<-ctx.Done()
		srv.Shutdown(context.Background())
	}()
	return srv.Serve(ln)
}
```

- [ ] **Step 4: Update `gateway.go` to add `NewWithDispatcher` and `Handler`**

Update `internal/gateway/gateway.go`:

```go
package gateway

import (
	"context"
	"fmt"
	"net/http"
	"os"

	"github.com/kimitsu-ai/ktsu/internal/config"
	"github.com/kimitsu-ai/ktsu/internal/gateway/providers"
	"github.com/kimitsu-ai/ktsu/internal/gateway/providers/anthropic"
	"github.com/kimitsu-ai/ktsu/internal/gateway/providers/openai"
)

// Config holds Gateway startup configuration.
type Config struct {
	ConfigPath    string
	GatewayConfig *config.GatewayConfig
	Host          string
	Port          int
}

// Gateway is the LLM proxy service.
type Gateway struct {
	cfg        Config
	dispatcher Dispatchable
}

// New creates a Gateway from config, registering providers from GatewayConfig.
// Returns an error if any required provider cannot be initialized.
func New(cfg Config) (*Gateway, error) {
	provs, err := buildProviders(cfg.GatewayConfig)
	if err != nil {
		return nil, fmt.Errorf("build providers: %w", err)
	}
	d := NewDispatcher(cfg.GatewayConfig, provs)
	return &Gateway{cfg: cfg, dispatcher: d}, nil
}

// NewWithDispatcher creates a Gateway with a custom dispatcher (for testing).
func NewWithDispatcher(cfg Config, d Dispatchable) *Gateway {
	return &Gateway{cfg: cfg, dispatcher: d}
}

// Handler returns the HTTP handler (for testing without starting a listener).
func (g *Gateway) Handler() http.Handler {
	return newServer(g, g.dispatcher).Handler()
}

// Start begins serving requests.
func (g *Gateway) Start(ctx context.Context) error {
	srv := newServer(g, g.dispatcher)
	return srv.serve(ctx)
}

func (g *Gateway) String() string {
	return fmt.Sprintf("Gateway(config=%s)", g.cfg.ConfigPath)
}

// buildProviders instantiates providers from the gateway config.
func buildProviders(cfg *config.GatewayConfig) (map[string]providers.Provider, error) {
	provs := make(map[string]providers.Provider, len(cfg.Providers))
	for _, pc := range cfg.Providers {
		p, err := buildProvider(pc)
		if err != nil {
			return nil, fmt.Errorf("provider %q: %w", pc.Name, err)
		}
		provs[pc.Name] = p
	}
	return provs, nil
}

func buildProvider(pc config.ProviderConfig) (providers.Provider, error) {
	switch pc.Type {
	case "openai":
		baseURL := pc.Config["base_url"]
		apiKey := pc.Config["api_key_env"]
		if apiKey == "" {
			return nil, fmt.Errorf("missing api_key_env")
		}
		key := os.Getenv(apiKey)
		if key == "" {
			return nil, fmt.Errorf("env var %q is not set", apiKey)
		}
		return openai.New(baseURL, key), nil
	case "anthropic":
		apiKeyEnv := pc.Config["api_key_env"]
		if apiKeyEnv == "" {
			return nil, fmt.Errorf("missing api_key_env")
		}
		key := os.Getenv(apiKeyEnv)
		if key == "" {
			return nil, fmt.Errorf("env var %q is not set", apiKeyEnv)
		}
		return anthropic.New("", key), nil
	default:
		return nil, fmt.Errorf("unknown provider type %q", pc.Type)
	}
}
```

- [ ] **Step 5: Run to verify server tests pass**

```bash
cd /home/kyle/repositories/kimitsu-ai/ktsu && go test ./internal/gateway/
```
Expected: all tests PASS (dispatcher tests + server tests).

- [ ] **Step 6: Run all gateway tests**

```bash
cd /home/kyle/repositories/kimitsu-ai/ktsu && go test ./internal/gateway/...
```
Expected: all tests PASS.

- [ ] **Step 7: Build check**

```bash
cd /home/kyle/repositories/kimitsu-ai/ktsu && go build ./...
```
Expected: no errors.

- [ ] **Step 8: Commit**

```bash
git add internal/gateway/server.go internal/gateway/gateway.go internal/gateway/server_test.go
git commit -m "feat(gateway): implement /invoke handler and wire gateway startup"
```

---

## Task 8: Update main.go to use new Gateway constructor

**Files:**
- Modify: `cmd/kimitsu/main.go`

- [ ] **Step 1: Check current main.go usage of Gateway**

```bash
grep -n "gateway" /home/kyle/repositories/kimitsu-ai/ktsu/cmd/kimitsu/main.go
```

- [ ] **Step 2: Update gateway instantiation to use `New` and handle error**

Find the `gateway.New(cfg)` call in `main.go` and replace with the error-returning version:

```go
gw, err := gateway.New(gateway.Config{
    ConfigPath:    gatewayCfgPath,
    GatewayConfig: gatewayCfg,
    Host:          host,
    Port:          gatewayPort,
})
if err != nil {
    log.Fatalf("gateway init: %v", err)
}
```

- [ ] **Step 3: Build check**

```bash
cd /home/kyle/repositories/kimitsu-ai/ktsu && go build ./...
```
Expected: no errors.

- [ ] **Step 4: Run all tests**

```bash
cd /home/kyle/repositories/kimitsu-ai/ktsu && go test ./...
```
Expected: all pass.

- [ ] **Step 5: Commit**

```bash
git add cmd/kimitsu/main.go
git commit -m "feat(gateway): wire gateway startup with fail-fast provider init"
```
