package smc

import (
	"fmt"

	"google.golang.org/protobuf/proto"

	assayv1 "github.com/bcypher01/assay/gen/go/assay/v1"
	"github.com/bcypher01/assay/internal/core"
	"github.com/bcypher01/assay/internal/primitive"
)

func init() {
	for _, p := range []primitive.Primitive{
		MarketStructure{}, MarketStructureBullish{}, MarketStructureBearish{},
		Displacement{}, FairValueGap{}, OrderBlock{}, Breaker{}, LiquidityPool{},
		LiquiditySweep{}, LiquiditySweepBullish{}, LiquiditySweepBearish{}, PremiumDiscount{},
	} {
		primitive.MustRegister(p)
	}
}

func spec(name string, out primitive.Kind, p proto.Message) primitive.Spec {
	return primitive.Spec{Pack: "smc", Type: name, Out: out, Params: p.ProtoReflect().Descriptor(), Timeframed: true}
}

// MarketStructure marks either direction of a close/wick break of the latest
// confirmed symmetric swing. Use its directional variants when a strategy must
// distinguish bullish and bearish structure breaks.
type MarketStructure struct{}

func (MarketStructure) Spec() primitive.Spec {
	return spec("market_structure", primitive.KindBool, (*assayv1.MarketStructureParams)(nil))
}
func (MarketStructure) New(p proto.Message, _ core.TF) (primitive.Instance, error) {
	return newMarketStructure(p, 0)
}

// MarketStructureBullish marks only upward breaks of confirmed swing highs.
type MarketStructureBullish struct{}

func (MarketStructureBullish) Spec() primitive.Spec {
	return spec("market_structure_bullish", primitive.KindBool, (*assayv1.MarketStructureParams)(nil))
}
func (MarketStructureBullish) New(p proto.Message, _ core.TF) (primitive.Instance, error) {
	return newMarketStructure(p, core.SideLong)
}

// MarketStructureBearish marks only downward breaks of confirmed swing lows.
type MarketStructureBearish struct{}

func (MarketStructureBearish) Spec() primitive.Spec {
	return spec("market_structure_bearish", primitive.KindBool, (*assayv1.MarketStructureParams)(nil))
}
func (MarketStructureBearish) New(p proto.Message, _ core.TF) (primitive.Instance, error) {
	return newMarketStructure(p, core.SideShort)
}

func newMarketStructure(p proto.Message, side core.Side) (primitive.Instance, error) {
	x, ok := p.(*assayv1.MarketStructureParams)
	if !ok || x.SwingLookback == 0 {
		return nil, fmt.Errorf("smc: swing lookback must be positive")
	}
	internal := int(x.SwingLookback) / 2
	if internal == 0 {
		internal = 1
	}
	return &marketStructure{n: int(x.SwingLookback), internalN: internal, trackInternal: x.TrackInternal, close: x.ConfirmByClose, tolerance: core.Price(x.EqualLevelToleranceTicks), side: side}, nil
}

type marketStructure struct {
	n, internalN  int
	trackInternal bool
	close         bool
	tolerance     core.Price
	side          core.Side
	overlays      []primitive.Overlay
}

