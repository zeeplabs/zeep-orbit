// Package catalog is the list of models Orbit offers for the providers with a
// public price list (openai, anthropic) and their list prices in USD
// (ai-model-catalog). It is part of the release: the models and prices change
// only with a new Orbit version, never at runtime and never from the network.
//
// Prices are decimal strings per million tokens, as the providers publish
// them; the usage ledger parses them with store.ParsePrice. A model can carry
// several price versions (EffectiveFrom) so a release can ship a known future
// change, and a version can have tiers by prompt size.
//
// It imports nothing from Orbit, so the store and the dashboard can both use it.
package catalog

import (
	"slices"
	"time"
)

// Currency is the currency of every price in the catalog, and of the
// builder's costs and money caps.
const Currency = "USD"

// Provider kinds covered by the catalog. They are the provider names the
// usage ledger records (provider.Provider.Name()).
const (
	ProviderOpenAI    = "openai"
	ProviderAnthropic = "anthropic"
)

// Tier is the price of a range of prompt sizes.
type Tier struct {
	// MaxPromptTokens is the largest prompt (input + cached input + cache
	// write of one call) billed at this tier. 0 means no limit and is only
	// valid on the last tier.
	MaxPromptTokens int
	// Input and Output are the prices of uncached input and of output.
	Input, Output string
	// CachedInput and CacheWrite are "" when the provider does not price that
	// class separately; it is then billed at Input.
	CachedInput, CacheWrite string
}

// PriceVersion is the price of a model from EffectiveFrom on.
type PriceVersion struct {
	// EffectiveFrom is in UTC.
	EffectiveFrom time.Time
	// Tiers are in ascending MaxPromptTokens; the last one has no limit.
	Tiers []Tier
}

// Model is one model of a provider.
type Model struct {
	Provider string
	ID       string
	// Deprecated models are not offered for a new choice; a provider row that
	// already uses one keeps working. A model the provider still serves is
	// deprecated, never removed (removing it switches the builder off on
	// every installation that uses it).
	Deprecated bool
	// Prices are in ascending EffectiveFrom.
	Prices []PriceVersion
}

// Covers reports whether models and prices of provider come from the catalog.
func Covers(provider string) bool {
	return provider == ProviderOpenAI || provider == ProviderAnthropic
}

// Models returns the models of provider in catalog order (nil for a provider
// the catalog does not cover). The slice is a copy.
func Models(provider string) []Model {
	var out []Model
	for _, m := range models {
		if m.Provider == provider {
			out = append(out, m)
		}
	}
	return out
}

// All returns every model of the catalog, in catalog order. The slice is a copy.
func All() []Model {
	return slices.Clone(models)
}

// Lookup finds a model by provider and ID.
func Lookup(provider, id string) (Model, bool) {
	for _, m := range models {
		if m.Provider == provider && m.ID == id {
			return m, true
		}
	}
	return Model{}, false
}

// PriceAt returns the price version in force at t: the last one whose
// EffectiveFrom is not after t. ok is false before the first version.
func (m Model) PriceAt(t time.Time) (PriceVersion, bool) {
	var (
		found PriceVersion
		ok    bool
	)
	for _, v := range m.Prices {
		if v.EffectiveFrom.After(t) {
			break
		}
		found, ok = v, true
	}
	return found, ok
}

// TierFor returns the tier a prompt of promptTokens falls in. A version
// always has at least one tier (the catalog test checks it).
func (v PriceVersion) TierFor(promptTokens int) Tier {
	for _, t := range v.Tiers {
		if t.MaxPromptTokens == 0 || promptTokens <= t.MaxPromptTokens {
			return t
		}
	}
	return v.Tiers[len(v.Tiers)-1]
}
