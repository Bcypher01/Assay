// Package core defines the value types the whole engine is built from:
// fixed-point prices and ratios, timestamps, timeframes, unit-carrying
// distances, the struct-of-arrays series, and the bitset.
//
// Nothing in this package allocates on a hot path, reads a clock, or touches
// the filesystem. See docs/conventions.md.
package core

import (
	"math"
	"math/bits"
)

// Price is a fixed-point price expressed as a signed count of instrument
// ticks. A Price is only meaningful alongside the instrument.Spec it was
// created from — conversion to and from decimal lives on that type.
//
// Ticks rather than a universal scale means precision automatically matches
// the instrument: a pair quoted to five decimals and a coin quoted to eight
// are both exact, with no shared scale to overflow or truncate.
type Price int64

// Abs returns the magnitude of p.
func (p Price) Abs() Price {
	if p < 0 {
		return -p
	}
	return p
}

// Sign returns -1, 0 or 1.
func (p Price) Sign() int {
	switch {
	case p < 0:
		return -1
	case p > 0:
		return 1
	default:
		return 0
	}
}

// Min returns the smaller of a and b.
func Min(a, b Price) Price {
	if a < b {
		return a
	}
	return b
}

// Max returns the larger of a and b.
func Max(a, b Price) Price {
	if a > b {
		return a
	}
	return b
}

// RatioScale is the fixed-point scale of Ratio: values are stored in units of
// 1e-9.
const RatioScale int64 = 1_000_000_000

// Ratio is a fixed-point ratio in units of 1e-9, used for R-multiples,
// percentages and any multiplier applied to a Price. Fixed point rather than
// float64 so that a study is reproducible bit for bit across platforms.
type Ratio int64

// RatioFromFloat converts a float64 to a Ratio, rounding half away from zero.
// This is a conversion helper at an input boundary; do not use it inside the
// engine.
func RatioFromFloat(f float64) Ratio {
	scaled := f * float64(RatioScale)
	if scaled < 0 {
		return Ratio(int64(scaled - 0.5))
	}
	return Ratio(int64(scaled + 0.5))
}

// RatioFromInt returns a Ratio equal to the whole number n.
func RatioFromInt(n int64) Ratio { return Ratio(n * RatioScale) }

// Float64 converts r to a float64. Display and statistics only.
func (r Ratio) Float64() float64 { return float64(r) / float64(RatioScale) }

// One is the Ratio 1.0.
const One Ratio = Ratio(RatioScale)

// MulPrice returns p scaled by r, rounding half away from zero.
func (r Ratio) MulPrice(p Price) Price {
	return Price(mulDivRound(int64(p), int64(r), RatioScale))
}

// Mul returns r*o.
func (r Ratio) Mul(o Ratio) Ratio {
	return Ratio(mulDivRound(int64(r), int64(o), RatioScale))
}

// DivPrice returns a/b as a Ratio, rounding half away from zero. Returns 0
// when b is 0 — callers that care must check first.
func DivPrice(a, b Price) Ratio {
	if b == 0 {
		return 0
	}
	return Ratio(mulDivRound(int64(a), RatioScale, int64(b)))
}

// mulDivRound computes a*b/d in 128-bit intermediate precision, rounding half
// away from zero. d must be non-zero. On overflow of the 64-bit result it
// saturates rather than panicking; the inputs required to reach that are far
// outside any real instrument.
func mulDivRound(a, b, d int64) int64 {
	if d == 0 {
		return 0
	}
	neg := false
	ua, ub, ud := uint64(a), uint64(b), uint64(d)
	if a < 0 {
		ua = uint64(-a)
		neg = !neg
	}
	if b < 0 {
		ub = uint64(-b)
		neg = !neg
	}
	if d < 0 {
		ud = uint64(-d)
		neg = !neg
	}

	hi, lo := bits.Mul64(ua, ub)

	// Round half away from zero by adding d/2 before the divide.
	var carry uint64
	lo, carry = bits.Add64(lo, ud/2, 0)
	hi += carry

	if hi >= ud {
		return saturate(neg)
	}

	q, _ := bits.Div64(hi, lo, ud)
	if q > uint64(math.MaxInt64) {
		return saturate(neg)
	}
	if neg {
		return -int64(q)
	}
	return int64(q)
}

func saturate(neg bool) int64 {
	if neg {
		return math.MinInt64 + 1
	}
	return math.MaxInt64
}