func (x *marketStructure) Eval(_ *primitive.EvalCtx, s *core.Series) (primitive.Value, error) {
	b, v := core.NewBitset(s.Len()), core.NewBitset(s.Len())
	var high, low core.Price
	haveHigh, haveLow := false, false
	for i := 0; i < s.Len(); i++ {
		if i >= x.n*2 {
			v.Set(i)
		}
		consider := func(lookback int, label string) {
			pivot := i - lookback
			if pivot < lookback || pivot+lookback >= s.Len() {
				return
			}
			ph, pl := true, true
			for j := pivot - lookback; j <= pivot+lookback; j++ {
				if j != pivot {
					ph = ph && s.High[pivot] > s.High[j]
					pl = pl && s.Low[pivot] < s.Low[j]
				}
			}
			if ph {
				high, haveHigh = s.High[pivot], true
			}
			if pl {
				low, haveLow = s.Low[pivot], true
			}
			_ = label
		}
		consider(x.n, "swing")
		if x.trackInternal && x.internalN != x.n {
			consider(x.internalN, "internal")
		}
		up, down := s.High[i], s.Low[i]
		if x.close {
			up, down = s.Close[i], s.Close[i]
		}
		if haveHigh && up > high+x.tolerance {
			if x.matches(core.SideLong) {
				b.Set(i)
			}
			x.overlays = append(x.overlays, primitive.Overlay{Kind: primitive.OverlayMarker, From: s.Time[i], To: s.Time[i], Price: high, Text: "BOS up"})
			haveHigh = false
		}
		if haveLow && down < low-x.tolerance {
			if x.matches(core.SideShort) {
				b.Set(i)
			}
			x.overlays = append(x.overlays, primitive.Overlay{Kind: primitive.OverlayMarker, From: s.Time[i], To: s.Time[i], Price: low, Text: "BOS down"})
			haveLow = false
		}
	}
	return primitive.Value{Kind: primitive.KindBool, Bool: b, Valid: v}, nil
}
func (x *marketStructure) matches(side core.Side) bool { return x.side == 0 || x.side == side }
func (x *marketStructure) Overlays(from, to core.Time) []primitive.Overlay {
	return clip(x.overlays, from, to)
}

type Displacement struct{}

func (Displacement) Spec() primitive.Spec {
	return spec("displacement", primitive.KindBool, (*assayv1.DisplacementParams)(nil))
}
func (Displacement) New(p proto.Message, _ core.TF) (primitive.Instance, error) {
	x, ok := p.(*assayv1.DisplacementParams)
	if !ok || x.MinBodyRatioMilli == 0 || x.MinBodyRatioMilli > 1000 || x.MinConsecutiveBars == 0 {
		return nil, fmt.Errorf("smc: invalid displacement parameters")
	}
	return &displacement{ratio: int(x.MinBodyRatioMilli), consecutive: int(x.MinConsecutiveBars), gap: x.RequireGap}, nil
}

type displacement struct {
	ratio, consecutive int
	gap                bool
	overlays           []primitive.Overlay
}

func (x *displacement) Eval(_ *primitive.EvalCtx, s *core.Series) (primitive.Value, error) {
	b, v := core.NewBitset(s.Len()), core.NewBitset(s.Len())
	run := 0
	for i := range s.Time {
		r := s.High[i] - s.Low[i]
		body := (s.Close[i] - s.Open[i]).Abs()
		qualifies := r > 0 && body*1000 >= r*core.Price(x.ratio)
		if x.gap {
			qualifies = qualifies && i > 0 && (s.Low[i] > s.High[i-1] || s.High[i] < s.Low[i-1])
		}
		if qualifies {
			run++
		} else {
			run = 0
		}
		if run >= x.consecutive {
			b.Set(i)
			x.overlays = append(x.overlays, primitive.Overlay{Kind: primitive.OverlayMarker, From: s.Time[i], To: s.Time[i], Price: s.Close[i], Text: "displacement"})
		}
		v.Set(i)
	}
	return primitive.Value{Kind: primitive.KindBool, Bool: b, Valid: v}, nil
}
func (x *displacement) Overlays(from, to core.Time) []primitive.Overlay {
	return clip(x.overlays, from, to)
}

// FairValueGap detects three-candle imbalances and reports their lifecycle in
// the zone stream. A zone is live from its creating bar until mitigation or
// expiry; each bar receives the newest live zone, avoiding ambiguous arrays.
type FairValueGap struct{}

