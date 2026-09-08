// Package semantic contains detectors produced by the chart-first Setup Test
// authoring surface. A drawing supplies an example and parameters; these
// primitives detect the repeatable market structure without retaining the
// example's absolute price or timestamp.
package semantic

import (
	"fmt"
	"math"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"

	"github.com/bcypher01/assay/internal/core"
	"github.com/bcypher01/assay/internal/primitive"
)

func init() {
	for _, p := range []primitive.Primitive{
		BreakRetest{side: core.SideLong}, BreakRetest{side: core.SideShort},
		OrderBlockRetest{side: core.SideLong}, OrderBlockRetest{side: core.SideShort},
		BreakerRetest{side: core.SideLong}, BreakerRetest{side: core.SideShort},
		FairValueGapRetest{side: core.SideLong}, FairValueGapRetest{side: core.SideShort},
		DrawnPrecedent{side: core.SideLong}, DrawnPrecedent{side: core.SideShort},
		POIZone{side: core.SideLong}, POIZone{side: core.SideShort},
		ShiftCandle{side: core.SideLong}, ShiftCandle{side: core.SideShort},
	} {
		primitive.MustRegister(p)
	}
}

type parameters struct {
	lookback, retest, tolerance, expiry, minSize int
	displacementBody                             int
	useBody, confirmByClose                      bool
	mitigationRule                               string
	precedent                                    []normalizedBar
	similarity                                   int
}

type normalizedBar struct{ open, high, low, close int }

func readParameters(message proto.Message) (parameters, error) {
	x, ok := message.(*structpb.Struct)
	if !ok {
		return parameters{}, fmt.Errorf("semantic: parameters must be a protobuf Struct")
	}
	readInt := func(name string, fallback int) (int, error) {
		field, ok := x.Fields[name]
		if !ok {
			return fallback, nil
		}
		n := field.GetNumberValue()
		if n < 0 || n != math.Trunc(n) || n > math.MaxInt32 {
			return 0, fmt.Errorf("semantic: %s must be a non-negative integer", name)
		}
		return int(n), nil
	}
	var out parameters
	var err error
	if out.lookback, err = readInt("lookbackBars", 20); err != nil {
		return out, err
	}
	if out.retest, err = readInt("retestBars", 10); err != nil {
		return out, err
	}
	if out.tolerance, err = readInt("toleranceTicks", 5); err != nil {
		return out, err
	}
	if out.expiry, err = readInt("expiryBars", 50); err != nil {
		return out, err
	}
	if out.displacementBody, err = readInt("displacementBodyMilli", 600); err != nil {
		return out, err
	}
	if out.minSize, err = readInt("minSizeTicks", 1); err != nil {
		return out, err
	}
	if out.similarity, err = readInt("similarityMilli", 800); err != nil {
		return out, err
	}
	if field, ok := x.Fields["precedent"]; ok {
		for _, value := range field.GetListValue().Values {
			item := value.GetStructValue()
			if item == nil {
				return out, fmt.Errorf("semantic: precedent candles must be objects")
			}
			read := func(name string) (int, error) {
				value, ok := item.Fields[name]
				if !ok || value.GetNumberValue() != math.Trunc(value.GetNumberValue()) {
					return 0, fmt.Errorf("semantic: precedent %s must be an integer", name)
				}
				return int(value.GetNumberValue()), nil
			}
			var bar normalizedBar
			if bar.open, err = read("openMilli"); err != nil {
				return out, err
			}
			if bar.high, err = read("highMilli"); err != nil {
				return out, err
			}
			if bar.low, err = read("lowMilli"); err != nil {
				return out, err
			}
			if bar.close, err = read("closeMilli"); err != nil {
				return out, err
			}
			out.precedent = append(out.precedent, bar)
		}
	}
	if field, ok := x.Fields["mitigationRule"]; ok {
		out.mitigationRule = field.GetStringValue()
	}
	if out.mitigationRule == "" {
		out.mitigationRule = "first_touch"
	}
	out.useBody = boolField(x, "useBody", false)
	out.confirmByClose = boolField(x, "confirmByClose", true)
	if out.lookback < 2 || out.retest < 1 || out.expiry < 1 || out.displacementBody < 1 || out.displacementBody > 1000 {
		return out, fmt.Errorf("semantic: invalid setup detector parameters")
	}
	return out, nil
}

