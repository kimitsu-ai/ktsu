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