func (FairValueGap) Spec() primitive.Spec {
	return spec("fair_value_gap", primitive.KindZone, (*assayv1.FairValueGapParams)(nil))
}
func (FairValueGap) New(p proto.Message, _ core.TF) (primitive.Instance, error) {
	x, ok := p.(*assayv1.FairValueGapParams)
	if !ok || x.MinSizeTicks == 0 || x.ExpiryBars == 0 || x.MitigationRule == assayv1.MitigationRule_MITIGATION_RULE_UNSPECIFIED {
		return nil, fmt.Errorf("smc: invalid fair value gap parameters")
	}
	return &fairValueGap{min: core.Price(x.MinSizeTicks), rule: x.MitigationRule, expiry: int(x.ExpiryBars), inverted: x.TrackInverted}, nil
}

type fairValueGap struct {
	min      core.Price
	rule     assayv1.MitigationRule
	expiry   int
	inverted bool
	overlays []primitive.Overlay
}
type liveZone struct {
	zone primitive.Zone
	born int
}

func (x *fairValueGap) Eval(_ *primitive.EvalCtx, s *core.Series) (primitive.Value, error) {
	out := make([]primitive.Zone, s.Len())
	valid := core.NewBitset(s.Len())
	live := make([]liveZone, 0)
	for i := range s.Time {
		var transition *primitive.Zone
		for j := 0; j < len(live); {
			z := &live[j]
			if i-z.born > x.expiry {
				live = append(live[:j], live[j+1:]...)
				continue
			}
			if i > z.born && mitigated(*z, s, i, x.rule) {
				z.zone.Mitigated, z.zone.MitigatedAt = true, s.Time[i]
				z.zone.To = s.Time[i]
				copy := z.zone
				transition = &copy
				x.overlays = append(x.overlays, primitive.Overlay{Kind: primitive.OverlayZoneBox, From: copy.From, To: copy.To, Price: copy.Low, Price2: copy.High, Text: "FVG mitigated"})
				live = append(live[:j], live[j+1:]...)
				continue
			}
			j++
		}
		if i >= 2 {
			var z primitive.Zone
			found := false
			if s.Low[i]-s.High[i-2] >= x.min {
				z = primitive.Zone{Low: s.High[i-2], High: s.Low[i], From: s.Time[i], To: s.Time[x.expiryOrLast(i, s.Len())], Side: core.SideLong}
				found = true
			} else if x.inverted && s.Low[i-2]-s.High[i] >= x.min {
				z = primitive.Zone{Low: s.High[i], High: s.Low[i-2], From: s.Time[i], To: s.Time[x.expiryOrLast(i, s.Len())], Side: core.SideShort}
				found = true
			}
			if found {
				live = append(live, liveZone{zone: z, born: i})
				x.overlays = append(x.overlays, primitive.Overlay{Kind: primitive.OverlayZoneBox, From: z.From, To: z.To, Price: z.Low, Price2: z.High, Text: "FVG"})
			}
		}
		if transition != nil {
			out[i] = *transition
			valid.Set(i)
		} else if len(live) > 0 {
			out[i] = live[len(live)-1].zone
			valid.Set(i)
		}
	}
	return primitive.Value{Kind: primitive.KindZone, Zone: out, Valid: valid}, nil
}
func (x *fairValueGap) expiryOrLast(i, n int) int {
	if i+x.expiry >= n {
		return n - 1
	}
	return i + x.expiry
}
func (x *fairValueGap) Overlays(from, to core.Time) []primitive.Overlay {
	return clip(x.overlays, from, to)
}
func mitigated(z liveZone, s *core.Series, i int, rule assayv1.MitigationRule) bool {
	mid := z.zone.Low + (z.zone.High-z.zone.Low)/2
	switch rule {
	case assayv1.MitigationRule_MITIGATION_RULE_FIRST_TOUCH:
		return s.Low[i] <= z.zone.High && s.High[i] >= z.zone.Low
	case assayv1.MitigationRule_MITIGATION_RULE_HALF_FILL:
		if z.zone.Side == core.SideLong {
			return s.Low[i] <= mid
		}
		return s.High[i] >= mid
	default:
		if z.zone.Side == core.SideLong {
			return s.Low[i] <= z.zone.Low
		}
		return s.High[i] >= z.zone.High
	}
}