// DrawnPrecedent repeats the normalized candle shape selected directly on the
// chart. It deliberately has no named trading concept: the user's example is
// the strategy precedent. Absolute price and timestamp never enter matching.
type DrawnPrecedent struct{ side core.Side }

func (x DrawnPrecedent) Spec() primitive.Spec {
	if x.side == core.SideShort {
		return semanticSpec("drawn_precedent_bearish")
	}
	return semanticSpec("drawn_precedent_bullish")
}

func (x DrawnPrecedent) New(p proto.Message, _ core.TF) (primitive.Instance, error) {
	params, err := readParameters(p)
	if err != nil {
		return nil, err
	}
	if len(params.precedent) < 3 || len(params.precedent) > 64 {
		return nil, fmt.Errorf("semantic: precedent must contain 3 to 64 candles")
	}
	if params.similarity < 500 || params.similarity > 1000 {
		return nil, fmt.Errorf("semantic: similarityMilli must be between 500 and 1000")
	}
	return &drawnPrecedent{parameters: params, side: x.side}, nil
}

type drawnPrecedent struct {
	parameters
	side     core.Side
	overlays []primitive.Overlay
}

func (x *drawnPrecedent) Eval(_ *primitive.EvalCtx, s *core.Series) (primitive.Value, error) {
	b, valid := core.NewBitset(s.Len()), core.NewBitset(s.Len())
	n := len(x.precedent)
	expected := make([]PrecedentCandle, n)
	for i, bar := range x.precedent {
		expected[i] = PrecedentCandle{Open: bar.open, High: bar.high, Low: bar.low, Close: bar.close}
	}
	for i := n - 1; i < s.Len(); i++ {
		valid.Set(i)
		from := i - n + 1
		low, high := s.Low[from], s.High[from]
		for j := from + 1; j <= i; j++ {
			low = core.Min(low, s.Low[j])
			high = core.Max(high, s.High[j])
		}
		score := ScoreSeriesPrecedent(expected, s, i, x.similarity)
		if !score.Accepted {
			continue
		}
		b.Set(i)
		x.overlays = append(x.overlays,
			primitive.Overlay{Kind: primitive.OverlayZoneBox, From: s.Time[from], To: s.Time[i], Price: low, Price2: high, Text: "drawn precedent"},
			primitive.Overlay{Kind: primitive.OverlayMarker, From: s.Time[i], To: s.Time[i], Price: s.Close[i], Text: "precedent match"},
		)
	}
	return primitive.Value{Kind: primitive.KindBool, Bool: b, Valid: valid}, nil
}

