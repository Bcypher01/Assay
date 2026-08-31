package priceaction

import (
	"fmt"
	assayv1 "github.com/bcypher01/assay/gen/go/assay/v1"
	"github.com/bcypher01/assay/internal/core"
	"github.com/bcypher01/assay/internal/primitive"
	"google.golang.org/protobuf/proto"
)

func init() {
	primitive.MustRegister(SwingHigh{})
	primitive.MustRegister(SwingLow{})
	primitive.MustRegister(Engulfing{})
	primitive.MustRegister(EngulfingBullish{})
	primitive.MustRegister(EngulfingBearish{})
	primitive.MustRegister(InsideBar{})
	primitive.MustRegister(PinBar{})
	primitive.MustRegister(Doji{})
	primitive.MustRegister(Consolidation{})
	primitive.MustRegister(MeasuredMove{})
	primitive.MustRegister(SupportCluster{})
	primitive.MustRegister(ResistanceCluster{})
}

type SwingHigh struct{}

func (SwingHigh) Spec() primitive.Spec { return swingSpec("swing_high") }
func (SwingHigh) New(p proto.Message, _ core.TF) (primitive.Instance, error) {
	n, e := swing(p)
	return swingInst{n, true}, e
}

type SwingLow struct{}

func (SwingLow) Spec() primitive.Spec { return swingSpec("swing_low") }
func (SwingLow) New(p proto.Message, _ core.TF) (primitive.Instance, error) {
	n, e := swing(p)
	return swingInst{n, false}, e
}
func swingSpec(t string) primitive.Spec {
	return primitive.Spec{Pack: "priceaction", Type: t, Out: primitive.KindBool, Params: (*assayv1.SwingParams)(nil).ProtoReflect().Descriptor(), Timeframed: true}
}

type swingInst struct {
	n    int
	high bool
}

func (x swingInst) Eval(_ *primitive.EvalCtx, s *core.Series) (primitive.Value, error) {
	b := core.NewBitset(s.Len())
	v := core.NewBitset(s.Len())
	for i := x.n; i+x.n < s.Len(); i++ {
		ok := true
		z := s.High[i]
		if !x.high {
			z = s.Low[i]
		}
		for j := i - x.n; j <= i+x.n; j++ {
			if j == i {
				continue
			}
			q := s.High[j]
			if !x.high {
				q = s.Low[j]
			}
			if (x.high && q >= z) || (!x.high && q <= z) {
				ok = false
				break
			}
		}
		if ok {
			b.Set(i)
		}
		v.Set(i)
	}
	return primitive.Value{Kind: primitive.KindBool, Bool: b, Valid: v}, nil
}
func (swingInst) Overlays(core.Time, core.Time) []primitive.Overlay { return nil }
func swing(p proto.Message) (int, error) {
	x, ok := p.(*assayv1.SwingParams)
	if !ok || x.Left == 0 || x.Right == 0 || x.Left != x.Right {
		return 0, fmt.Errorf("priceaction: left and right must be equal and positive")
	}
	return int(x.Left), nil
}

type Engulfing struct{}
type EngulfingBullish struct{}
type EngulfingBearish struct{}

func (Engulfing) Spec() primitive.Spec {
	return candleSpec("engulfing")
}
func (EngulfingBullish) Spec() primitive.Spec {
	return candleSpec("engulfing_bullish")
}
func (EngulfingBearish) Spec() primitive.Spec {
	return candleSpec("engulfing_bearish")
}
func (Engulfing) New(proto.Message, core.TF) (primitive.Instance, error) {
	return engulf{}, nil
}
func (EngulfingBullish) New(proto.Message, core.TF) (primitive.Instance, error) {
	return engulf{bullish: true}, nil
}
func (EngulfingBearish) New(proto.Message, core.TF) (primitive.Instance, error) {
	return engulf{bearish: true}, nil
}

type engulf struct{ bullish, bearish bool }

func (x engulf) Eval(_ *primitive.EvalCtx, s *core.Series) (primitive.Value, error) {
	b := core.NewBitset(s.Len())
	v := core.NewBitset(s.Len())
	for i := 1; i < s.Len(); i++ {
		v.Set(i)
		bull := s.Close[i] > s.Open[i] && s.Close[i-1] < s.Open[i-1] && s.Open[i] <= s.Close[i-1] && s.Close[i] >= s.Open[i-1]
		bear := s.Close[i] < s.Open[i] && s.Close[i-1] > s.Open[i-1] && s.Open[i] >= s.Close[i-1] && s.Close[i] <= s.Open[i-1]
		if (!x.bullish && !x.bearish && (bull || bear)) || (x.bullish && bull) || (x.bearish && bear) {
			b.Set(i)
		}
	}
	return primitive.Value{Kind: primitive.KindBool, Bool: b, Valid: v}, nil
}
func (engulf) Overlays(core.Time, core.Time) []primitive.Overlay { return nil }