// OrderBlock reports the last opposing candle before a confirmed displacement.
// It is emitted on the confirming bar, rather than back-dated to the candle,
// so a strategy can never trade information that was not known at the time.
type OrderBlock struct{}

func (OrderBlock) Spec() primitive.Spec {
	return spec("order_block", primitive.KindZone, (*assayv1.OrderBlockParams)(nil))
}
func (OrderBlock) New(p proto.Message, _ core.TF) (primitive.Instance, error) {
	x, ok := p.(*assayv1.OrderBlockParams)
	if !ok || x.DisplacementWithinBars == 0 || x.MinBodyRatioMilli == 0 || x.MinBodyRatioMilli > 1000 {
		return nil, fmt.Errorf("smc: invalid order block parameters")
	}
	cluster := int(x.ClusterBars)
	if cluster == 0 {
		cluster = 1
	}
	return &orderBlock{within: int(x.DisplacementWithinBars), cluster: cluster, body: core.Price(x.MinBodyRatioMilli), useBody: x.UseBody, unmitigated: x.MustBeUnmitigated}, nil
}

type orderBlock struct {
	within, cluster int
	body            core.Price
	useBody         bool
	unmitigated     bool
	overlays        []primitive.Overlay
}

func (x *orderBlock) Eval(_ *primitive.EvalCtx, s *core.Series) (primitive.Value, error) {
	out, v := make([]primitive.Zone, s.Len()), core.NewBitset(s.Len())
	for i := range s.Time {
		rangeSize := s.High[i] - s.Low[i]
		body := (s.Close[i] - s.Open[i]).Abs()
		if rangeSize <= 0 || body*1000 < rangeSize*x.body {
			continue
		}
		side := core.SideLong
		if s.Close[i] < s.Open[i] {
			side = core.SideShort
		}
		for j := i - 1; j >= 0 && i-j <= x.within; j-- {
			opposite := (side == core.SideLong && s.Close[j] < s.Open[j]) || (side == core.SideShort && s.Close[j] > s.Open[j])
			if !opposite {
				continue
			}
			lo, hi := s.Low[j], s.High[j]
			// A cluster is the contiguous opposing run ending at j. Its complete
			// range is the order-block zone rather than only the final candle.
			start := j
			for k := j - 1; k >= 0 && j-k < x.cluster; k-- {
				if (side == core.SideLong && s.Close[k] < s.Open[k]) || (side == core.SideShort && s.Close[k] > s.Open[k]) {
					start = k
					lo, hi = core.Min(lo, s.Low[k]), core.Max(hi, s.High[k])
				} else {
					break
				}
			}
			if x.useBody {
				lo, hi = core.Min(s.Open[start], s.Close[start]), core.Max(s.Open[start], s.Close[start])
				for k := start + 1; k <= j; k++ {
					lo, hi = core.Min(lo, core.Min(s.Open[k], s.Close[k])), core.Max(hi, core.Max(s.Open[k], s.Close[k]))
				}
			}
			if x.unmitigated && touched(s, start+1, i, lo, hi) {
				continue
			}
			out[i] = primitive.Zone{Low: lo, High: hi, From: s.Time[start], To: s.Time[i], Side: side}
			v.Set(i)
			x.overlays = append(x.overlays, primitive.Overlay{Kind: primitive.OverlayZoneBox, From: s.Time[start], To: s.Time[i], Price: lo, Price2: hi, Text: "OB"})
			break
		}
	}
	return primitive.Value{Kind: primitive.KindZone, Zone: out, Valid: v}, nil
}
func touched(s *core.Series, from, to int, low, high core.Price) bool {
	for i := from; i < to; i++ {
		if s.Low[i] <= high && s.High[i] >= low {
			return true
		}
	}
	return false
}
func (x *orderBlock) Overlays(from, to core.Time) []primitive.Overlay {
	return clip(x.overlays, from, to)
}

// Breaker marks a close (or wick) through the previous opposing candle's
// range. The invalidating-touches parameter is retained in the wire contract
// for consumers that combine the output with their own order-block lifecycle.
type Breaker struct{}

