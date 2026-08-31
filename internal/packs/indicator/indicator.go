// Package indicator provides deterministic, fixed-point technical indicators.
package indicator

import (
	"fmt"
	"math"

	assayv1 "github.com/bcypher01/assay/gen/go/assay/v1"
	"github.com/bcypher01/assay/internal/core"
	"github.com/bcypher01/assay/internal/primitive"
	"google.golang.org/protobuf/proto"
)

func init() {
	for _, p := range []primitive.Primitive{SMA{}, EMA{}, WMA{}, Slope{}, MATouch{}, MACross{}, MACrossBullish{}, MACrossBearish{}, DonchianHigh{}, DonchianLow{}, RSI{}, MACD{}, BollingerBands{}, BollingerUpper{}, BollingerLower{}, ATR{}, ADX{}, Stochastic{}, Volume{}, OBV{}} {
		primitive.MustRegister(p)
	}
}

type SMA struct{}
type EMA struct{}
type WMA struct{}

func (SMA) Spec() primitive.Spec {
	return spec("sma", primitive.KindLevel, (*assayv1.MovingAverageParams)(nil))
}
func (EMA) Spec() primitive.Spec {
	return spec("ema", primitive.KindLevel, (*assayv1.MovingAverageParams)(nil))
}
func (WMA) Spec() primitive.Spec {
	return spec("wma", primitive.KindLevel, (*assayv1.MovingAverageParams)(nil))
}
func (SMA) New(p proto.Message, _ core.TF) (primitive.Instance, error) {
	n, e := maPeriod(p)
	return moving{n, 0}, e
}
func (EMA) New(p proto.Message, _ core.TF) (primitive.Instance, error) {
	n, e := maPeriod(p)
	return moving{n, 1}, e
}
func (WMA) New(p proto.Message, _ core.TF) (primitive.Instance, error) {
	n, e := maPeriod(p)
	return moving{n, 2}, e
}

type moving struct{ period, mode int }

func (m moving) Eval(_ *primitive.EvalCtx, s *core.Series) (primitive.Value, error) {
	o := make([]core.Price, s.Len())
	v := core.NewBitset(s.Len())
	if m.period > s.Len() {
		return level(o, v), nil
	}
	var sum core.Price
	for i := 0; i < m.period; i++ {
		sum += s.Close[i]
	}
	set := func(i int) {
		switch m.mode {
		case 0:
			o[i] = sum / core.Price(m.period)
		case 1:
			if i == m.period-1 {
				o[i] = sum / core.Price(m.period)
			} else {
				o[i] = (2*s.Close[i] + core.Price(m.period-1)*o[i-1]) / core.Price(m.period+1)
			}
		case 2:
			var w core.Price
			for j := 0; j < m.period; j++ {
				w += s.Close[i-m.period+1+j] * core.Price(j+1)
			}
			o[i] = w / core.Price(m.period*(m.period+1)/2)
		}
		v.Set(i)
	}
	set(m.period - 1)
	for i := m.period; i < s.Len(); i++ {
		sum += s.Close[i] - s.Close[i-m.period]
		set(i)
	}
	return level(o, v), nil
}
func (moving) Overlays(core.Time, core.Time) []primitive.Overlay { return nil }

type Slope struct{}

func (Slope) Spec() primitive.Spec {
	return spec("slope", primitive.KindNumber, (*assayv1.SlopeParams)(nil))
}
func (Slope) New(p proto.Message, _ core.TF) (primitive.Instance, error) {
	n, e := periodOf(p)
	return slope{n}, e
}

type slope struct{ n int }

func (x slope) Eval(_ *primitive.EvalCtx, s *core.Series) (primitive.Value, error) {
	o := make([]core.Ratio, s.Len())
	v := core.NewBitset(s.Len())
	for i := x.n; i < s.Len(); i++ {
		o[i] = core.DivPrice(s.Close[i]-s.Close[i-x.n], core.Price(x.n))
		v.Set(i)
	}
	return primitive.Value{Kind: primitive.KindNumber, Number: o, Valid: v}, nil
}
func (slope) Overlays(core.Time, core.Time) []primitive.Overlay { return nil }

