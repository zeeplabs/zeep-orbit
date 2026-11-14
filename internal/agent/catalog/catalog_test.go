package catalog_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/zeeplabs/zeep-orbit/internal/agent/catalog"
	"github.com/zeeplabs/zeep-orbit/internal/agent/store"
)

// validate is the rule set every release of the catalog must pass. It lives
// in the test package because it parses prices with store.ParsePrice, and
// store imports catalog.
func validate(models []catalog.Model, reviewedAt time.Time) error {
	seen := map[string]bool{}
	for _, m := range models {
		key := m.Provider + "/" + m.ID
		switch {
		case !catalog.Covers(m.Provider):
			return fmt.Errorf("%s: unknown provider", key)
		case strings.TrimSpace(m.ID) == "" || m.ID != strings.TrimSpace(m.ID):
			return fmt.Errorf("%q: model ID must be non-empty and trimmed", key)
		case seen[key]:
			return fmt.Errorf("%s: duplicated", key)
		case len(m.Prices) == 0:
			return fmt.Errorf("%s: no price", key)
		}
		seen[key] = true
		for i, v := range m.Prices {
			if v.EffectiveFrom.IsZero() || v.EffectiveFrom.Location() != time.UTC {
				return fmt.Errorf("%s: version %d: EffectiveFrom must be a UTC time", key, i)
			}
			if i > 0 && !v.EffectiveFrom.After(m.Prices[i-1].EffectiveFrom) {
				return fmt.Errorf("%s: versions out of order", key)
			}
			if v.EffectiveFrom.After(reviewedAt) && i == 0 {
				return fmt.Errorf("%s: first price starts after ReviewedAt", key)
			}
			if len(v.Tiers) == 0 {
				return fmt.Errorf("%s: version %d has no tier", key, i)
			}
			for j, t := range v.Tiers {
				last := j == len(v.Tiers)-1
				if last != (t.MaxPromptTokens == 0) {
					return fmt.Errorf("%s: version %d: only the last tier is unbounded, and it must be", key, i)
				}
				if t.MaxPromptTokens < 0 || (j > 0 && !last && t.MaxPromptTokens <= v.Tiers[j-1].MaxPromptTokens) {
					return fmt.Errorf("%s: version %d: tiers out of order", key, i)
				}
				for _, p := range []struct {
					name, value string
					optional    bool
				}{{"input", t.Input, false}, {"output", t.Output, false}, {"cached input", t.CachedInput, true}, {"cache write", t.CacheWrite, true}} {
					if p.optional && p.value == "" {
						continue
					}
					if _, err := store.ParsePrice(p.value); err != nil {
						return fmt.Errorf("%s: version %d: %s price %q: %w", key, i, p.name, p.value, err)
					}
				}
			}
		}
	}
	return nil
}

func TestCatalogIsValid(t *testing.T) {
	if err := validate(catalog.All(), catalog.ReviewedAt); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{catalog.ProviderOpenAI, catalog.ProviderAnthropic} {
		if len(catalog.Models(p)) == 0 {
			t.Errorf("no models for %s", p)
		}
	}
}

