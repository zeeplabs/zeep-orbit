package catalog

import "time"

// How to update this file (RELEASE.md, "Model catalog"):
//   - Check developers.openai.com/api/docs/pricing (Standard tier, short
//     context) and platform.claude.com/docs/en/about-claude/pricing.
//   - A price change is a new PriceVersion with its EffectiveFrom; never edit
//     a version that is already in a release (history was priced with it).
//   - A model the provider still serves is marked Deprecated, never removed.
//   - Set ReviewedAt to the day of the check and add a CHANGELOG line.

// ReviewedAt is the day the models and prices below were checked against the
// providers' pricing pages.
var ReviewedAt = day(2026, 11, 13)

// launch is the EffectiveFrom of the first data set.
var launch = day(2026, 11, 13)

func day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

// openai is a model with one flat price. OpenAI bills no cache write. The
// prices are the short-context ones: gpt-5.5 and gpt-5.4 cost more above 272K
// input tokens, which the builder does not reach (compaction runs well below).
func openai(id, input, cachedInput, output string) Model {
	return Model{Provider: ProviderOpenAI, ID: id, Prices: []PriceVersion{{
		EffectiveFrom: launch,
		Tiers:         []Tier{{Input: input, CachedInput: cachedInput, Output: output}},
	}}}
}

// anthropic is a model with one flat price. CacheWrite is the 5-minute write
// (the adapter requests no 1-hour cache).
func anthropic(id, input, cacheWrite, cachedInput, output string) Model {
	return Model{Provider: ProviderAnthropic, ID: id, Prices: []PriceVersion{{
		EffectiveFrom: launch,
		Tiers:         []Tier{{Input: input, CacheWrite: cacheWrite, CachedInput: cachedInput, Output: output}},
	}}}
}

var models = []Model{
	openai("gpt-6-astra", "10", "1", "50"),
	openai("gpt-6.1-sol", "2", "0.1", "10"),
	openai("gpt-6.1-terra", "1", "0.05", "5"),
	openai("gpt-6-luna", "0.1", "0.01", "0.5"),
	openai("gpt-5.5", "5", "0.5", "30"),
	openai("gpt-5.4", "2.5", "0.25", "15"),
	openai("gpt-5.4-mini", "0.75", "0.075", "4.5"),
	openai("gpt-5.4-nano", "0.2", "0.02", "1.25"),
	openai("gpt-5-mini", "0.25", "0.025", "2"),
	openai("gpt-4.1", "2", "0.5", "8"),
	openai("gpt-4.1-mini", "0.4", "0.1", "1.6"),
	openai("gpt-4o", "2.5", "1.25", "10"),
	openai("gpt-4o-mini", "0.15", "0.075", "0.6"),

	anthropic("claude-fable-5-2", "8", "10", "0.2", "40"),
	anthropic("claude-opus-5-5", "4", "5", "0.2", "20"),
	anthropic("claude-sonnet-5-5", "2", "2.5", "0.1", "10"),
	// Haiku 5.5 is priced by prompt size: above 100,000 tokens every class
	// costs five times more.
	{Provider: ProviderAnthropic, ID: "claude-haiku-5-5", Prices: []PriceVersion{{
		EffectiveFrom: launch,
		Tiers: []Tier{
			{MaxPromptTokens: 100_000, Input: "0.1", CacheWrite: "0.125", CachedInput: "0.01", Output: "0.5"},
			{Input: "0.5", CacheWrite: "0.625", CachedInput: "0.05", Output: "2.5"},
		},
	}}},
	anthropic("claude-fable-5-1", "10", "12.5", "0.25", "50"),
	anthropic("claude-opus-5", "5", "6.25", "0.5", "25"),
	anthropic("claude-sonnet-5", "2", "2.5", "0.2", "10"),
	anthropic("claude-sonnet-4-6", "3", "3.75", "0.3", "15"),
	anthropic("claude-haiku-4-5", "1", "1.25", "0.1", "5"),
}