type InsideBar struct{}

func (InsideBar) Spec() primitive.Spec                                   { return candleSpec("inside_bar") }
func (InsideBar) New(proto.Message, core.TF) (primitive.Instance, error) { return inside{}, nil }

type inside struct{}

func (inside) Eval(_ *primitive.EvalCtx, s *core.Series) (primitive.Value, error) {
	b := core.NewBitset(s.Len())
	v := core.NewBitset(s.Len())
	for i := 1; i < s.Len(); i++ {
		v.Set(i)
		if s.High[i] <= s.High[i-1] && s.Low[i] >= s.Low[i-1] {
			b.Set(i)
		}
	}
	return primitive.Value{Kind: primitive.KindBool, Bool: b, Valid: v}, nil
}
func (inside) Overlays(core.Time, core.Time) []primitive.Overlay { return nil }
func candleSpec(t string) primitive.Spec {
	return primitive.Spec{Pack: "priceaction", Type: t, Out: primitive.KindBool, Params: (*assayv1.SampleParams)(nil).ProtoReflect().Descriptor(), Timeframed: true}
}

// PinBar identifies a candle whose body is no more than body_ratio_milli of
// its range and whose longer wick is at least wick_ratio_milli of that range.
type PinBar struct{}

func (PinBar) Spec() primitive.Spec {
	return primitive.Spec{Pack: "priceaction", Type: "pin_bar", Out: primitive.KindBool, Params: (*assayv1.PinBarParams)(nil).ProtoReflect().Descriptor(), Timeframed: true}
}
func (PinBar) New(p proto.Message, _ core.TF) (primitive.Instance, error) {
	x, ok := p.(*assayv1.PinBarParams)
	if !ok || x.BodyRatioMilli == 0 || x.WickRatioMilli == 0 {
		return nil, fmt.Errorf("priceaction: invalid pin bar parameters")
	}
	return pinBar{x.BodyRatioMilli, x.WickRatioMilli}, nil
}

type pinBar struct{ body, wick uint32 }

func (x pinBar) Eval(_ *primitive.EvalCtx, s *core.Series) (primitive.Value, error) {
	b, v := core.NewBitset(s.Len()), core.NewBitset(s.Len())
	for i := range s.Time {
		r := s.High[i] - s.Low[i]
		body := (s.Close[i] - s.Open[i]).Abs()
		upper := s.High[i] - core.Max(s.Open[i], s.Close[i])
		lower := core.Min(s.Open[i], s.Close[i]) - s.Low[i]
		if r > 0 && body*1000 <= r*core.Price(x.body) && core.Max(upper, lower)*1000 >= r*core.Price(x.wick) {
			b.Set(i)
		}
		v.Set(i)
	}
	return primitive.Value{Kind: primitive.KindBool, Bool: b, Valid: v}, nil
}
func (pinBar) Overlays(core.Time, core.Time) []primitive.Overlay { return nil }

type Doji struct{}

func (Doji) Spec() primitive.Spec {
	return primitive.Spec{Pack: "priceaction", Type: "doji", Out: primitive.KindBool, Params: (*assayv1.DojiParams)(nil).ProtoReflect().Descriptor(), Timeframed: true}
}
func (Doji) New(p proto.Message, _ core.TF) (primitive.Instance, error) {
	x, ok := p.(*assayv1.DojiParams)
	if !ok || x.BodyRatioMilli == 0 {
		return nil, fmt.Errorf("priceaction: invalid doji parameters")
	}
	return doji{x.BodyRatioMilli}, nil
}

type doji struct{ body uint32 }

func (x doji) Eval(_ *primitive.EvalCtx, s *core.Series) (primitive.Value, error) {
	b, v := core.NewBitset(s.Len()), core.NewBitset(s.Len())
	for i := range s.Time {
		r := s.High[i] - s.Low[i]
		if r > 0 && (s.Close[i]-s.Open[i]).Abs()*1000 <= r*core.Price(x.body) {
			b.Set(i)
		}
		v.Set(i)
	}
	return primitive.Value{Kind: primitive.KindBool, Bool: b, Valid: v}, nil
}
func (doji) Overlays(core.Time, core.Time) []primitive.Overlay { return nil }

// Consolidation marks each completed period whose high-low width fits the configured range.
type Consolidation struct{}

func (Consolidation) Spec() primitive.Spec {
	return primitive.Spec{Pack: "priceaction", Type: "consolidation", Out: primitive.KindBool, Params: (*assayv1.RangeParams)(nil).ProtoReflect().Descriptor(), Timeframed: true}
}
func (Consolidation) New(p proto.Message, _ core.TF) (primitive.Instance, error) {
	x, ok := p.(*assayv1.RangeParams)
	if !ok || x.Period == 0 {
		return nil, fmt.Errorf("priceaction: period must be positive")
	}
	return consolidation{int(x.Period), core.Price(x.MaxWidthTicks)}, nil
}

