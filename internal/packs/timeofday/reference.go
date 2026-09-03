package timeofday

import (
	"fmt"
	"math"

	"google.golang.org/protobuf/proto"

	assayv1 "github.com/bcypher01/assay/gen/go/assay/v1"
	"github.com/bcypher01/assay/internal/core"
	"github.com/bcypher01/assay/internal/primitive"
)

func init() {
	primitive.MustRegister(PeriodHigh{})
	primitive.MustRegister(PeriodLow{})
	primitive.MustRegister(OpeningRangeHigh{})
	primitive.MustRegister(OpeningRangeLow{})
	primitive.MustRegister(SessionVWAP{})
}

// PeriodHigh and PeriodLow expose the extreme of a completed bar of their own
// timeframe: lookback 0 is the bar that has just closed, 1 the one before it.
//
// They are written to be read from a finer chart. A D1 node projected onto an
// M15 entry series holds the previous day's high or low, because only closed
// coarse bars are ever visible to a fine bar — which is what a trader means by
// PDH and PDL. On W1 the same node is PWH and PWL. There is no monthly
// timeframe, so a monthly reference is not expressible yet.
type PeriodHigh struct{}
type PeriodLow struct{}

func (PeriodHigh) Spec() primitive.Spec {
	return primitive.Spec{Pack: "timeofday", Type: "period_high", Out: primitive.KindLevel, Params: (*assayv1.PeriodExtremeParams)(nil).ProtoReflect().Descriptor(), Timeframed: true}
}
func (PeriodLow) Spec() primitive.Spec {
	return primitive.Spec{Pack: "timeofday", Type: "period_low", Out: primitive.KindLevel, Params: (*assayv1.PeriodExtremeParams)(nil).ProtoReflect().Descriptor(), Timeframed: true}
}
func (PeriodHigh) New(p proto.Message, _ core.TF) (primitive.Instance, error) {
	n, err := lookbackOf(p)
	return periodExtreme{lookback: n, high: true}, err
}
func (PeriodLow) New(p proto.Message, _ core.TF) (primitive.Instance, error) {
	n, err := lookbackOf(p)
	return periodExtreme{lookback: n}, err
}

func lookbackOf(p proto.Message) (int, error) {
	x, ok := p.(*assayv1.PeriodExtremeParams)
	if !ok {
		return 0, fmt.Errorf("timeofday: invalid period extreme parameters")
	}
	if x.Lookback > 1000 {
		return 0, fmt.Errorf("timeofday: lookback must not exceed 1000 bars")
	}
	return int(x.Lookback), nil
}

type periodExtreme struct {
	lookback int
	high     bool
}

func (x periodExtreme) Eval(_ *primitive.EvalCtx, s *core.Series) (primitive.Value, error) {
	levels := make([]core.Price, s.Len())
	valid := core.NewBitset(s.Len())
	for i := x.lookback; i < s.Len(); i++ {
		if x.high {
			levels[i] = s.High[i-x.lookback]
		} else {
			levels[i] = s.Low[i-x.lookback]
		}
		valid.Set(i)
	}
	return primitive.Value{Kind: primitive.KindLevel, Level: levels, Valid: valid}, nil
}
func (periodExtreme) Overlays(core.Time, core.Time) []primitive.Overlay { return nil }

// OpeningRangeHigh and OpeningRangeLow expose the extreme of the first minutes
// after a session opens — the formulation session_high and session_low cannot
// express, because those cover the whole session.
//
// The level is invalid while the range is still forming, which is both the
// honest reading and the one that cannot look ahead. Once it closes it is the
// published level until the next range starts forming, at which point it goes
// invalid again — so a bar is never told "this is today's opening range" while
// today's is still being built.
type OpeningRangeHigh struct{}
type OpeningRangeLow struct{}

func (OpeningRangeHigh) Spec() primitive.Spec {
	return primitive.Spec{Pack: "timeofday", Type: "opening_range_high", Out: primitive.KindLevel, Params: (*assayv1.OpeningRangeParams)(nil).ProtoReflect().Descriptor(), Timeframed: true}
}
func (OpeningRangeLow) Spec() primitive.Spec {
	return primitive.Spec{Pack: "timeofday", Type: "opening_range_low", Out: primitive.KindLevel, Params: (*assayv1.OpeningRangeParams)(nil).ProtoReflect().Descriptor(), Timeframed: true}
}
func (OpeningRangeHigh) New(p proto.Message, _ core.TF) (primitive.Instance, error) {
	r, err := newOpeningRange(p)
	if err != nil {
		return nil, err
	}
	r.high = true
	return r, nil
}
func (OpeningRangeLow) New(p proto.Message, _ core.TF) (primitive.Instance, error) {
	return newOpeningRange(p)
}

type openingRange struct {
	start, minutes uint32
	high           bool
}

