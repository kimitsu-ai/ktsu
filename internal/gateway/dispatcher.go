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
	RunID       string                   `json:"run_id"`
	StepID      string                   `json:"step_id"`
	Group       string                   `json:"group"`
	Messages    []providers.Message      `json:"messages"`
	MaxTokens   int                      `json:"max_tokens"`
	Temperature *float64                 `json:"temperature,omitempty"`
	Tools       []providers.ToolDefinition `json:"tools,omitempty"`
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
		return providers.InvokeResponse{}, &providers.GatewayError{
			Type:      "invalid_model_config",
			Message:   fmt.Sprintf("invalid model entry %q: expected provider/model", model),
			Retryable: false,
		}
	}

	// 4. Look up provider
	prov, ok := d.providers[providerName]
	if !ok {
		return providers.InvokeResponse{}, &providers.GatewayError{
			Type:      "provider_not_registered",
			Message:   fmt.Sprintf("provider %q not registered", providerName),
			Retryable: false,
		}
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
		Tools:       req.Tools,
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