func (Breaker) Spec() primitive.Spec {
	return spec("breaker", primitive.KindBool, (*assayv1.BreakerParams)(nil))
}
func (Breaker) New(p proto.Message, _ core.TF) (primitive.Instance, error) {
	x, ok := p.(*assayv1.BreakerParams)
	if !ok {
		return nil, fmt.Errorf("smc: invalid breaker parameters")
	}
	// Older guided documents did not scaffold this required value. One touch
	// is the least surprising usable default and preserves those strategies.
	if x.InvalidatingTouches == 0 {
		x.InvalidatingTouches = 1
	}
	return &breaker{close: x.FlipOnClose, touches: int(x.InvalidatingTouches)}, nil
}

type breaker struct {
	close    bool
	touches  int
	overlays []primitive.Overlay
}

func (x *breaker) Eval(_ *primitive.EvalCtx, s *core.Series) (primitive.Value, error) {
	b, v := core.NewBitset(s.Len()), core.NewBitset(s.Len())
	for i := 1; i < s.Len(); i++ {
		v.Set(i)
		up, down := s.High[i], s.Low[i]
		if x.close {
			up, down = s.Close[i], s.Close[i]
		}
		for j := i - 1; j >= 0; j-- {
			bearish := s.Close[j] < s.Open[j]
			bullish := s.Close[j] > s.Open[j]
			if !bearish && !bullish {
				continue
			}
			count := 0
			for k := j + 1; k < i; k++ {
				if s.Low[k] <= s.High[j] && s.High[k] >= s.Low[j] {
					count++
				}
			}
			if count < x.touches {
				continue
			}
			if bearish && up > s.High[j] {
				b.Set(i)
				x.overlays = append(x.overlays, primitive.Overlay{Kind: primitive.OverlayMarker, From: s.Time[i], To: s.Time[i], Price: s.High[j], Text: "breaker up"})
				break
			}
			if bullish && down < s.Low[j] {
				b.Set(i)
				x.overlays = append(x.overlays, primitive.Overlay{Kind: primitive.OverlayMarker, From: s.Time[i], To: s.Time[i], Price: s.Low[j], Text: "breaker down"})
				break
			}
		}
	}
	return primitive.Value{Kind: primitive.KindBool, Bool: b, Valid: v}, nil
}
func (x *breaker) Overlays(from, to core.Time) []primitive.Overlay { return clip(x.overlays, from, to) }

type LiquidityPool struct{}

func (LiquidityPool) Spec() primitive.Spec {
	return spec("liquidity_pool", primitive.KindBool, (*assayv1.LiquidityPoolParams)(nil))
}
func (LiquidityPool) New(p proto.Message, _ core.TF) (primitive.Instance, error) {
	x, ok := p.(*assayv1.LiquidityPoolParams)
	if !ok || x.MinTouches < 2 || x.AgeBars == 0 {
		return nil, fmt.Errorf("smc: invalid liquidity pool parameters")
	}
	return &liquidityPool{tolerance: core.Price(x.EqualLevelToleranceTicks), touches: int(x.MinTouches), age: int(x.AgeBars)}, nil
}

type liquidityPool struct {
	tolerance    core.Price
	touches, age int
	overlays     []primitive.Overlay
}

func (x *liquidityPool) Eval(_ *primitive.EvalCtx, s *core.Series) (primitive.Value, error) {
	b, v := core.NewBitset(s.Len()), core.NewBitset(s.Len())
	for i := range s.Time {
		highs, lows := 1, 1
		for j := i - 1; j >= 0 && i-j <= x.age; j-- {
			if (s.High[i] - s.High[j]).Abs() <= x.tolerance {
				highs++
			}
			if (s.Low[i] - s.Low[j]).Abs() <= x.tolerance {
				lows++
			}
		}
		if highs >= x.touches || lows >= x.touches {
			b.Set(i)
			price := s.High[i]
			if lows > highs {
				price = s.Low[i]
			}
			x.overlays = append(x.overlays, primitive.Overlay{Kind: primitive.OverlayLevelLine, From: s.Time[i], To: s.Time[i], Price: price, Text: "liquidity"})
		}
		v.Set(i)
	}
	return primitive.Value{Kind: primitive.KindBool, Bool: b, Valid: v}, nil
}
func (x *liquidityPool) Overlays(from, to core.Time) []primitive.Overlay {
	return clip(x.overlays, from, to)
}