type MATouch struct{}

func (MATouch) Spec() primitive.Spec {
	return spec("ma_touch", primitive.KindBool, (*assayv1.MovingAverageTouchParams)(nil))
}
func (MATouch) New(p proto.Message, _ core.TF) (primitive.Instance, error) {
	x, ok := p.(*assayv1.MovingAverageTouchParams)
	if !ok || x.Period == 0 {
		return nil, fmt.Errorf("indicator: period must be positive")
	}
	return touch{int(x.Period), core.Price(x.ToleranceTicks)}, nil
}

type touch struct {
	n int
	t core.Price
}

func (x touch) Eval(_ *primitive.EvalCtx, s *core.Series) (primitive.Value, error) {
	ma, _ := moving{x.n, 0}.Eval(nil, s)
	b := core.NewBitset(s.Len())
	for i := x.n - 1; i < s.Len(); i++ {
		if s.Low[i] <= ma.Level[i]+x.t && s.High[i] >= ma.Level[i]-x.t {
			b.Set(i)
		}
	}
	return primitive.Value{Kind: primitive.KindBool, Bool: b, Valid: ma.Valid}, nil
}
func (touch) Overlays(core.Time, core.Time) []primitive.Overlay { return nil }

type MACross struct{}
type MACrossBullish struct{}
type MACrossBearish struct{}

func (MACross) Spec() primitive.Spec {
	return spec("ma_cross", primitive.KindBool, (*assayv1.MovingAverageCrossParams)(nil))
}
func (MACrossBullish) Spec() primitive.Spec {
	return spec("ma_cross_bullish", primitive.KindBool, (*assayv1.MovingAverageCrossParams)(nil))
}
func (MACrossBearish) Spec() primitive.Spec {
	return spec("ma_cross_bearish", primitive.KindBool, (*assayv1.MovingAverageCrossParams)(nil))
}
func (MACross) New(p proto.Message, _ core.TF) (primitive.Instance, error) {
	return newCross(p, crossEither)
}
func (MACrossBullish) New(p proto.Message, _ core.TF) (primitive.Instance, error) {
	return newCross(p, crossBullish)
}
func (MACrossBearish) New(p proto.Message, _ core.TF) (primitive.Instance, error) {
	return newCross(p, crossBearish)
}

type crossDirection uint8

const (
	crossEither crossDirection = iota
	crossBullish
	crossBearish
)

func newCross(p proto.Message, direction crossDirection) (primitive.Instance, error) {
	x, ok := p.(*assayv1.MovingAverageCrossParams)
	if !ok || x.FastPeriod == 0 || x.SlowPeriod == 0 {
		return nil, fmt.Errorf("indicator: MA periods must be positive")
	}
	return cross{f: int(x.FastPeriod), l: int(x.SlowPeriod), direction: direction}, nil
}

type cross struct {
	f, l      int
	direction crossDirection
}

func (x cross) Eval(_ *primitive.EvalCtx, s *core.Series) (primitive.Value, error) {
	f, _ := moving{x.f, 0}.Eval(nil, s)
	l, _ := moving{x.l, 0}.Eval(nil, s)
	b, v := core.NewBitset(s.Len()), core.NewBitset(s.Len())
	start := x.f
	if x.l > start {
		start = x.l
	}
	for i := start; i < s.Len(); i++ {
		v.Set(i)
		bullish := f.Level[i-1] <= l.Level[i-1] && f.Level[i] > l.Level[i]
		bearish := f.Level[i-1] >= l.Level[i-1] && f.Level[i] < l.Level[i]
		if (x.direction == crossEither && (bullish || bearish)) ||
			(x.direction == crossBullish && bullish) ||
			(x.direction == crossBearish && bearish) {
			b.Set(i)
		}
	}
	return primitive.Value{Kind: primitive.KindBool, Bool: b, Valid: v}, nil
}
func (cross) Overlays(core.Time, core.Time) []primitive.Overlay { return nil }

