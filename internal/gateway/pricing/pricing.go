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
