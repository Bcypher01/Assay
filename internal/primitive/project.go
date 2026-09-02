package primitive

import (
	"github.com/bcypher01/assay/internal/core"
)

// StalePolicy governs how long a projected coarse value remains usable once
// its bar has closed.
//
// Holding a value across the fine bars inside its coarse bar is the whole
// point of projection. Holding it indefinitely across a gap is not: a Friday
// daily bias should not silently govern Monday morning unless the author said
// so. Past MaxAge the projection reports invalid rather than stale, so gaps
// across weekends, holidays and halts show up in the run report instead of
// quietly propagating.
type StalePolicy struct {
	// MaxAge bounds the span between a coarse bar's close and the entry bar's
	// close. Zero selects the default: one coarse period plus one entry bar of
	// tolerance, which holds a value for exactly its own bar and one further
	// entry bar when the next coarse bar is missing.
	MaxAge core.Duration
	// Hold keeps a projected value usable indefinitely. It is an explicit
	// opt-in, never a default.
	Hold bool
}

// maxAge resolves the effective bound for a projection from tf onto to.
func (p StalePolicy) maxAge(from, to core.TF) core.Duration {
	if p.MaxAge > 0 {
		return p.MaxAge
	}
	return core.TFDuration(from) + core.TFDuration(to)
}

// Project maps a Value computed on a coarser series onto the index space of a
// finer one by a backward as-of join on close time.
//
// For entry bar i the decision is taken at that bar's close, so the visible
// coarse bar is
//
//	j(i) = max{ j : from.TF.CloseOf(from.Time[j]) <= to.TF.CloseOf(to.Time[i]) }
//
// The comparison is inclusive, and that is the whole subtlety. Because the
// timeframes nest, an H4 bar closing at 12:00 closes at the same instant as
// the M15 bar spanning 11:45-12:00. That H4 close is fully determined by data
// at or before 12:00, so the M15 bar at that index may see it. Joining on the
// entry bar's open time, or comparing strictly, would delay every coarse
// signal by one entry bar — systematically, and in the direction that makes a
// backtest differ from what the trader actually did.
//
// Before the first coarse bar closes no j exists and the result is invalid.
// Warmup is never zero-filled.
//
// Project is a pure function of the coarse Value and the two time arrays: one
// forward scan, no maps, no clock.
func Project(v Value, from, to *core.Series, policy StalePolicy) (Value, error) {
	if from == nil || to == nil {
		return Value{}, ErrInvalidSpec
	}
	if from.TF == to.TF {
		if err := v.Validate(to.Len()); err != nil {
			return Value{}, err
		}
		return v, nil
	}
	if !from.TF.Coarser(to.TF) {
		return Value{}, ErrTimeframe
	}
	if err := v.Validate(from.Len()); err != nil {
		return Value{}, err
	}
	n := to.Len()
	out := Value{Kind: v.Kind, Valid: core.NewBitset(n)}
	switch v.Kind {
	case KindBool:
		out.Bool = core.NewBitset(n)
	case KindLevel:
		out.Level = make([]core.Price, n)
	case KindZone:
		out.Zone = make([]Zone, n)
	case KindNumber:
		out.Number = make([]core.Ratio, n)
	case KindState:
		out.State = make([]uint32, n)
	}
	maxAge := policy.maxAge(from.TF, to.TF)
	j := -1
	for i := 0; i < n; i++ {
		closeAt := to.TF.CloseOf(to.Time[i])
		for j+1 < from.Len() && from.TF.CloseOf(from.Time[j+1]) <= closeAt {
			j++
		}
		if j < 0 {
			continue // Warmup: no coarse bar has closed yet.
		}
		if v.Valid != nil && !v.Valid.Get(j) {
			continue
		}
		if !policy.Hold && core.Duration(closeAt-from.TF.CloseOf(from.Time[j])) > maxAge {
			continue
		}
		switch v.Kind {
		case KindBool:
			if v.Bool.Get(j) {
				out.Bool.Set(i)
			}
		case KindLevel:
			out.Level[i] = v.Level[j]
		case KindZone:
			out.Zone[i] = v.Zone[j]
		case KindNumber:
			out.Number[i] = v.Number[j]
		case KindState:
			out.State[i] = v.State[j]
		}
		out.Valid.Set(i)
	}
	return out, nil
}