func newOpeningRange(p proto.Message) (openingRange, error) {
	x, ok := p.(*assayv1.OpeningRangeParams)
	if !ok || x.StartMinuteUtc > 1439 || x.RangeMinutes == 0 {
		return openingRange{}, fmt.Errorf("timeofday: invalid opening range")
	}
	// A window that would run past midnight is refused rather than wrapped:
	// wrapping it silently would make the range mean something different from
	// what the author wrote.
	if uint64(x.StartMinuteUtc)+uint64(x.RangeMinutes) > 1440 {
		return openingRange{}, fmt.Errorf("timeofday: opening range must end on the day it starts")
	}
	return openingRange{start: x.StartMinuteUtc, minutes: x.RangeMinutes}, nil
}

func (x openingRange) Eval(_ *primitive.EvalCtx, s *core.Series) (primitive.Value, error) {
	levels := make([]core.Price, s.Len())
	valid := core.NewBitset(s.Len())
	var forming, settled core.Price
	building, have := false, false
	for i, t := range s.Time {
		std := t.Std()
		minute := uint32(std.Hour()*60 + std.Minute())
		switch {
		case minute >= x.start && minute < x.start+x.minutes:
			// Inside the window the range is still forming. Nothing is
			// published — not the range being built, which would be look-ahead,
			// and not the previous one, which is no longer the current range.
			if !building {
				building = true
				forming = x.extreme(s, i, forming, true)
				continue
			}
			forming = x.extreme(s, i, forming, false)
			continue
		case building:
			building, settled, have = false, forming, true
		}
		if have {
			levels[i] = settled
			valid.Set(i)
		}
	}
	return primitive.Value{Kind: primitive.KindLevel, Level: levels, Valid: valid}, nil
}

func (x openingRange) extreme(s *core.Series, i int, current core.Price, first bool) core.Price {
	if x.high {
		if first {
			return s.High[i]
		}
		return core.Max(current, s.High[i])
	}
	if first {
		return s.Low[i]
	}
	return core.Min(current, s.Low[i])
}
func (openingRange) Overlays(core.Time, core.Time) []primitive.Overlay { return nil }

// SessionVWAP is the volume-weighted average price since the session opened.
//
// It is weighted by whatever the feed calls volume. Broker-native FX volume is
// a count of ticks, not traded size, so on those symbols this is a tick-weighted
// average and every surface that offers it has to say so — an unlabelled VWAP
// on FX quietly claims an authority it does not have.
//
// The value is published only inside the session and only once the session has
// traded, so it never carries yesterday's average into this morning.
type SessionVWAP struct{}

func (SessionVWAP) Spec() primitive.Spec {
	return primitive.Spec{Pack: "timeofday", Type: "session_vwap", Out: primitive.KindLevel, Params: (*assayv1.VwapParams)(nil).ProtoReflect().Descriptor(), Timeframed: true}
}
func (SessionVWAP) New(p proto.Message, _ core.TF) (primitive.Instance, error) {
	x, ok := p.(*assayv1.VwapParams)
	if !ok || x.StartMinuteUtc > 1439 || x.EndMinuteUtc > 1439 || x.StartMinuteUtc == x.EndMinuteUtc {
		return nil, fmt.Errorf("timeofday: invalid vwap session")
	}
	return vwap{session{x.StartMinuteUtc, x.EndMinuteUtc}}, nil
}

type vwap struct{ session session }

func (x vwap) Eval(_ *primitive.EvalCtx, s *core.Series) (primitive.Value, error) {
	levels := make([]core.Price, s.Len())
	valid := core.NewBitset(s.Len())
	// Typical price is accumulated as high+low+close and divided by three only
	// at the end, so the per-bar average is never rounded before it is weighted.
	var sumPV, sumV int64
	active, overflowed := false, false
	var key int64
	for i, t := range s.Time {
		if !x.session.contains(t) {
			active = false
			continue
		}
		if k := x.session.key(t); !active || k != key {
			active, key, overflowed = true, k, false
			sumPV, sumV = 0, 0
		}
		volume := s.Volume[i]
		if volume <= 0 {
			// A bar with no volume cannot move a weighted average. Publishing
			// the running value is correct; inventing a weight is not.
			if sumV > 0 && !overflowed {
				levels[i] = core.Price(sumPV / (3 * sumV))
				valid.Set(i)
			}
			continue
		}
		typical := int64(s.High[i] + s.Low[i] + s.Close[i])
		if overflowed || wouldOverflow(sumPV, typical, volume) || sumV > math.MaxInt64-volume {
			// A session whose weights no longer fit in fixed point reports
			// nothing for the rest of that session. A wrapped sum would look
			// like a price.
			overflowed = true
			continue
		}
		sumPV += typical * volume
		sumV += volume
		levels[i] = core.Price(sumPV / (3 * sumV))
		valid.Set(i)
	}
	return primitive.Value{Kind: primitive.KindLevel, Level: levels, Valid: valid}, nil
}
func (vwap) Overlays(core.Time, core.Time) []primitive.Overlay { return nil }

// wouldOverflow reports whether adding typical*volume to sum leaves int64.
func wouldOverflow(sum, typical, volume int64) bool {
	if typical == 0 || volume == 0 {
		return false
	}
	if typical < 0 || volume < 0 {
		return true
	}
	if typical > math.MaxInt64/volume {
		return true
	}
	return sum > math.MaxInt64-typical*volume
}
