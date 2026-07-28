package harness

import "strings"

// modelPrice is the USD list price per million tokens. In is uncached input,
// Out is output, CachedIn is cache reads, and CacheWrite is cache creation.
type modelPrice struct {
	In, Out, CachedIn, CacheWrite float64
}

// modelPricing lets callers calculate cost when a backend reports tokens but
// no dollar amount. Rows for backends that report cost directly remain here so
// tests can require every default model to have a known price.
//
// Prices are USD list rates per million tokens as of 2026-07. Update this table
// with each backend's DefaultModels list.
//
//nolint:mnd // pricing is data, and named constants would obscure the table
var modelPricing = map[string]modelPrice{
	// Anthropic
	"claude-opus-4-6":   {In: 5.00, Out: 25.00, CachedIn: 0.50, CacheWrite: 6.25},
	"claude-opus-4-7":   {In: 5.00, Out: 25.00, CachedIn: 0.50, CacheWrite: 6.25},
	"claude-opus-4-8":   {In: 5.00, Out: 25.00, CachedIn: 0.50, CacheWrite: 6.25},
	"claude-opus-5":     {In: 5.00, Out: 25.00, CachedIn: 0.50, CacheWrite: 6.25},
	"claude-sonnet-4-6": {In: 3.00, Out: 15.00, CachedIn: 0.30, CacheWrite: 3.75},
	// Sonnet 5 has an introductory $2/$10 rate through 2026-08-31.
	"claude-sonnet-5":  {In: 3.00, Out: 15.00, CachedIn: 0.30, CacheWrite: 3.75},
	"claude-haiku-4-5": {In: 1.00, Out: 5.00, CachedIn: 0.10, CacheWrite: 1.25},
	"claude-fable-5":   {In: 10.00, Out: 50.00, CachedIn: 1.00, CacheWrite: 12.50},

	// Copilot uses dotted Anthropic version ids.
	"claude-opus-4.6":   {In: 5.00, Out: 25.00, CachedIn: 0.50, CacheWrite: 6.25},
	"claude-sonnet-4.6": {In: 3.00, Out: 15.00, CachedIn: 0.30, CacheWrite: 3.75},
	"claude-haiku-4.5":  {In: 1.00, Out: 5.00, CachedIn: 0.10, CacheWrite: 1.25},

	// OpenAI
	"gpt-5.5":       {In: 5.00, Out: 30.00, CachedIn: 0.50},
	"gpt-5.4":       {In: 2.50, Out: 15.00, CachedIn: 0.25},
	"gpt-5.4-mini":  {In: 0.75, Out: 4.50, CachedIn: 0.075},
	"gpt-5.3-codex": {In: 1.75, Out: 14.00, CachedIn: 0.175},
	"gpt-5.2":       {In: 1.75, Out: 14.00, CachedIn: 0.175},
}

const perMillion = 1e6

// CostFromUsage calculates a result event's list-price cost. It returns zero
// for an unknown model rather than presenting an incorrect estimate.
//
// InputTokens includes all prompt tokens. CacheReadTokens is a discounted
// subset. CacheWriteTokens is separate only for models with a dedicated write
// rate; it remains ordinary input when CacheWrite is zero.
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

// normalizeModelID removes OpenCode's provider prefix and a context-window
// variant suffix so all backends share one base-model pricing key.
func normalizeModelID(id string) string {
	if slash := strings.LastIndexByte(id, '/'); slash >= 0 {
		id = id[slash+1:]
	}
	if bracket := strings.IndexByte(id, '['); bracket > 0 {
		return id[:bracket]
	}
	return id
}