// DonchianHigh and DonchianLow expose the preceding N-bar channel bounds.
// The current bar is intentionally excluded so a close crossing the boundary
// can be detected without comparing a bar to a level it just changed.
type DonchianHigh struct{}
type DonchianLow struct{}

func (DonchianHigh) Spec() primitive.Spec {
	return spec("donchian_high", primitive.KindLevel, (*assayv1.PeriodParams)(nil))
}
func (DonchianLow) Spec() primitive.Spec {
	return spec("donchian_low", primitive.KindLevel, (*assayv1.PeriodParams)(nil))
}
func (DonchianHigh) New(p proto.Message, _ core.TF) (primitive.Instance, error) {
	n, err := periodOf(p)
	return donchian{period: n, high: true}, err
}
func (DonchianLow) New(p proto.Message, _ core.TF) (primitive.Instance, error) {
	n, err := periodOf(p)
	return donchian{period: n}, err
}

type donchian struct {
	period int
	high   bool
}

func (x donchian) Eval(_ *primitive.EvalCtx, s *core.Series) (primitive.Value, error) {
	levels := make([]core.Price, s.Len())
	valid := core.NewBitset(s.Len())
	if x.period >= s.Len() {
		return level(levels, valid), nil
	}
	// The queue stores candidate indexes in descending (high) or ascending
	// (low) value order. Its fixed period-sized ring avoids per-bar allocation
	// and reduces the rolling bound calculation from O(bars*period) to O(bars).
	queue := make([]int, x.period)
	head, tail, count := 0, 0, 0
	value := func(i int) core.Price {
		if x.high {
			return s.High[i]
		}
		return s.Low[i]
	}
	for i := 0; i < s.Len(); i++ {
		if i >= x.period {
			levels[i] = value(queue[head])
			valid.Set(i)
			for count > 0 && queue[head] <= i-x.period {
				head = (head + 1) % x.period
				count--
			}
		}
		for count > 0 {
			last := (tail - 1 + x.period) % x.period
			if (x.high && value(queue[last]) >= value(i)) || (!x.high && value(queue[last]) <= value(i)) {
				break
			}
			tail = last
			count--
		}
		queue[tail] = i
		tail = (tail + 1) % x.period
		count++
	}
	return level(levels, valid), nil
}
func (donchian) Overlays(core.Time, core.Time) []primitive.Overlay { return nil }

type RSI struct{}

func (RSI) Spec() primitive.Spec { return spec("rsi", primitive.KindNumber, (*assayv1.RsiParams)(nil)) }
func (RSI) New(p proto.Message, _ core.TF) (primitive.Instance, error) {
	n, e := rsiPeriod(p)
	return rsi{n}, e
}

type rsi struct{ n int }

func (x rsi) Eval(_ *primitive.EvalCtx, s *core.Series) (primitive.Value, error) {
	o := make([]core.Ratio, s.Len())
	v := core.NewBitset(s.Len())
	if x.n >= s.Len() {
		return primitive.Value{Kind: primitive.KindNumber, Number: o, Valid: v}, nil
	}
	var g, l core.Price
	for i := 1; i <= x.n; i++ {
		d := s.Close[i] - s.Close[i-1]
		if d > 0 {
			g += d
		} else {
			l -= d
		}
	}
	setRSI(o, v, x.n, g, l)
	for i := x.n + 1; i < s.Len(); i++ {
		d := s.Close[i] - s.Close[i-1]
		a, b := core.Price(0), core.Price(0)
		if d > 0 {
			a = d
		} else {
			b = -d
		}
		g = (g*core.Price(x.n-1) + a) / core.Price(x.n)
		l = (l*core.Price(x.n-1) + b) / core.Price(x.n)
		setRSI(o, v, i, g, l)
	}
	return primitive.Value{Kind: primitive.KindNumber, Number: o, Valid: v}, nil
}
func setRSI(o []core.Ratio, v *core.Bitset, i int, g, l core.Price) {
	if l == 0 {
		o[i] = core.RatioFromInt(100)
	} else {
		o[i] = core.DivPrice(g, g+l).Mul(core.RatioFromInt(100))
	}
	v.Set(i)
}
func (rsi) Overlays(core.Time, core.Time) []primitive.Overlay { return nil }