func (x *drawnPrecedent) Overlays(from, to core.Time) []primitive.Overlay {
	return clip(x.overlays, from, to)
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

// FairValueGapRetest is the executable meaning of a user-drawn FVG zone. The
// drawing supplies a confirmed example; this detector repeats the candle
// relationship and fill rule without retaining that example's price or time.
type FairValueGapRetest struct{ side core.Side }

func (x FairValueGapRetest) Spec() primitive.Spec {
	if x.side == core.SideShort {
		return semanticSpec("fair_value_gap_retest_bearish")
	}
	return semanticSpec("fair_value_gap_retest_bullish")
}

func (x FairValueGapRetest) New(p proto.Message, _ core.TF) (primitive.Instance, error) {
	params, err := readParameters(p)
	if err != nil {
		return nil, err
	}
	if params.minSize < 1 {
		return nil, fmt.Errorf("semantic: minSizeTicks must be positive")
	}
	switch params.mitigationRule {
	case "first_touch", "half_fill", "full_fill":
	default:
		return nil, fmt.Errorf("semantic: invalid FVG mitigation rule")
	}
	return &fairValueGapRetest{parameters: params, side: x.side}, nil
}

type fairValueGapRetest struct {
	parameters
	side     core.Side
	overlays []primitive.Overlay
}

func (x *fairValueGapRetest) Eval(_ *primitive.EvalCtx, s *core.Series) (primitive.Value, error) {
	b, valid := core.NewBitset(s.Len()), core.NewBitset(s.Len())
	var pending *pendingZone
	for i := range s.Time {
		valid.Set(i)
		if pending != nil {
			if i > pending.expires {
				pending = nil
			} else if i > pending.created && x.filled(s, i, *pending) {
				b.Set(i)
				x.overlays = append(x.overlays,
					primitive.Overlay{Kind: primitive.OverlayZoneBox, From: s.Time[pending.created], To: s.Time[i], Price: pending.low, Price2: pending.high, Text: "FVG"},
					primitive.Overlay{Kind: primitive.OverlayMarker, From: s.Time[i], To: s.Time[i], Price: midpoint(pending.low, pending.high), Text: "FVG retest"},
				)
				pending = nil
			}
		}
		if i < 2 {
			continue
		}
		var low, high core.Price
		if x.side == core.SideLong {
			low, high = s.High[i-2], s.Low[i]
		} else {
			low, high = s.High[i], s.Low[i-2]
		}
		if high-low < core.Price(x.minSize) {
			continue
		}
		pending = &pendingZone{low: low, high: high, created: i, expires: min(i+x.expiry, s.Len()-1)}
		x.overlays = append(x.overlays, primitive.Overlay{Kind: primitive.OverlayZoneBox, From: s.Time[i], To: s.Time[pending.expires], Price: low, Price2: high, Text: "FVG"})
	}
	return primitive.Value{Kind: primitive.KindBool, Bool: b, Valid: valid}, nil
}

func (x *fairValueGapRetest) filled(s *core.Series, i int, z pendingZone) bool {
	mid := midpoint(z.low, z.high)
	switch x.mitigationRule {
	case "first_touch":
		return s.Low[i] <= z.high && s.High[i] >= z.low
	case "half_fill":
		if x.side == core.SideLong {
			return s.Low[i] <= mid
		}
		return s.High[i] >= mid
	case "full_fill":
		if x.side == core.SideLong {
			return s.Low[i] <= z.low
		}
		return s.High[i] >= z.high
	default:
		return false
	}
}

func (x *fairValueGapRetest) Overlays(from, to core.Time) []primitive.Overlay {
	return clip(x.overlays, from, to)
}

func boolField(x *structpb.Struct, name string, fallback bool) bool {
	field, ok := x.Fields[name]
	if !ok {
		return fallback
	}
	return field.GetBoolValue()
}

func semanticSpec(name string) primitive.Spec {
	return primitive.Spec{Pack: "semantic", Type: name, Out: primitive.KindBool, Params: (&structpb.Struct{}).ProtoReflect().Descriptor(), Timeframed: true}
}

// BreakRetest identifies a close or wick through the preceding rolling
// extreme, followed on a later bar by a touch and directional close around the
// broken level. The rolling window excludes the breaking bar.
type BreakRetest struct{ side core.Side }

func (x BreakRetest) Spec() primitive.Spec {
	if x.side == core.SideShort {
		return semanticSpec("break_retest_bearish")
	}
	return semanticSpec("break_retest_bullish")
}
func (x BreakRetest) New(p proto.Message, _ core.TF) (primitive.Instance, error) {
	params, err := readParameters(p)
	return &breakRetest{parameters: params, side: x.side}, err
}

type pendingLevel struct {
	price   core.Price
	created int
	expires int
}

type breakRetest struct {
	parameters
	side     core.Side
	overlays []primitive.Overlay
}

func (x *breakRetest) Eval(_ *primitive.EvalCtx, s *core.Series) (primitive.Value, error) {
	b, valid := core.NewBitset(s.Len()), core.NewBitset(s.Len())
	var pending *pendingLevel
	for i := x.lookback; i < s.Len(); i++ {
		valid.Set(i)
		if pending != nil {
			if i > pending.expires {
				pending = nil
			} else if i > pending.created && x.retested(s, i, pending.price) {
				b.Set(i)
				x.overlays = append(x.overlays, primitive.Overlay{Kind: primitive.OverlayMarker, From: s.Time[i], To: s.Time[i], Price: pending.price, Text: "retest"})
				pending = nil
			}
		}
		level := precedingExtreme(s, i, x.lookback, x.side)
		probe := s.Close[i]
		if !x.confirmByClose {
			if x.side == core.SideLong {
				probe = s.High[i]
			} else {
				probe = s.Low[i]
			}
		}
		broken := (x.side == core.SideLong && probe > level) || (x.side == core.SideShort && probe < level)
		if broken {
			pending = &pendingLevel{price: level, created: i, expires: i + x.retest}
			x.overlays = append(x.overlays, primitive.Overlay{Kind: primitive.OverlayLevelLine, From: s.Time[i], To: s.Time[min(i+x.retest, s.Len()-1)], Price: level, Text: "broken level"})
		}
	}
	return primitive.Value{Kind: primitive.KindBool, Bool: b, Valid: valid}, nil
}

func (x *breakRetest) retested(s *core.Series, i int, level core.Price) bool {
	tolerance := core.Price(x.tolerance)
	if x.side == core.SideLong {
		return s.Low[i] <= level+tolerance && s.Close[i] > level
	}
	return s.High[i] >= level-tolerance && s.Close[i] < level
}
func (x *breakRetest) Overlays(from, to core.Time) []primitive.Overlay {
	return clip(x.overlays, from, to)
}

func precedingExtreme(s *core.Series, at, lookback int, side core.Side) core.Price {
	value := s.High[at-lookback]
	if side == core.SideShort {
		value = s.Low[at-lookback]
	}
	for i := at - lookback + 1; i < at; i++ {
		if side == core.SideLong {
			value = core.Max(value, s.High[i])
		} else {
			value = core.Min(value, s.Low[i])
		}
	}
	return value
}

// OrderBlockRetest identifies the last opposing candle before a directional
// displacement and signals the first later retest that closes back in the
// displacement direction. Only one newest zone is active per direction.
type OrderBlockRetest struct{ side core.Side }

func (x OrderBlockRetest) Spec() primitive.Spec {
	if x.side == core.SideShort {
		return semanticSpec("order_block_retest_bearish")
	}
	return semanticSpec("order_block_retest_bullish")
}
func (x OrderBlockRetest) New(p proto.Message, _ core.TF) (primitive.Instance, error) {
	params, err := readParameters(p)
	return &orderBlockRetest{parameters: params, side: x.side}, err
}

type pendingZone struct {
	low, high core.Price
	created   int
	expires   int
}

type orderBlockRetest struct {
	parameters
	side     core.Side
	overlays []primitive.Overlay
}

func (x *orderBlockRetest) Eval(_ *primitive.EvalCtx, s *core.Series) (primitive.Value, error) {
	b, valid := core.NewBitset(s.Len()), core.NewBitset(s.Len())
	var pending *pendingZone
	for i := 1; i < s.Len(); i++ {
		valid.Set(i)
		if pending != nil {
			if i > pending.expires {
				pending = nil
			} else if i > pending.created && x.zoneRetested(s, i, *pending) {
				b.Set(i)
				x.overlays = append(x.overlays, primitive.Overlay{Kind: primitive.OverlayMarker, From: s.Time[i], To: s.Time[i], Price: midpoint(pending.low, pending.high), Text: "order block retest"})
				pending = nil
			}
		}
		if !directionalDisplacement(s, i, x.side, x.displacementBody) {
			continue
		}
		from := max(0, i-x.lookback)
		for j := i - 1; j >= from; j-- {
			if !opposingCandle(s, j, x.side) {
				continue
			}
			low, high := s.Low[j], s.High[j]
			if x.useBody {
				low, high = core.Min(s.Open[j], s.Close[j]), core.Max(s.Open[j], s.Close[j])
			}
			pending = &pendingZone{low: low, high: high, created: i, expires: i + x.expiry}
			x.overlays = append(x.overlays, primitive.Overlay{Kind: primitive.OverlayZoneBox, From: s.Time[j], To: s.Time[min(i+x.expiry, s.Len()-1)], Price: low, Price2: high, Text: "order block"})
			break
		}
	}
	return primitive.Value{Kind: primitive.KindBool, Bool: b, Valid: valid}, nil
}
func (x *orderBlockRetest) zoneRetested(s *core.Series, i int, z pendingZone) bool {
	tolerance := core.Price(x.tolerance)
	touches := s.Low[i] <= z.high+tolerance && s.High[i] >= z.low-tolerance
	if x.side == core.SideLong {
		return touches && s.Close[i] > s.Open[i]
	}
	return touches && s.Close[i] < s.Open[i]
}
func (x *orderBlockRetest) Overlays(from, to core.Time) []primitive.Overlay {
	return clip(x.overlays, from, to)
}

// POIZone is the state half of the top-down entry ladder. It marks every bar
// whose range touches the active point-of-interest zone — the last opposing
// candle before a directional displacement, exactly the zone OrderBlockRetest
// signals from — but unlike that detector the zone is not consumed by the
// touch. Read on a coarse timeframe and projected onto a fine entry series,
// it answers "is price back inside my higher-timeframe zone right now", which
// the entry-half trigger (ShiftCandle) combines with a fine-timeframe shift.
type POIZone struct{ side core.Side }

func (x POIZone) Spec() primitive.Spec {
	if x.side == core.SideShort {
		return semanticSpec("poi_zone_bearish")
	}
	return semanticSpec("poi_zone_bullish")
}
func (x POIZone) New(p proto.Message, _ core.TF) (primitive.Instance, error) {
	params, err := readParameters(p)
	return &poiZone{parameters: params, side: x.side}, err
}

type poiZone struct {
	parameters
	side     core.Side
	overlays []primitive.Overlay
}

func (x *poiZone) Eval(_ *primitive.EvalCtx, s *core.Series) (primitive.Value, error) {
	b, valid := core.NewBitset(s.Len()), core.NewBitset(s.Len())
	var pending *pendingZone
	for i := 1; i < s.Len(); i++ {
		valid.Set(i)
		if pending != nil {
			if i > pending.expires {
				pending = nil
			} else if zoneTouched(s, i, *pending, core.Price(x.tolerance)) {
				b.Set(i)
			}
		}
		if !directionalDisplacement(s, i, x.side, x.displacementBody) {
			continue
		}
		from := max(0, i-x.lookback)
		for j := i - 1; j >= from; j-- {
			if !opposingCandle(s, j, x.side) {
				continue
			}
			low, high := s.Low[j], s.High[j]
			if x.useBody {
				low, high = core.Min(s.Open[j], s.Close[j]), core.Max(s.Open[j], s.Close[j])
			}
			pending = &pendingZone{low: low, high: high, created: i, expires: i + x.expiry}
			x.overlays = append(x.overlays, primitive.Overlay{Kind: primitive.OverlayZoneBox, From: s.Time[j], To: s.Time[min(i+x.expiry, s.Len()-1)], Price: low, Price2: high, Text: "POI zone"})
			break
		}
	}
	return primitive.Value{Kind: primitive.KindBool, Bool: b, Valid: valid}, nil
}

// zoneTouched reports whether bar i's high-to-low range reaches into the zone,
// with the configured tolerance widening it. It is a state probe, not an
// event: the same zone can report touched on many consecutive bars.
func zoneTouched(s *core.Series, i int, z pendingZone, tolerance core.Price) bool {
	return s.Low[i] <= z.high+tolerance && s.High[i] >= z.low-tolerance
}

func (x *poiZone) Overlays(from, to core.Time) []primitive.Overlay {
	return clip(x.overlays, from, to)
}

// ShiftCandle is the entry half of the top-down ladder: a single candle on the
// entry timeframe whose body is at least the configured share of its range and
// which closes in the trade direction — the displacement "shift" a trader
// waits for after price arrives at a higher-timeframe point of interest.
type ShiftCandle struct{ side core.Side }

func (x ShiftCandle) Spec() primitive.Spec {
	if x.side == core.SideShort {
		return semanticSpec("shift_candle_bearish")
	}
	return semanticSpec("shift_candle_bullish")
}
func (x ShiftCandle) New(p proto.Message, _ core.TF) (primitive.Instance, error) {
	params, err := readParameters(p)
	return &shiftCandle{parameters: params, side: x.side}, err
}

type shiftCandle struct {
	parameters
	side     core.Side
	overlays []primitive.Overlay
}

func (x *shiftCandle) Eval(_ *primitive.EvalCtx, s *core.Series) (primitive.Value, error) {
	b, valid := core.NewBitset(s.Len()), core.NewBitset(s.Len())
	for i := range s.Time {
		valid.Set(i)
		if !directionalDisplacement(s, i, x.side, x.displacementBody) {
			continue
		}
		b.Set(i)
		x.overlays = append(x.overlays, primitive.Overlay{Kind: primitive.OverlayMarker, From: s.Time[i], To: s.Time[i], Price: s.Close[i], Text: "shift"})
	}
	return primitive.Value{Kind: primitive.KindBool, Bool: b, Valid: valid}, nil
}
func (x *shiftCandle) Overlays(from, to core.Time) []primitive.Overlay {
	return clip(x.overlays, from, to)
}

// BreakerRetest treats an opposing candle zone as flipped after price closes
// through its far edge, then signals the first later retest from the new side.
type BreakerRetest struct{ side core.Side }

func (x BreakerRetest) Spec() primitive.Spec {
	if x.side == core.SideShort {
		return semanticSpec("breaker_retest_bearish")
	}
	return semanticSpec("breaker_retest_bullish")
}
func (x BreakerRetest) New(p proto.Message, _ core.TF) (primitive.Instance, error) {
	params, err := readParameters(p)
	return &breakerRetest{parameters: params, side: x.side}, err
}

type breakerRetest struct {
	parameters
	side     core.Side
	overlays []primitive.Overlay
}

func (x *breakerRetest) Eval(_ *primitive.EvalCtx, s *core.Series) (primitive.Value, error) {
	b, valid := core.NewBitset(s.Len()), core.NewBitset(s.Len())
	var pending *pendingZone
	for i := x.lookback; i < s.Len(); i++ {
		valid.Set(i)
		if pending != nil {
			if i > pending.expires {
				pending = nil
			} else if i > pending.created && breakerRetested(s, i, *pending, x.side, core.Price(x.tolerance)) {
				b.Set(i)
				x.overlays = append(x.overlays, primitive.Overlay{Kind: primitive.OverlayMarker, From: s.Time[i], To: s.Time[i], Price: midpoint(pending.low, pending.high), Text: "breaker retest"})
				pending = nil
			}
		}
		from := max(0, i-x.lookback)
		for j := i - 1; j >= from; j-- {
			if !opposingCandle(s, j, x.side) {
				continue
			}
			low, high := s.Low[j], s.High[j]
			if x.useBody {
				low, high = core.Min(s.Open[j], s.Close[j]), core.Max(s.Open[j], s.Close[j])
			}
			invalidated := (x.side == core.SideLong && s.Close[i] > high) || (x.side == core.SideShort && s.Close[i] < low)
			if invalidated {
				pending = &pendingZone{low: low, high: high, created: i, expires: i + x.expiry}
				x.overlays = append(x.overlays, primitive.Overlay{Kind: primitive.OverlayZoneBox, From: s.Time[j], To: s.Time[min(i+x.expiry, s.Len()-1)], Price: low, Price2: high, Text: "breaker"})
				break
			}
		}
	}
	return primitive.Value{Kind: primitive.KindBool, Bool: b, Valid: valid}, nil
}
func (x *breakerRetest) Overlays(from, to core.Time) []primitive.Overlay {
	return clip(x.overlays, from, to)
}

func breakerRetested(s *core.Series, i int, z pendingZone, side core.Side, tolerance core.Price) bool {
	if side == core.SideLong {
		return s.Low[i] <= z.high+tolerance && s.Close[i] > z.high
	}
	return s.High[i] >= z.low-tolerance && s.Close[i] < z.low
}

func directionalDisplacement(s *core.Series, i int, side core.Side, bodyMilli int) bool {
	rangeSize := s.High[i] - s.Low[i]
	if rangeSize <= 0 || (s.Close[i]-s.Open[i]).Abs()*1000 < rangeSize*core.Price(bodyMilli) {
		return false
	}
	return (side == core.SideLong && s.Close[i] > s.Open[i]) || (side == core.SideShort && s.Close[i] < s.Open[i])
}
func opposingCandle(s *core.Series, i int, side core.Side) bool {
	return (side == core.SideLong && s.Close[i] < s.Open[i]) || (side == core.SideShort && s.Close[i] > s.Open[i])
}
func midpoint(low, high core.Price) core.Price { return low + (high-low)/2 }
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
func clip(in []primitive.Overlay, from, to core.Time) []primitive.Overlay {
	out := make([]primitive.Overlay, 0, len(in))
	for _, overlay := range in {
		if overlay.To >= from && overlay.From <= to {
			out = append(out, overlay)
		}
	}
	return out
}