type LiquiditySweep struct{}

func (LiquiditySweep) Spec() primitive.Spec {
	return spec("liquidity_sweep", primitive.KindBool, (*assayv1.SweepParams)(nil))
}
func (LiquiditySweep) New(p proto.Message, _ core.TF) (primitive.Instance, error) {
	return newLiquiditySweep(p, 0)
}

// LiquiditySweepBullish marks sweeps below a prior low that subsequently close
// back above that low (and any configured structure-shift confirmation).
type LiquiditySweepBullish struct{}

func (LiquiditySweepBullish) Spec() primitive.Spec {
	return spec("liquidity_sweep_bullish", primitive.KindBool, (*assayv1.SweepParams)(nil))
}
func (LiquiditySweepBullish) New(p proto.Message, _ core.TF) (primitive.Instance, error) {
	return newLiquiditySweep(p, core.SideLong)
}

// LiquiditySweepBearish marks sweeps above a prior high that subsequently
// close back below that high (and any configured structure-shift confirmation).
type LiquiditySweepBearish struct{}

func (LiquiditySweepBearish) Spec() primitive.Spec {
	return spec("liquidity_sweep_bearish", primitive.KindBool, (*assayv1.SweepParams)(nil))
}
func (LiquiditySweepBearish) New(p proto.Message, _ core.TF) (primitive.Instance, error) {
	return newLiquiditySweep(p, core.SideShort)
}

func newLiquiditySweep(p proto.Message, side core.Side) (primitive.Instance, error) {
	x, ok := p.(*assayv1.SweepParams)
	if !ok || x.CloseBackBars == 0 {
		return nil, fmt.Errorf("smc: invalid sweep parameters")
	}
	return &liquiditySweep{depth: core.Price(x.WickThroughTicks), closeBars: int(x.CloseBackBars), requireShift: x.RequireStructureShift, side: side}, nil
}

type liquiditySweep struct {
	depth        core.Price
	closeBars    int
	requireShift bool
	side         core.Side
	overlays     []primitive.Overlay
}

type pendingSweep struct {
	side         core.Side
	level, shift core.Price
	expires      int
}