type MACD struct{}

func (MACD) Spec() primitive.Spec {
	return spec("macd", primitive.KindLevel, (*assayv1.MacdParams)(nil))
}
func (MACD) New(p proto.Message, _ core.TF) (primitive.Instance, error) {
	x, ok := p.(*assayv1.MacdParams)
	if !ok || x.FastPeriod == 0 || x.SlowPeriod == 0 || x.SignalPeriod == 0 {
		return nil, fmt.Errorf("indicator: MACD periods must be positive")
	}
	return macd{int(x.FastPeriod), int(x.SlowPeriod)}, nil
}

type macd struct{ f, l int }

func (x macd) Eval(_ *primitive.EvalCtx, s *core.Series) (primitive.Value, error) {
	f, _ := moving{x.f, 1}.Eval(nil, s)
	l, _ := moving{x.l, 1}.Eval(nil, s)
	o := make([]core.Price, s.Len())
	v := core.NewBitset(s.Len())
	start := x.f
	if x.l > start {
		start = x.l
	}
	for i := start - 1; i < s.Len(); i++ {
		o[i] = f.Level[i] - l.Level[i]
		v.Set(i)
	}
	return level(o, v), nil
}
func (macd) Overlays(core.Time, core.Time) []primitive.Overlay { return nil }

type BollingerBands struct{}
type BollingerUpper struct{}
type BollingerLower struct{}

func (BollingerBands) Spec() primitive.Spec {
	return spec("bollinger_bands", primitive.KindZone, (*assayv1.BollingerBandsParams)(nil))
}
func (BollingerUpper) Spec() primitive.Spec {
	return spec("bollinger_upper", primitive.KindLevel, (*assayv1.BollingerBandsParams)(nil))
}
func (BollingerLower) Spec() primitive.Spec {
	return spec("bollinger_lower", primitive.KindLevel, (*assayv1.BollingerBandsParams)(nil))
}
func (BollingerBands) New(p proto.Message, _ core.TF) (primitive.Instance, error) {
	b, err := newBands(p)
	return b, err
}
func (BollingerUpper) New(p proto.Message, _ core.TF) (primitive.Instance, error) {
	b, err := newBands(p)
	if err != nil {
		return nil, err
	}
	return bandEdge{bands: b, upper: true}, nil
}
func (BollingerLower) New(p proto.Message, _ core.TF) (primitive.Instance, error) {
	b, err := newBands(p)
	if err != nil {
		return nil, err
	}
	return bandEdge{bands: b}, nil
}

func newBands(p proto.Message) (bands, error) {
	x, ok := p.(*assayv1.BollingerBandsParams)
	if !ok || x.Period == 0 || x.WidthMilli == 0 {
		return bands{}, fmt.Errorf("indicator: invalid Bollinger parameters")
	}
	return bands{int(x.Period), int64(x.WidthMilli)}, nil
}

type bands struct {
	n int
	w int64
}

func (x bands) Eval(_ *primitive.EvalCtx, s *core.Series) (primitive.Value, error) {
	o := make([]primitive.Zone, s.Len())
	v := core.NewBitset(s.Len())
	for i := x.n - 1; i < s.Len(); i++ {
		var sum, ss core.Price
		for j := i - x.n + 1; j <= i; j++ {
			sum += s.Close[j]
		}
		m := sum / core.Price(x.n)
		for j := i - x.n + 1; j <= i; j++ {
			d := s.Close[j] - m
			ss += d * d
		}
		d := core.Price(math.Sqrt(float64(ss / core.Price(x.n))))
		w := d * core.Price(x.w) / 1000
		o[i] = primitive.Zone{Low: m - w, High: m + w}
		v.Set(i)
	}
	return primitive.Value{Kind: primitive.KindZone, Zone: o, Valid: v}, nil
}
func (bands) Overlays(core.Time, core.Time) []primitive.Overlay { return nil }