func TestValidateRejectsBrokenData(t *testing.T) {
	day := func(d int) time.Time { return time.Date(2026, 11, d, 0, 0, 0, 0, time.UTC) }
	flat := func(input string) []catalog.Tier { return []catalog.Tier{{Input: input, Output: "1"}} }
	good := func() catalog.Model {
		return catalog.Model{Provider: "openai", ID: "m", Prices: []catalog.PriceVersion{{EffectiveFrom: day(1), Tiers: flat("1")}}}
	}
	cases := map[string]func() []catalog.Model{
		"unknown provider": func() []catalog.Model { m := good(); m.Provider = "gemini"; return []catalog.Model{m} },
		"empty id":         func() []catalog.Model { m := good(); m.ID = " "; return []catalog.Model{m} },
		"duplicated":       func() []catalog.Model { return []catalog.Model{good(), good()} },
		"no price":         func() []catalog.Model { m := good(); m.Prices = nil; return []catalog.Model{m} },
		"bad price":        func() []catalog.Model { m := good(); m.Prices[0].Tiers = flat("1,5"); return []catalog.Model{m} },
		"negative price":   func() []catalog.Model { m := good(); m.Prices[0].Tiers = flat("-1"); return []catalog.Model{m} },
		"no tier":          func() []catalog.Model { m := good(); m.Prices[0].Tiers = nil; return []catalog.Model{m} },
		"zero time": func() []catalog.Model {
			m := good()
			m.Prices[0].EffectiveFrom = time.Time{}
			return []catalog.Model{m}
		},
		"versions out of order": func() []catalog.Model {
			m := good()
			m.Prices = append(m.Prices, catalog.PriceVersion{EffectiveFrom: day(1), Tiers: flat("2")})
			return []catalog.Model{m}
		},
		"starts after review": func() []catalog.Model {
			m := good()
			m.Prices[0].EffectiveFrom = day(20)
			return []catalog.Model{m}
		},
		"last tier bounded": func() []catalog.Model {
			m := good()
			m.Prices[0].Tiers = []catalog.Tier{{MaxPromptTokens: 10, Input: "1", Output: "1"}}
			return []catalog.Model{m}
		},
		"middle tier unbounded": func() []catalog.Model {
			m := good()
			m.Prices[0].Tiers = []catalog.Tier{{Input: "1", Output: "1"}, {Input: "2", Output: "2"}}
			return []catalog.Model{m}
		},
		"tiers out of order": func() []catalog.Model {
			m := good()
			m.Prices[0].Tiers = []catalog.Tier{{MaxPromptTokens: 20, Input: "1", Output: "1"}, {MaxPromptTokens: 10, Input: "1", Output: "1"}, {Input: "2", Output: "2"}}
			return []catalog.Model{m}
		},
	}
	if err := validate([]catalog.Model{good()}, day(13)); err != nil {
		t.Fatalf("good model rejected: %v", err)
	}
	for name, build := range cases {
		if err := validate(build(), day(13)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestLookupAndCovers(t *testing.T) {
	if !catalog.Covers("openai") || !catalog.Covers("anthropic") || catalog.Covers("openai_compatible") {
		t.Fatal("Covers: wrong providers")
	}
	if _, ok := catalog.Lookup("openai", "gpt-4.1"); !ok {
		t.Error("gpt-4.1 not found")
	}
	if _, ok := catalog.Lookup("anthropic", "gpt-4.1"); ok {
		t.Error("lookup must match the provider")
	}
	if _, ok := catalog.Lookup("openai", "o3-mini"); ok {
		t.Error("o3-mini must not be in the catalog")
	}
	if catalog.Models("openai_compatible") != nil {
		t.Error("openai_compatible has no catalog")
	}
}

func TestPriceAtPicksTheVersionInForce(t *testing.T) {
	day := func(d int) time.Time { return time.Date(2026, 11, d, 0, 0, 0, 0, time.UTC) }
	m := catalog.Model{Prices: []catalog.PriceVersion{
		{EffectiveFrom: day(1), Tiers: []catalog.Tier{{Input: "1", Output: "1"}}},
		{EffectiveFrom: day(30), Tiers: []catalog.Tier{{Input: "2", Output: "2"}}},
	}}
	if _, ok := m.PriceAt(day(1).Add(-time.Second)); ok {
		t.Error("a time before the first version has no price")
	}
	for at, want := range map[time.Time]string{day(1): "1", day(29): "1", day(30): "2", day(30).AddDate(1, 0, 0): "2"} {
		v, ok := m.PriceAt(at)
		if !ok || v.Tiers[0].Input != want {
			t.Errorf("PriceAt(%s) = %q, want %q", at, v.Tiers[0].Input, want)
		}
	}
}

func TestTierForHaiku55(t *testing.T) {
	m, ok := catalog.Lookup("anthropic", "claude-haiku-5-5")
	if !ok {
		t.Fatal("claude-haiku-5-5 not found")
	}
	v, ok := m.PriceAt(catalog.ReviewedAt)
	if !ok {
		t.Fatal("no price at ReviewedAt")
	}
	for tokens, want := range map[int]string{0: "0.1", 100_000: "0.1", 100_001: "0.5", 900_000: "0.5"} {
		if got := v.TierFor(tokens).Input; got != want {
			t.Errorf("TierFor(%d).Input = %q, want %q", tokens, got, want)
		}
	}
}
