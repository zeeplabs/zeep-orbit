package store

// cost.go — money arithmetic of the usage ledger (ai-builder-costs C03).
// Everything is integer: prices are micros per million tokens, costs are
// micros (1 unit of currency = 1,000,000 micros). No floating point touches
// money.

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"strings"
)

// MicrosPerUnit is how many micros make one unit of currency. It is also the
// number of tokens a price refers to (prices are per million tokens).
const MicrosPerUnit = 1_000_000

// maxPriceIntDigits is the integer part NUMERIC(18,6) can hold.
const maxPriceIntDigits = 12

// ErrInvalidPrice is returned by ParsePrice for a value that is not a
// non-negative decimal with at most 6 decimal places.
var ErrInvalidPrice = errors.New("price must be a non-negative decimal with at most 6 decimal places")

// Price is the price of one model, in micros per million tokens. A nil
// CachedInput or CacheWrite means "not set": that class is billed at Input.
type Price struct {
	Input       int64
	CachedInput *int64
	CacheWrite  *int64
	Output      int64
}

// ParsePrice converts a decimal string ("3", "0.15", "1.250000") into micros.
// It accepts what NUMERIC(18,6) can store and nothing else: no sign, no
// exponent, at most 12 integer digits and 6 decimals.
func ParsePrice(s string) (int64, error) {
	s = strings.TrimSpace(s)
	whole, frac, _ := strings.Cut(s, ".")
	if whole == "" && frac == "" {
		return 0, ErrInvalidPrice
	}
	if strings.HasSuffix(s, ".") || len(whole) > maxPriceIntDigits || len(frac) > 6 {
		return 0, ErrInvalidPrice
	}
	var v int64
	for _, part := range []string{whole, frac + strings.Repeat("0", 6-len(frac))} {
		for _, r := range part {
			if r < '0' || r > '9' {
				return 0, ErrInvalidPrice
			}
			v = v*10 + int64(r-'0')
		}
	}
	return v, nil
}

// FormatPrice renders micros as a decimal string with trailing zeros
// trimmed, keeping at least two decimals ("3.00", "0.15", "1.2345").
func FormatPrice(micros int64) string {
	whole, frac := micros/MicrosPerUnit, micros%MicrosPerUnit
	f := strings.TrimRight(fmt.Sprintf("%06d", frac), "0")
	for len(f) < 2 {
		f += "0"
	}
	return fmt.Sprintf("%d.%s", whole, f)
}

// CostMicros is the cost of one call. Each token class is multiplied by its
// price and the sum is divided by one million once, rounding half up, so the
// rounding error of a call is below one micro whatever the mix of classes.
// It fails only on negative inputs or a result that does not fit int64.
func CostMicros(p Price, input, cachedInput, cacheWrite, output int) (int64, error) {
	if input < 0 || cachedInput < 0 || cacheWrite < 0 || output < 0 {
		return 0, fmt.Errorf("%w: token counts must not be negative", ErrInvalidArgument)
	}
	cached, write := p.Input, p.Input
	if p.CachedInput != nil {
		cached = *p.CachedInput
	}
	if p.CacheWrite != nil {
		write = *p.CacheWrite
	}
	if p.Input < 0 || cached < 0 || write < 0 || p.Output < 0 {
		return 0, fmt.Errorf("%w: prices must not be negative", ErrInvalidArgument)
	}
	sum := new(big.Int)
	for _, term := range [][2]int64{{int64(input), p.Input}, {int64(cachedInput), cached}, {int64(cacheWrite), write}, {int64(output), p.Output}} {
		sum.Add(sum, new(big.Int).Mul(big.NewInt(term[0]), big.NewInt(term[1])))
	}
	// Half up: add half of the divisor before the integer division.
	sum.Add(sum, big.NewInt(MicrosPerUnit/2))
	sum.Quo(sum, big.NewInt(MicrosPerUnit))
	if !sum.IsInt64() || sum.Int64() > math.MaxInt64/2 {
		return 0, fmt.Errorf("%w: cost out of range", ErrInvalidArgument)
	}
	return sum.Int64(), nil
}
