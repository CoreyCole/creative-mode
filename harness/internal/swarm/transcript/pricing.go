package transcript

import "strings"

// modelPricing holds per-million-token pricing for a model.
type modelPricing struct {
	Input         float64
	Output        float64
	CacheRead     float64
	CacheCreation float64
}

// Per-million-token pricing constants.
//
//nolint:mnd // pricing values are inherently "magic" numbers
var modelPricingTable = map[string]modelPricing{
	"opus": {
		Input:         15.0,
		Output:        75.0,
		CacheRead:     1.50,
		CacheCreation: 3.75,
	},
	"sonnet": {
		Input:         3.0,
		Output:        15.0,
		CacheRead:     0.30,
		CacheCreation: 0.75,
	},
	"haiku": {
		Input:         0.80,
		Output:        4.0,
		CacheRead:     0.08,
		CacheCreation: 0.20,
	},
}

// EstimateCost calculates the estimated USD cost for a given model and token counts.
func EstimateCost(model string, input, output, cacheRead, cacheCreation int64) float64 {
	pricing := resolvePricing(model)
	perMillion := 1_000_000.0

	return float64(input)*pricing.Input/perMillion +
		float64(output)*pricing.Output/perMillion +
		float64(cacheRead)*pricing.CacheRead/perMillion +
		float64(cacheCreation)*pricing.CacheCreation/perMillion
}

// resolvePricing finds the pricing for a model string using case-insensitive substring matching.
func resolvePricing(model string) modelPricing {
	lower := strings.ToLower(model)
	for key, pricing := range modelPricingTable {
		if strings.Contains(lower, key) {
			return pricing
		}
	}
	// Default to sonnet pricing if model is unknown.
	return modelPricingTable["sonnet"]
}
