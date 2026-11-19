package store

import (
	"errors"
	"math"
	"testing"
	"time"
)

func ptr(v int64) *int64 { return &v }

func TestParsePrice(t *testing.T) {
	ok := map[string]int64{
		"3":                   3_000_000,
		"3.0":                 3_000_000,
		"0.15":                150_000,
		"1.250000":            1_250_000,
		"0.000001":            1,
		"0":                   0,
		".5":                  500_000,
		" 2.5 ":               2_500_000,
		"999999999999.999999": 999_999_999_999_999_999,
	}
	for in, want := range ok {
		got, err := ParsePrice(in)
		if err != nil || got != want {
			t.Errorf("ParsePrice(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, in := range []string{"", ".", "-1", "+1", "1e3", "1,5", "1.", "abc", "1.0000001", "1234567890123", "1.2.3", "１"} {
		if _, err := ParsePrice(in); !errors.Is(err, ErrInvalidPrice) {
			t.Errorf("ParsePrice(%q) error = %v, want ErrInvalidPrice", in, err)
		}
	}
}

func TestFormatPrice(t *testing.T) {
	for in, want := range map[int64]string{3_000_000: "3.00", 150_000: "0.15", 1_234_500: "1.2345", 1: "0.000001", 0: "0.00"} {
		if got := FormatPrice(in); got != want {
			t.Errorf("FormatPrice(%d) = %q, want %q", in, got, want)
		}
		if back, err := ParsePrice(FormatPrice(in)); err != nil || back != in {
			t.Errorf("round trip of %d = %d, %v", in, back, err)
		}
	}
}

func TestCostMicros(t *testing.T) {
	// 3.00 in, 0.30 cached, 3.75 cache write, 15.00 out, per million tokens.
	full := Price{Input: 3_000_000, CachedInput: ptr(300_000), CacheWrite: ptr(3_750_000), Output: 15_000_000}
	cases := []struct {
		name                   string
		p                      Price
		in, cached, write, out int
		want                   int64
	}{
		{"one million of each class", full, 1_000_000, 1_000_000, 1_000_000, 1_000_000, 3_000_000 + 300_000 + 3_750_000 + 15_000_000},
		{"input only", full, 1000, 0, 0, 0, 3000},
		{"output only", full, 0, 0, 0, 1000, 15000},
		{"a hand-checked mixed call", full, 86, 1920, 0, 300, 5334}, // 258 + 576 + 0 + 4500
		{"cached falls back to the input price", Price{Input: 3_000_000, Output: 15_000_000}, 0, 1000, 0, 0, 3000},
		{"cache write falls back to the input price", Price{Input: 3_000_000, Output: 15_000_000}, 0, 0, 1000, 0, 3000},
		{"an explicit zero cached price is free, not a fallback", Price{Input: 3_000_000, CachedInput: ptr(0), Output: 15_000_000}, 0, 1000, 0, 0, 0},
		{"zero prices cost zero", Price{}, 5000, 5000, 5000, 5000, 0},
		{"no tokens", full, 0, 0, 0, 0, 0},
		{"rounds half up at exactly half a micro", Price{Input: 500_000}, 1, 0, 0, 0, 1},
		{"rounds down just below half a micro", Price{Input: 499_999}, 1, 0, 0, 0, 0},
		{"rounds once over the sum, not per class", Price{Input: 250_000, Output: 250_000}, 1, 0, 0, 1, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := CostMicros(tc.p, tc.in, tc.cached, tc.write, tc.out)
			if err != nil || got != tc.want {
				t.Fatalf("CostMicros = %d, %v; want %d", got, err, tc.want)
			}
		})
	}
}

func TestCostMicrosRejectsBadInput(t *testing.T) {
	if _, err := CostMicros(Price{Input: 1}, -1, 0, 0, 0); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("negative tokens: %v", err)
	}
	if _, err := CostMicros(Price{Input: -1}, 1, 0, 0, 0); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("negative price: %v", err)
	}
	// The largest price NUMERIC(18,6) holds times the largest token count: far
	// outside int64, must be an error and not a wrapped number.
	if _, err := CostMicros(Price{Input: 999_999_999_999_999_999}, math.MaxInt32, 0, 0, 0); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("overflow: %v", err)
	}
}

// --- ai-model-catalog M02: catalog prices (AMC-06, AMC-07) ---

func TestCatalogCost(t *testing.T) {
	at := time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)
	cost := func(provider, model string, in, cached, write, out int, when time.Time) *int64 {
		return catalogCost(UsageRecord{Provider: provider, Model: model,
			InputTokens: in, CachedInputTokens: cached, CacheWriteTokens: write, OutputTokens: out}, when)
	}

	// gpt-4.1: 2 in, 0.5 cached, no cache write price (billed at input), 8 out.
	// 1000*2 + 10000*0.5 + 0*2 + 500*8 = 11000 micros.
	if got := cost("openai", "gpt-4.1", 1000, 10_000, 0, 500, at); got == nil || *got != 11_000 {
		t.Errorf("gpt-4.1 cost = %v, want 11000", got)
	}
	// claude-sonnet-5-5: 2 in, 2.5 write, 0.1 cached, 10 out.
	// 1000*2 + 10000*0.1 + 2000*2.5 + 500*10 = 13000 micros.
	if got := cost("anthropic", "claude-sonnet-5-5", 1000, 10_000, 2000, 500, at); got == nil || *got != 13_000 {
		t.Errorf("claude-sonnet-5-5 cost = %v, want 13000", got)
	}
	if got := cost("openai", "o3-mini", 1000, 0, 0, 0, at); got != nil {
		t.Errorf("a model outside the catalog must have no price, got %d", *got)
	}
	if got := cost("anthropic", "gpt-4.1", 1000, 0, 0, 0, at); got != nil {
		t.Errorf("the provider is part of the key, got %d", *got)
	}
	if got := cost("openai", "gpt-4.1", 1000, 0, 0, 0, time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)); got != nil {
		t.Errorf("before the first price version there is no price, got %d", *got)
	}

	// Haiku 5.5 tiers by prompt size (input + cached + cache write).
	// 100,000 tokens: lower tier, 60000*0.1 + 30000*0.01 + 10000*0.125 + 1000*0.5 = 8050.
	if got := cost("anthropic", "claude-haiku-5-5", 60_000, 30_000, 10_000, 1000, at); got == nil || *got != 8050 {
		t.Errorf("haiku 5.5 at 100000 prompt tokens = %v, want 8050", got)
	}
	// 100,001 tokens: every class at the upper tier,
	// 60001*0.5 + 30000*0.05 + 10000*0.625 + 1000*2.5 = 40250.5 -> 40251 (half up).
	if got := cost("anthropic", "claude-haiku-5-5", 60_001, 30_000, 10_000, 1000, at); got == nil || *got != 40_251 {
		t.Errorf("haiku 5.5 at 100001 prompt tokens = %v, want 40251", got)
	}
}
