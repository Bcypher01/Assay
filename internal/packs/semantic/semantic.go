// Package semantic contains detectors produced by the chart-first Setup Test
// authoring surface. A drawing supplies an example and parameters; these
// primitives detect the repeatable market structure without retaining the
// example's absolute price or timestamp.
package semantic

import (
	"fmt"
	"math"

	"github.com/Bcypher01/Assay/internal/core"
	"github.com/Bcypher01/Assay/internal/primitive"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

func init() {
	for _, p := range []primitive.Primitive{
		BreakRetest{side: core.SideLong}, BreakRetest{side: core.SideShort},
		OrderBlockRetest{side: core.SideLong}, OrderBlockRetest{side: core.SideShort},
		BreakerRetest{side: core.SideLong}, BreakerRetest{side: core.SideShort},
	} {
		primitive.MustRegister(p)
	}
}

type parameters struct {
	lookback, retest, tolerance, expiry int
	displacementBody                    int
	useBody, confirmByClose             bool
}

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
	out.useBody = boolField(x, "useBody", false)
	out.confirmByClose = boolField(x, "confirmByClose", true)
	if out.lookback < 2 || out.retest < 1 || out.expiry < 1 || out.displacementBody < 1 || out.displacementBody > 1000 {
		return out, fmt.Errorf("semantic: invalid setup detector parameters")
	}
	return out, nil
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