type bandEdge struct {
	bands bands
	upper bool
}

func (x bandEdge) Eval(ctx *primitive.EvalCtx, s *core.Series) (primitive.Value, error) {
	zones, err := x.bands.Eval(ctx, s)
	if err != nil {
		return primitive.Value{}, err
	}
	levels := make([]core.Price, s.Len())
	for i := range levels {
		levels[i] = zones.Zone[i].Low
		if x.upper {
			levels[i] = zones.Zone[i].High
		}
	}
	return level(levels, zones.Valid), nil
}
func (bandEdge) Overlays(core.Time, core.Time) []primitive.Overlay { return nil }

type ATR struct{}

func (ATR) Spec() primitive.Spec { return spec("atr", primitive.KindLevel, (*assayv1.AtrParams)(nil)) }
func (ATR) New(p proto.Message, _ core.TF) (primitive.Instance, error) {
	n, e := atrPeriod(p)
	return atr{n}, e
}

type atr struct{ n int }

func (x atr) Eval(_ *primitive.EvalCtx, s *core.Series) (primitive.Value, error) {
	o := make([]core.Price, s.Len())
	v := core.NewBitset(s.Len())
	if x.n >= s.Len() {
		return level(o, v), nil
	}
	for i := 1; i <= x.n; i++ {
		o[x.n] += tr(s, i)
	}
	o[x.n] /= core.Price(x.n)
	v.Set(x.n)
	for i := x.n + 1; i < s.Len(); i++ {
		o[i] = (o[i-1]*core.Price(x.n-1) + tr(s, i)) / core.Price(x.n)
		v.Set(i)
	}
	return level(o, v), nil
}
func tr(s *core.Series, i int) core.Price {
	return core.Max(s.High[i]-s.Low[i], core.Max((s.High[i]-s.Close[i-1]).Abs(), (s.Low[i]-s.Close[i-1]).Abs()))
}
func (atr) Overlays(core.Time, core.Time) []primitive.Overlay { return nil }

type ADX struct{}

func (ADX) Spec() primitive.Spec { return spec("adx", primitive.KindNumber, (*assayv1.AdxParams)(nil)) }
func (ADX) New(p proto.Message, _ core.TF) (primitive.Instance, error) {
	n, e := periodOf(p)
	return directional{n}, e
}

type directional struct{ n int }

func (x directional) Eval(_ *primitive.EvalCtx, s *core.Series) (primitive.Value, error) {
	o := make([]core.Ratio, s.Len())
	v := core.NewBitset(s.Len())
	for i := 2 * x.n; i < s.Len(); i++ {
		var up, down core.Price
		for j := i - x.n + 1; j <= i; j++ {
			a := s.High[j] - s.High[j-1]
			b := s.Low[j-1] - s.Low[j]
			if a > b && a > 0 {
				up += a
			}
			if b > a && b > 0 {
				down += b
			}
		}
		if up+down > 0 {
			o[i] = core.DivPrice((up - down).Abs(), up+down).Mul(core.RatioFromInt(100))
		}
		v.Set(i)
	}
	return primitive.Value{Kind: primitive.KindNumber, Number: o, Valid: v}, nil
}
func (directional) Overlays(core.Time, core.Time) []primitive.Overlay { return nil }

type Stochastic struct{}

func (Stochastic) Spec() primitive.Spec {
	return spec("stochastic", primitive.KindNumber, (*assayv1.StochasticParams)(nil))
}
func (Stochastic) New(p proto.Message, _ core.TF) (primitive.Instance, error) {
	n, e := periodOf(p)
	return stochastic{n}, e
}

type stochastic struct{ n int }

