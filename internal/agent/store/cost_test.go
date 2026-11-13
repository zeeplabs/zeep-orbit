package store

import (
	"errors"
	"math"
	"testing"
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