type consolidation struct {
	n     int
	width core.Price
}

func (x consolidation) Eval(_ *primitive.EvalCtx, s *core.Series) (primitive.Value, error) {
	b, v := core.NewBitset(s.Len()), core.NewBitset(s.Len())
	for i := x.n - 1; i < s.Len(); i++ {
		hi, lo := s.High[i], s.Low[i]
		for j := i - x.n + 1; j <= i; j++ {
			hi = core.Max(hi, s.High[j])
			lo = core.Min(lo, s.Low[j])
		}
		if hi-lo <= x.width {
			b.Set(i)
		}
		v.Set(i)
	}
	return primitive.Value{Kind: primitive.KindBool, Bool: b, Valid: v}, nil
}
func (consolidation) Overlays(core.Time, core.Time) []primitive.Overlay { return nil }

// MeasuredMove projects the current close by the preceding period's range.
type MeasuredMove struct{}

func (MeasuredMove) Spec() primitive.Spec {
	return primitive.Spec{Pack: "priceaction", Type: "measured_move", Out: primitive.KindLevel, Params: (*assayv1.MeasuredMoveParams)(nil).ProtoReflect().Descriptor(), Timeframed: true}
}
func (MeasuredMove) New(p proto.Message, _ core.TF) (primitive.Instance, error) {
	x, ok := p.(*assayv1.MeasuredMoveParams)
	if !ok || x.Period == 0 {
		return nil, fmt.Errorf("priceaction: period must be positive")
	}
	return measured{int(x.Period)}, nil
}

type measured struct{ n int }

func (x measured) Eval(_ *primitive.EvalCtx, s *core.Series) (primitive.Value, error) {
	o := make([]core.Price, s.Len())
	v := core.NewBitset(s.Len())
	for i := x.n - 1; i < s.Len(); i++ {
		hi, lo := s.High[i], s.Low[i]
		for j := i - x.n + 1; j <= i; j++ {
			hi = core.Max(hi, s.High[j])
			lo = core.Min(lo, s.Low[j])
		}
		if s.Close[i] >= s.Open[i] {
			o[i] = s.Close[i] + hi - lo
		} else {
			o[i] = s.Close[i] - (hi - lo)
		}
		v.Set(i)
	}
	return primitive.Value{Kind: primitive.KindLevel, Level: o, Valid: v}, nil
}
func (measured) Overlays(core.Time, core.Time) []primitive.Overlay { return nil }

// SupportCluster and ResistanceCluster emit the inclusive low/high zone of a
// rolling price cluster. Their tolerance makes the definition independent of
// a particular instrument's tick size.
type SupportCluster struct{}
type ResistanceCluster struct{}

func (SupportCluster) Spec() primitive.Spec    { return clusterSpec("support_cluster") }
func (ResistanceCluster) Spec() primitive.Spec { return clusterSpec("resistance_cluster") }
func (SupportCluster) New(p proto.Message, _ core.TF) (primitive.Instance, error) {
	n, tolerance, err := clusterParams(p)
	return cluster{n: n, tolerance: tolerance, resistance: false}, err
}
func (ResistanceCluster) New(p proto.Message, _ core.TF) (primitive.Instance, error) {
	n, tolerance, err := clusterParams(p)
	return cluster{n: n, tolerance: tolerance, resistance: true}, err
}
func clusterSpec(t string) primitive.Spec {
	return primitive.Spec{Pack: "priceaction", Type: t, Out: primitive.KindZone, Params: (*assayv1.ClusterParams)(nil).ProtoReflect().Descriptor(), Timeframed: true}
}
func clusterParams(p proto.Message) (int, core.Price, error) {
	x, ok := p.(*assayv1.ClusterParams)
	if !ok || x.Lookback == 0 {
		return 0, 0, fmt.Errorf("priceaction: cluster lookback must be positive")
	}
	return int(x.Lookback), core.Price(x.ToleranceTicks), nil
}

type cluster struct {
	n          int
	tolerance  core.Price
	resistance bool
}

func (x cluster) Eval(_ *primitive.EvalCtx, s *core.Series) (primitive.Value, error) {
	o, v := make([]primitive.Zone, s.Len()), core.NewBitset(s.Len())
	for i := x.n - 1; i < s.Len(); i++ {
		price := s.Low[i]
		if x.resistance {
			price = s.High[i]
		}
		for j := i - x.n + 1; j <= i; j++ {
			candidate := s.Low[j]
			if x.resistance {
				candidate = s.High[j]
			}
			if x.resistance {
				price = core.Max(price, candidate)
			} else {
				price = core.Min(price, candidate)
			}
		}
		o[i] = primitive.Zone{Low: price - x.tolerance, High: price + x.tolerance}
		v.Set(i)
	}
	return primitive.Value{Kind: primitive.KindZone, Zone: o, Valid: v}, nil
}
func (cluster) Overlays(core.Time, core.Time) []primitive.Overlay { return nil }