func (x stochastic) Eval(_ *primitive.EvalCtx, s *core.Series) (primitive.Value, error) {
	o := make([]core.Ratio, s.Len())
	v := core.NewBitset(s.Len())
	for i := x.n - 1; i < s.Len(); i++ {
		h, l := s.High[i], s.Low[i]
		for j := i - x.n + 1; j <= i; j++ {
			h = core.Max(h, s.High[j])
			l = core.Min(l, s.Low[j])
		}
		if h != l {
			o[i] = core.DivPrice(s.Close[i]-l, h-l).Mul(core.RatioFromInt(100))
		}
		v.Set(i)
	}
	return primitive.Value{Kind: primitive.KindNumber, Number: o, Valid: v}, nil
}
func (stochastic) Overlays(core.Time, core.Time) []primitive.Overlay { return nil }

type Volume struct{}

func (Volume) Spec() primitive.Spec {
	return spec("volume", primitive.KindLevel, (*assayv1.SampleParams)(nil))
}
func (Volume) New(proto.Message, core.TF) (primitive.Instance, error) { return volume{}, nil }

type volume struct{}

func (volume) Eval(_ *primitive.EvalCtx, s *core.Series) (primitive.Value, error) {
	o := make([]core.Price, s.Len())
	v := core.NewBitset(s.Len())
	for i, n := range s.Volume {
		o[i] = core.Price(n)
		v.Set(i)
	}
	return level(o, v), nil
}
func (volume) Overlays(core.Time, core.Time) []primitive.Overlay { return nil }

type OBV struct{}

func (OBV) Spec() primitive.Spec {
	return spec("obv", primitive.KindLevel, (*assayv1.SampleParams)(nil))
}
func (OBV) New(proto.Message, core.TF) (primitive.Instance, error) { return obv{}, nil }

type obv struct{}

func (obv) Eval(_ *primitive.EvalCtx, s *core.Series) (primitive.Value, error) {
	o := make([]core.Price, s.Len())
	v := core.NewBitset(s.Len())
	if s.Len() > 0 {
		v.Set(0)
	}
	for i := 1; i < s.Len(); i++ {
		o[i] = o[i-1]
		if s.Close[i] > s.Close[i-1] {
			o[i] += core.Price(s.Volume[i])
		} else if s.Close[i] < s.Close[i-1] {
			o[i] -= core.Price(s.Volume[i])
		}
		v.Set(i)
	}
	return level(o, v), nil
}
func (obv) Overlays(core.Time, core.Time) []primitive.Overlay { return nil }
func spec(t string, k primitive.Kind, p proto.Message) primitive.Spec {
	return primitive.Spec{Pack: "indicator", Type: t, Out: k, Params: p.ProtoReflect().Descriptor(), Timeframed: true}
}
func level(o []core.Price, v *core.Bitset) primitive.Value {
	return primitive.Value{Kind: primitive.KindLevel, Level: o, Valid: v}
}
func maPeriod(p proto.Message) (int, error) {
	x, ok := p.(*assayv1.MovingAverageParams)
	if !ok || x.Period == 0 {
		return 0, fmt.Errorf("indicator: period must be positive")
	}
	return int(x.Period), nil
}
func rsiPeriod(p proto.Message) (int, error) {
	x, ok := p.(*assayv1.RsiParams)
	if !ok || x.Period == 0 {
		return 0, fmt.Errorf("indicator: period must be positive")
	}
	return int(x.Period), nil
}
func atrPeriod(p proto.Message) (int, error) {
	x, ok := p.(*assayv1.AtrParams)
	if !ok || x.Period == 0 {
		return 0, fmt.Errorf("indicator: period must be positive")
	}
	return int(x.Period), nil
}
func periodOf(p proto.Message) (int, error) {
	switch x := p.(type) {
	case *assayv1.SlopeParams:
		if x.Period > 0 {
			return int(x.Period), nil
		}
	case *assayv1.AdxParams:
		if x.Period > 0 {
			return int(x.Period), nil
		}
	case *assayv1.StochasticParams:
		if x.Period > 0 {
			return int(x.Period), nil
		}
	case *assayv1.PeriodParams:
		if x.Period > 0 {
			return int(x.Period), nil
		}
	}
	return 0, fmt.Errorf("indicator: period must be positive")
}
