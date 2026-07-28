package harness

import "strings"

type modelPrice struct {
	In, Out, CachedIn, CacheWrite float64
}

// Prices are USD per one million tokens. Update this table when a backend's
// default model list changes.
//
//nolint:mnd // pricing is data, and named constants would obscure the table
var modelPricing = map[string]modelPrice{
	"claude-opus-4-6":   {In: 5.00, Out: 25.00, CachedIn: 0.50, CacheWrite: 6.25},
	"claude-opus-4-7":   {In: 5.00, Out: 25.00, CachedIn: 0.50, CacheWrite: 6.25},
	"claude-opus-4-8":   {In: 5.00, Out: 25.00, CachedIn: 0.50, CacheWrite: 6.25},
	"claude-opus-5":     {In: 5.00, Out: 25.00, CachedIn: 0.50, CacheWrite: 6.25},
	"claude-sonnet-4-6": {In: 3.00, Out: 15.00, CachedIn: 0.30, CacheWrite: 3.75},
	"claude-sonnet-5":   {In: 3.00, Out: 15.00, CachedIn: 0.30, CacheWrite: 3.75},
	"claude-haiku-4-5":  {In: 1.00, Out: 5.00, CachedIn: 0.10, CacheWrite: 1.25},
	"claude-fable-5":    {In: 10.00, Out: 50.00, CachedIn: 1.00, CacheWrite: 12.50},

	// Copilot uses dotted Anthropic version ids.
	"claude-opus-4.6":   {In: 5.00, Out: 25.00, CachedIn: 0.50, CacheWrite: 6.25},
	"claude-sonnet-4.6": {In: 3.00, Out: 15.00, CachedIn: 0.30, CacheWrite: 3.75},
	"claude-haiku-4.5":  {In: 1.00, Out: 5.00, CachedIn: 0.10, CacheWrite: 1.25},

	"gpt-5.5":       {In: 5.00, Out: 30.00, CachedIn: 0.50},
	"gpt-5.4":       {In: 2.50, Out: 15.00, CachedIn: 0.25},
	"gpt-5.4-mini":  {In: 0.75, Out: 4.50, CachedIn: 0.075},
	"gpt-5.3-codex": {In: 1.75, Out: 14.00, CachedIn: 0.175},
	"gpt-5.2":       {In: 1.75, Out: 14.00, CachedIn: 0.175},
}

const perMillion = 1e6

// CostFromUsage calculates a result event's list-price cost. It returns zero
// when model is not present in the pricing table.
func CostFromUsage(model string, usage Usage) float64 {
	price, ok := modelPricing[normalizeModelID(model)]
	if !ok {
		return 0
	}
	uncached := usage.InputTokens - usage.CacheReadTokens
	if price.CacheWrite > 0 {
		uncached -= usage.CacheWriteTokens
	}
	if uncached < 0 {
		uncached = 0
	}
	return (float64(uncached)*price.In +
		float64(usage.CacheReadTokens)*price.CachedIn +
		float64(usage.CacheWriteTokens)*price.CacheWrite +
		float64(usage.OutputTokens)*price.Out) / perMillion
}

func normalizeModelID(id string) string {
	if slash := strings.LastIndexByte(id, '/'); slash >= 0 {
		id = id[slash+1:]
	}
	if bracket := strings.IndexByte(id, '['); bracket > 0 {
		return id[:bracket]
	}
	return id
}
