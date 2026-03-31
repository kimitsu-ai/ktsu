package pricing

import "github.com/kimitsu-ai/ktsu/internal/config"

// builtinPricing holds known model prices (USD per million tokens) as a fallback
// when no pricing is configured in gateway.yaml. Keyed by bare model ID (no provider prefix).
var builtinPricing = map[string]config.PricingConfig{
	// Anthropic
	"claude-opus-4-6":           {Model: "claude-opus-4-6", InputPerMillion: 15.00, OutputPerMillion: 75.00},
	"claude-sonnet-4-6":         {Model: "claude-sonnet-4-6", InputPerMillion: 3.00, OutputPerMillion: 15.00},
	"claude-haiku-4-5-20251001": {Model: "claude-haiku-4-5-20251001", InputPerMillion: 0.80, OutputPerMillion: 4.00},
	"claude-3-5-sonnet-20241022": {Model: "claude-3-5-sonnet-20241022", InputPerMillion: 3.00, OutputPerMillion: 15.00},
	"claude-3-5-haiku-20241022":  {Model: "claude-3-5-haiku-20241022", InputPerMillion: 0.80, OutputPerMillion: 4.00},
	"claude-3-opus-20240229":     {Model: "claude-3-opus-20240229", InputPerMillion: 15.00, OutputPerMillion: 75.00},
	"claude-3-haiku-20240307":    {Model: "claude-3-haiku-20240307", InputPerMillion: 0.25, OutputPerMillion: 1.25},
	// OpenAI
	"gpt-4o":       {Model: "gpt-4o", InputPerMillion: 2.50, OutputPerMillion: 10.00},
	"gpt-4o-mini":  {Model: "gpt-4o-mini", InputPerMillion: 0.15, OutputPerMillion: 0.60},
	"gpt-4-turbo":  {Model: "gpt-4-turbo", InputPerMillion: 10.00, OutputPerMillion: 30.00},
	"gpt-3.5-turbo": {Model: "gpt-3.5-turbo", InputPerMillion: 0.50, OutputPerMillion: 1.50},
}

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

// LookupBuiltin returns the built-in pricing for a model ID, if known.
func LookupBuiltin(modelID string) (config.PricingConfig, bool) {
	pc, ok := builtinPricing[modelID]
	return pc, ok
}
