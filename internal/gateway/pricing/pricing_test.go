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

func TestLookupBuiltin_known_model(t *testing.T) {
	pc, ok := pricing.LookupBuiltin("claude-haiku-4-5-20251001")
	if !ok {
		t.Fatal("expected builtin pricing for claude-haiku-4-5-20251001")
	}
	if pc.InputPerMillion <= 0 || pc.OutputPerMillion <= 0 {
		t.Errorf("expected positive pricing, got in=%f out=%f", pc.InputPerMillion, pc.OutputPerMillion)
	}
}

func TestLookupBuiltin_unknown_model(t *testing.T) {
	_, ok := pricing.LookupBuiltin("unknown-custom-model")
	if ok {
		t.Fatal("expected no builtin pricing for unknown model")
	}
}

func TestLookupBuiltin_user_config_overrides(t *testing.T) {
	// User sets a custom price for a model that also exists in builtins.
	userConfigs := []config.PricingConfig{
		{Model: "gpt-4o", InputPerMillion: 99.00, OutputPerMillion: 99.00},
	}
	got, ok := pricing.LookupPricing("gpt-4o", userConfigs)
	if !ok {
		t.Fatal("expected to find user-configured pricing")
	}
	if got.InputPerMillion != 99.00 {
		t.Errorf("expected user override price 99.00, got %f", got.InputPerMillion)
	}
	// Builtin should still exist but not be used when user config is present.
	builtin, ok := pricing.LookupBuiltin("gpt-4o")
	if !ok {
		t.Fatal("expected builtin to exist for gpt-4o")
	}
	if builtin.InputPerMillion == 99.00 {
		t.Error("builtin should not have been modified by user config")
	}
}