func (x *liquiditySweep) Eval(_ *primitive.EvalCtx, s *core.Series) (primitive.Value, error) {
	b, v := core.NewBitset(s.Len()), core.NewBitset(s.Len())
	pending := make([]pendingSweep, 0, x.closeBars)
	for i := 1; i < s.Len(); i++ {
		v.Set(i)
		hi, lo := s.High[i-1], s.Low[i-1]
		for j := 0; j < len(pending); {
			p := pending[j]
			inside := (p.side == core.SideShort && s.Close[i] < p.level) || (p.side == core.SideLong && s.Close[i] > p.level)
			shifted := !x.requireShift || (p.side == core.SideShort && s.Close[i] < p.shift) || (p.side == core.SideLong && s.Close[i] > p.shift)
			if inside && shifted {
				if x.matches(p.side) {
					b.Set(i)
				}
				x.overlays = append(x.overlays, primitive.Overlay{Kind: primitive.OverlayMarker, From: s.Time[i], To: s.Time[i], Price: p.level, Text: "liquidity sweep"})
				pending = append(pending[:j], pending[j+1:]...)
				continue
			}
			if i >= p.expires {
				pending = append(pending[:j], pending[j+1:]...)
				continue
			}
			j++
		}
		highSweep := s.High[i] >= hi+x.depth
		lowSweep := s.Low[i] <= lo-x.depth
		// A same-bar close-back is fully known at this close and needs no future
		// confirmation; process it immediately without backdating the signal.
		if highSweep && s.Close[i] < hi && !x.requireShift {
			if x.matches(core.SideShort) {
				b.Set(i)
			}
			x.overlays = append(x.overlays, primitive.Overlay{Kind: primitive.OverlayMarker, From: s.Time[i], To: s.Time[i], Price: hi, Text: "liquidity sweep"})
		} else if highSweep {
			pending = append(pending, pendingSweep{side: core.SideShort, level: hi, shift: lo, expires: i + x.closeBars})
		}
		if lowSweep && s.Close[i] > lo && !x.requireShift {
			if x.matches(core.SideLong) {
				b.Set(i)
			}
			x.overlays = append(x.overlays, primitive.Overlay{Kind: primitive.OverlayMarker, From: s.Time[i], To: s.Time[i], Price: lo, Text: "liquidity sweep"})
		} else if lowSweep {
			pending = append(pending, pendingSweep{side: core.SideLong, level: lo, shift: hi, expires: i + x.closeBars})
		}
	}
	return primitive.Value{Kind: primitive.KindBool, Bool: b, Valid: v}, nil
}
func (x *liquiditySweep) matches(side core.Side) bool { return x.side == 0 || x.side == side }
func (x *liquiditySweep) Overlays(from, to core.Time) []primitive.Overlay {
	return clip(x.overlays, from, to)
}

type PremiumDiscount struct{}

func (PremiumDiscount) Spec() primitive.Spec {
	return spec("premium_discount", primitive.KindZone, (*assayv1.PremiumDiscountParams)(nil))
}
func (PremiumDiscount) New(p proto.Message, _ core.TF) (primitive.Instance, error) {
	x, ok := p.(*assayv1.PremiumDiscountParams)
	if !ok || x.DealingRangeBars == 0 || x.OteLowerMilli > x.OteUpperMilli || x.OteUpperMilli > 1000 {
		return nil, fmt.Errorf("smc: invalid premium/discount parameters")
	}
	return &premiumDiscount{n: int(x.DealingRangeBars), lower: x.OteLowerMilli, upper: x.OteUpperMilli}, nil
}

type premiumDiscount struct {
	n            int
	lower, upper uint32
	overlays     []primitive.Overlay
}

func (x *premiumDiscount) Eval(_ *primitive.EvalCtx, s *core.Series) (primitive.Value, error) {
	out := make([]primitive.Zone, s.Len())
	v := core.NewBitset(s.Len())
	for i := x.n - 1; i < s.Len(); i++ {
		hi, lo := s.High[i], s.Low[i]
		for j := i - x.n + 1; j <= i; j++ {
			hi, lo = core.Max(hi, s.High[j]), core.Min(lo, s.Low[j])
		}
		width := hi - lo
		out[i] = primitive.Zone{Low: lo + width*core.Price(x.lower)/1000, High: lo + width*core.Price(x.upper)/1000, From: s.Time[i], To: s.Time[i]}
		v.Set(i)
		// Consecutive equivalent zones add no information and make overlays grow
		// with every bar, so retain only range changes.
		if i == x.n-1 || out[i].Low != out[i-1].Low || out[i].High != out[i-1].High {
			x.overlays = append(x.overlays, primitive.Overlay{Kind: primitive.OverlayZoneBox, From: s.Time[i], To: s.Time[i], Price: out[i].Low, Price2: out[i].High, Text: "OTE"})
		}
	}
	return primitive.Value{Kind: primitive.KindZone, Zone: out, Valid: v}, nil
}
func (x *premiumDiscount) Overlays(from, to core.Time) []primitive.Overlay {
	return clip(x.overlays, from, to)
}

func clip(in []primitive.Overlay, from, to core.Time) []primitive.Overlay {
	out := make([]primitive.Overlay, 0, len(in))
	for _, x := range in {
		if x.To >= from && x.From <= to {
			out = append(out, x)
		}
	}
	return out
}
