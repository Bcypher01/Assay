package engine

import (
	"context"
	"encoding/json"
	"fmt"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"

	assayv1 "github.com/bcypher01/assay/gen/go/assay/v1"
	"github.com/bcypher01/assay/internal/core"
	"github.com/bcypher01/assay/internal/ir"
	_ "github.com/bcypher01/assay/internal/packs/indicator"
	_ "github.com/bcypher01/assay/internal/packs/priceaction"
	_ "github.com/bcypher01/assay/internal/packs/semantic"
	_ "github.com/bcypher01/assay/internal/packs/smc"
	_ "github.com/bcypher01/assay/internal/packs/timeofday"
	"github.com/bcypher01/assay/internal/primitive"
	"github.com/bcypher01/assay/internal/sim"
)

type Data struct {
	Series, Fine *core.Series
	Config       sim.Config
	Snapshot     string
	Cache        primitive.Cache
	Registry     *primitive.Registry
	// Stale bounds how long a higher-timeframe result stays usable once its
	// bar has closed. The zero value holds a coarse value for its own period
	// plus one entry bar, so a gap reports invalid rather than a stale bias.
	Stale primitive.StalePolicy
}
type NearMiss struct {
	Bars         []Miss
	SlotFailures map[string]int
}
type Miss struct {
	Bar    int
	Held   []string
	Failed []string
}

// Run evaluates an IR document and passes its sparse entry plan to the
// simulator. Primitives remain self-contained: adding one only registers it.
func Run(ctx context.Context, strategy *ir.Strategy, data Data) (sim.RunResult, NearMiss, error) {
	var result sim.RunResult
	miss := NearMiss{SlotFailures: make(map[string]int)}
	if strategy == nil || data.Series == nil {
		return result, miss, fmt.Errorf("engine: strategy and series are required")
	}
	reg := data.Registry
	if reg == nil {
		reg = primitive.DefaultRegistry
	}
	if errs := strategy.Validate(reg); len(errs) > 0 {
		return result, miss, errs[0]
	}
	activeNodes := referencedNodesForStrategy(strategy)
	values := make(map[string]primitive.Value, len(strategy.Nodes))
	for _, n := range strategy.Nodes {
		// A saved strategy can contain exploratory building blocks that are not
		// wired into a condition or exit. They are not part of the executable
		// strategy and must not be evaluated (or be able to fail a run).
		if !activeNodes[n.ID] {
			continue
		}
		p, e := reg.Get(n.Pack, n.Type)
		if e != nil {
			return result, miss, e
		}
		params, e := decodeParams(p, n.Params)
		if e != nil {
			return result, miss, fmt.Errorf("engine: node %s: %w", n.ID, e)
		}
		tf := n.Timeframe
		// Node timeframes were optional in the first strategy documents. The
		// omitted enum decodes to TFTick, but a tick primitive cannot run over a
		// candle series. Treat that legacy zero value as the selected entry
		// timeframe; explicitly-authored candle timeframes remain unchanged.
		if !tf.Valid() || (tf == core.TFTick && data.Series.TF != core.TFTick) {
			tf = data.Series.TF
		}
		// A node coarser than the entry series is detected on its own completed
		// bars and then projected back down onto the entry index space, so that
		// a closed higher-timeframe result is visible to the entry bar that
		// closes with it. A node finer than the entry series cannot be
		// aggregated from it at all and is refused here rather than degraded.
		v, _, e := primitive.EvaluateAligned(ctx, p, params, tf, data.Series, data.Snapshot, data.Cache, data.Stale)
		if e != nil {
			return result, miss, fmt.Errorf("engine: node %s: %w", n.ID, e)
		}
		if v.Len() != data.Series.Len() {
			return result, miss, fmt.Errorf("engine: node %s timeframe output cannot be aligned to entry series", n.ID)
		}
		values[n.ID] = v
	}
	ex, err := resolvePlan(ctx, strategy, data, values, reg)
	if err != nil {
		return result, miss, err
	}
	sig := sim.Signals{Enter: core.NewBitset(data.Series.Len())}
	useRelativeExits := strategy.Slots.Invalidation == "" && strategy.Slots.Target == ""
	poiAt := -1
	poiWindow := resolveWindow(data.Series, strategy.Slots.Trigger.Within, strategy.Slots.Trigger.WithinBars)
	windowed := strategy.Slots.Trigger.Within > 0 || strategy.Slots.Trigger.WithinBars > 0
	for bar := 0; bar < data.Series.Len(); bar++ {
		if strategy.Schema == ir.SchemaV2 {
			runV2Bar(strategy, data.Series, values, &sig, &miss, bar, useRelativeExits, ex)
			continue
		}
		bias := holds(strategy.Slots.Bias, values, bar)
		poi := holds(strategy.Slots.POI, values, bar)
		trigger := holds(strategy.Slots.Trigger, values, bar)
		if poi {
			poiAt = bar
		}
		poiOK := poi
		triggerOK := trigger
		if windowed {
			poiOK = poiAt >= 0 && poiWindow.contains(data.Series, bar, poiAt)
			triggerOK = trigger && poiOK
		}
		if bias && poiOK && triggerOK {
			switch {
			case !ex.ready(bar):
				// The rule fired but the distance or level it needs is not
				// readable yet, and an entry sized against nothing would vanish
				// without comment.
				miss.SlotFailures["atr_warmup"]++
				miss.Bars = append(miss.Bars, Miss{Bar: bar, Failed: []string{"atr_warmup"}})
			case useRelativeExits:
				addRelativeSides(&sig, strategy.Direction, bar, strategy.Risk.Stop, strategy.Risk.Target, ex)
			default:
				addSides(&sig, strategy.Direction, bar, values[strategy.Slots.Invalidation].Level[bar], values[strategy.Slots.Target].Level[bar], ex)
			}
		} else {
			m := Miss{Bar: bar}
			if !bias {
				m.Failed = append(m.Failed, "bias")
				miss.SlotFailures["bias"]++
			} else {
				m.Held = append(m.Held, "bias")
			}
			if !poiOK {
				m.Failed = append(m.Failed, "poi")
				miss.SlotFailures["poi"]++
			} else {
				m.Held = append(m.Held, "poi")
			}
			if !triggerOK {
				m.Failed = append(m.Failed, "trigger")
				miss.SlotFailures["trigger"]++
			} else {
				m.Held = append(m.Held, "trigger")
			}
			miss.Bars = append(miss.Bars, m)
		}
	}
	if err := sim.Run(ctx, data.Series, data.Fine, sig, data.Config, &result); err != nil {
		return result, miss, err
	}
	return result, miss, nil
}

func referencedNodes(slots ir.Slots) map[string]bool {
	used := make(map[string]bool)
	for _, slot := range []ir.SlotSpec{slots.Bias, slots.POI, slots.Trigger} {
		for _, id := range slot.All {
			used[id] = true
		}
	}
	if slots.Invalidation != "" {
		used[slots.Invalidation] = true
	}
	if slots.Target != "" {
		used[slots.Target] = true
	}
	return used
}

func referencedNodesForStrategy(strategy *ir.Strategy) map[string]bool {
	used := referencedNodes(strategy.Slots)
	if strategy.Schema != ir.SchemaV2 {
		return used
	}
	collectExpressionNodes(used, strategy.Entries.Long.When)
	collectExpressionNodes(used, strategy.Entries.Short.When)
	if o := strategy.Entries.Order; o != nil && o.Node != "" {
		used[o.Node] = true
	}
	if strategy.Slots.Invalidation != "" {
		used[strategy.Slots.Invalidation] = true
	}
	if strategy.Slots.Target != "" {
		used[strategy.Slots.Target] = true
	}
	return used
}

func collectExpressionNodes(used map[string]bool, expression ir.Expression) {
	if expression.Node != "" {
		used[expression.Node] = true
	}
	if expression.Comparison != nil && expression.Comparison.Node != "" {
		used[expression.Comparison.Node] = true
	}
	if expression.Price != nil && expression.Price.Node != "" {
		used[expression.Price.Node] = true
	}
	if expression.Zone != nil && expression.Zone.Node != "" {
		used[expression.Zone.Node] = true
	}
	if expression.Within != nil {
		collectExpressionNodes(used, expression.Within.Condition)
	}
	if expression.Persists != nil {
		collectExpressionNodes(used, expression.Persists.Condition)
	}
	if expression.After != nil {
		collectExpressionNodes(used, expression.After.Event)
		collectExpressionNodes(used, expression.After.Predecessor)
	}
	for _, child := range expression.All {
		collectExpressionNodes(used, child)
	}
	for _, child := range expression.Any {
		collectExpressionNodes(used, child)
	}
	if expression.Not != nil {
		collectExpressionNodes(used, *expression.Not)
	}
}
func decodeParams(p primitive.Primitive, raw json.RawMessage) (proto.Message, error) {
	m, err := parameterMessage(string(p.Spec().Params.FullName()))
	if err != nil {
		return nil, err
	}
	if len(raw) == 0 || string(raw) == "null" {
		return applyLegacyDefaults(m), nil
	}
	if err := protojson.Unmarshal(raw, m); err != nil {
		return nil, err
	}
	return applyLegacyDefaults(m), nil
}

// applyLegacyDefaults lets previously saved guided strategies remain runnable
// after parameter fields became explicit. Values are deliberately conservative
// and match the defaults authored by the current guided UI.
func applyLegacyDefaults(m proto.Message) proto.Message {
	switch x := m.(type) {
	case *assayv1.MarketStructureParams:
		if x.SwingLookback == 0 {
			x.SwingLookback = 5
		}
	case *assayv1.DisplacementParams:
		if x.MinBodyRatioMilli == 0 {
			x.MinBodyRatioMilli = 600
		}
		if x.MinConsecutiveBars == 0 {
			x.MinConsecutiveBars = 1
		}
	case *assayv1.FairValueGapParams:
		if x.MinSizeTicks == 0 {
			x.MinSizeTicks = 1
		}
		if x.MitigationRule == assayv1.MitigationRule_MITIGATION_RULE_UNSPECIFIED {
			x.MitigationRule = assayv1.MitigationRule_MITIGATION_RULE_FIRST_TOUCH
		}
		if x.ExpiryBars == 0 {
			x.ExpiryBars = 20
		}
	case *assayv1.OrderBlockParams:
		if x.ClusterBars == 0 {
			x.ClusterBars = 1
		}
		if x.DisplacementWithinBars == 0 {
			x.DisplacementWithinBars = 5
		}
		if x.MinBodyRatioMilli == 0 {
			x.MinBodyRatioMilli = 600
		}
	case *assayv1.BreakerParams:
		if x.InvalidatingTouches == 0 {
			x.InvalidatingTouches = 1
		}
	case *assayv1.LiquidityPoolParams:
		if x.MinTouches == 0 {
			x.MinTouches = 2
		}
		if x.AgeBars == 0 {
			x.AgeBars = 50
		}
	case *assayv1.SweepParams:
		if x.CloseBackBars == 0 {
			x.CloseBackBars = 3
		}
	case *assayv1.PremiumDiscountParams:
		if x.DealingRangeBars == 0 {
			x.DealingRangeBars = 20
		}
		if x.OteLowerMilli == 0 {
			x.OteLowerMilli = 620
		}
		if x.OteUpperMilli == 0 {
			x.OteUpperMilli = 790
		}
	}
	return m
}
func parameterMessage(name string) (proto.Message, error) {
	switch name {
	case "assay.v1.SampleParams":
		return &assayv1.SampleParams{}, nil
	case "assay.v1.MovingAverageParams":
		return &assayv1.MovingAverageParams{}, nil
	case "assay.v1.RsiParams":
		return &assayv1.RsiParams{}, nil
	case "assay.v1.AtrParams":
		return &assayv1.AtrParams{}, nil
	case "assay.v1.PeriodParams":
		return &assayv1.PeriodParams{}, nil
	case "assay.v1.SlopeParams":
		return &assayv1.SlopeParams{}, nil
	case "assay.v1.MovingAverageTouchParams":
		return &assayv1.MovingAverageTouchParams{}, nil
	case "assay.v1.MovingAverageCrossParams":
		return &assayv1.MovingAverageCrossParams{}, nil
	case "assay.v1.MacdParams":
		return &assayv1.MacdParams{}, nil
	case "assay.v1.BollingerBandsParams":
		return &assayv1.BollingerBandsParams{}, nil
	case "assay.v1.AdxParams":
		return &assayv1.AdxParams{}, nil
	case "assay.v1.StochasticParams":
		return &assayv1.StochasticParams{}, nil
	case "assay.v1.SwingParams":
		return &assayv1.SwingParams{}, nil
	case "assay.v1.ClusterParams":
		return &assayv1.ClusterParams{}, nil
	case "assay.v1.PinBarParams":
		return &assayv1.PinBarParams{}, nil
	case "assay.v1.DojiParams":
		return &assayv1.DojiParams{}, nil
	case "assay.v1.RangeParams":
		return &assayv1.RangeParams{}, nil
	case "assay.v1.MeasuredMoveParams":
		return &assayv1.MeasuredMoveParams{}, nil
	case "assay.v1.SessionParams":
		return &assayv1.SessionParams{}, nil
	case "assay.v1.DayOfWeekParams":
		return &assayv1.DayOfWeekParams{}, nil
	case "assay.v1.TimeOfDayRangeParams":
		return &assayv1.TimeOfDayRangeParams{}, nil
	case "assay.v1.BarOfSessionParams":
		return &assayv1.BarOfSessionParams{}, nil
	case "assay.v1.MarketStructureParams":
		return &assayv1.MarketStructureParams{}, nil
	case "assay.v1.DisplacementParams":
		return &assayv1.DisplacementParams{}, nil
	case "assay.v1.FairValueGapParams":
		return &assayv1.FairValueGapParams{}, nil
	case "assay.v1.OrderBlockParams":
		return &assayv1.OrderBlockParams{}, nil
	case "assay.v1.BreakerParams":
		return &assayv1.BreakerParams{}, nil
	case "assay.v1.LiquidityPoolParams":
		return &assayv1.LiquidityPoolParams{}, nil
	case "assay.v1.SweepParams":
		return &assayv1.SweepParams{}, nil
	case "assay.v1.PremiumDiscountParams":
		return &assayv1.PremiumDiscountParams{}, nil
	case "google.protobuf.Struct":
		return &structpb.Struct{}, nil
	default:
		return nil, fmt.Errorf("engine: unsupported parameter schema %q", name)
	}
}
func holds(slot ir.SlotSpec, vals map[string]primitive.Value, bar int) bool {
	for _, id := range slot.All {
		v := vals[id]
		if v.Valid == nil || !v.Valid.Get(bar) || v.Bool == nil || !v.Bool.Get(bar) {
			return false
		}
	}
	return true
}

func holdsExpression(series *core.Series, expression ir.Expression, vals map[string]primitive.Value, bar int) bool {
	if expression.Node != "" {
		v := vals[expression.Node]
		return v.Valid != nil && v.Valid.Get(bar) && v.Bool != nil && v.Bool.Get(bar)
	}
	if expression.Not != nil {
		return !holdsExpression(series, *expression.Not, vals, bar)
	}
	if expression.Comparison != nil {
		comparison := expression.Comparison
		v := vals[comparison.Node]
		if v.Valid == nil || !v.Valid.Get(bar) || len(v.Number) <= bar {
			return false
		}
		switch comparison.Operator {
		case ir.ComparisonGreaterThan:
			return v.Number[bar] > comparison.Threshold
		case ir.ComparisonLessThan:
			return v.Number[bar] < comparison.Threshold
		case ir.ComparisonCrossesAbove:
			return bar > 0 && v.Valid.Get(bar-1) && v.Number[bar-1] <= comparison.Threshold && v.Number[bar] > comparison.Threshold
		case ir.ComparisonCrossesBelow:
			return bar > 0 && v.Valid.Get(bar-1) && v.Number[bar-1] >= comparison.Threshold && v.Number[bar] < comparison.Threshold
		default:
			return false
		}
	}
	if expression.Price != nil {
		predicate := expression.Price
		v := vals[predicate.Node]
		if series == nil || v.Valid == nil || !v.Valid.Get(bar) || len(v.Level) <= bar {
			return false
		}
		price := priceAt(series, predicate.Source, bar)
		switch predicate.Relation {
		case ir.PriceAbove:
			return price > v.Level[bar]
		case ir.PriceBelow:
			return price < v.Level[bar]
		case ir.PriceCrossesAbove:
			return bar > 0 && v.Valid.Get(bar-1) && priceAt(series, predicate.Source, bar-1) <= v.Level[bar-1] && price > v.Level[bar]
		case ir.PriceCrossesBelow:
			return bar > 0 && v.Valid.Get(bar-1) && priceAt(series, predicate.Source, bar-1) >= v.Level[bar-1] && price < v.Level[bar]
		default:
			return false
		}
	}
	if expression.Zone != nil {
		predicate := expression.Zone
		v := vals[predicate.Node]
		if series == nil || v.Valid == nil || !v.Valid.Get(bar) || len(v.Zone) <= bar {
			return false
		}
		zone := v.Zone[bar]
		switch predicate.Relation {
		case ir.ZoneInside:
			return inZone(series.Close[bar], zone)
		case ir.ZoneTouches:
			return series.Low[bar] <= zone.High && series.High[bar] >= zone.Low
		case ir.ZoneLeaves:
			return bar > 0 && v.Valid.Get(bar-1) && !inZone(series.Close[bar], zone) && inZone(series.Close[bar-1], v.Zone[bar-1])
		default:
			return false
		}
	}
	if expression.Within != nil {
		w := resolveWindow(series, expression.Within.Window, expression.Within.Bars)
		for i := bar; i >= 0 && w.contains(series, bar, i); i-- {
			if holdsExpression(series, expression.Within.Condition, vals, i) {
				return true
			}
		}
		return false
	}
	if expression.Persists != nil {
		w := resolveWindow(series, expression.Persists.Window, expression.Persists.Bars)
		first := bar
		for first > 0 && w.spans(series, bar, first-1) {
			first--
		}
		// A window the series does not fully cover cannot be reported as an
		// unbroken condition: the bars before the series began were not
		// observed, and an unobserved bar is not a holding one.
		if !w.covered(series, bar, first) {
			return false
		}
		for i := first; i <= bar; i++ {
			if !holdsExpression(series, expression.Persists.Condition, vals, i) {
				return false
			}
		}
		return true
	}
	if expression.After != nil {
		if !holdsExpression(series, expression.After.Event, vals, bar) {
			return false
		}
		w := resolveWindow(series, expression.After.Within, expression.After.WithinBars)
		for i := bar - 1; i >= 0 && w.contains(series, bar, i); i-- {
			if holdsExpression(series, expression.After.Predecessor, vals, i) {
				return true
			}
		}
		return false
	}
	if len(expression.All) != 0 {
		for _, child := range expression.All {
			if !holdsExpression(series, child, vals, bar) {
				return false
			}
		}
		return true
	}
	for _, child := range expression.Any {
		if holdsExpression(series, child, vals, bar) {
			return true
		}
	}
	return false
}

// window is a resolved temporal window on the entry series.
//
// Windows are durations in the IR, because a bar count means different spans
// to nodes on different timeframes. They are resolved against bar close times
// here, which also makes them behave correctly across a weekend gap, where a
// bar count silently means something else. A document written before windows
// were durations carries a bar count instead; it is converted using the entry
// timeframe, which is what it meant when it was written.
type window struct {
	span core.Duration
	bars int // Only for a tick series, where bars have no fixed span.
}

func resolveWindow(series *core.Series, span core.Duration, bars int) window {
	if span > 0 {
		return window{span: span}
	}
	if d := core.TFDuration(series.TF); d > 0 {
		return window{span: core.Duration(bars) * d}
	}
	return window{bars: bars}
}

// contains reports whether bar i lies within the window ending at bar's close.
// Both bars are on the entry series, so comparing open times compares closes.
func (w window) contains(series *core.Series, bar, i int) bool {
	if w.span > 0 {
		return core.Duration(series.Time[bar]-series.Time[i]) <= w.span
	}
	return bar-i <= w.bars
}

// spans is contains with an exclusive bound: a window of one bar covers only
// the bar it ends on.
func (w window) spans(series *core.Series, bar, i int) bool {
	if w.span > 0 {
		return core.Duration(series.Time[bar]-series.Time[i]) < w.span
	}
	return bar-i < w.bars
}

// covered reports whether the series reaches back far enough for bars first
// through bar to span the whole window.
func (w window) covered(series *core.Series, bar, first int) bool {
	if w.span > 0 {
		return core.Duration(series.Time[bar]-series.Time[first])+core.TFDuration(series.TF) >= w.span
	}
	return bar-first+1 >= w.bars
}

func priceAt(series *core.Series, source ir.PriceSource, bar int) core.Price {
	switch source {
	case ir.PriceOpen:
		return series.Open[bar]
	case ir.PriceHigh:
		return series.High[bar]
	case ir.PriceLow:
		return series.Low[bar]
	default:
		return series.Close[bar]
	}
}

func inZone(price core.Price, zone primitive.Zone) bool {
	return price >= zone.Low && price <= zone.High
}

func runV2Bar(strategy *ir.Strategy, series *core.Series, values map[string]primitive.Value, sig *sim.Signals, miss *NearMiss, bar int, relativeExits bool, ex entryPlan) {
	long := strategy.Entries.Long.Enabled && holdsExpression(series, strategy.Entries.Long.When, values, bar)
	short := strategy.Entries.Short.Enabled && holdsExpression(series, strategy.Entries.Short.When, values, bar)
	if long && short {
		// Signals are one instruction per bar. Opposing entries on the same close
		// have no unambiguous ordering, so skip them rather than manufacture one.
		miss.SlotFailures["entry_conflict"]++
		miss.Bars = append(miss.Bars, Miss{Bar: bar, Failed: []string{"entry_conflict"}})
		return
	}
	if long || short {
		if !ex.ready(bar) {
			// The rule fired but its ATR distance cannot be resolved yet. An
			// entry sized against a zero distance would vanish without comment.
			miss.SlotFailures["atr_warmup"]++
			miss.Bars = append(miss.Bars, Miss{Bar: bar, Failed: []string{"atr_warmup"}})
			return
		}
		side := core.SideLong
		if short {
			side = core.SideShort
		}
		if relativeExits {
			addRelativeSide(sig, side, bar, strategy.Risk.Stop, strategy.Risk.Target, ex)
		} else {
			addSide(sig, side, bar, values[strategy.Slots.Invalidation].Level[bar], values[strategy.Slots.Target].Level[bar], ex)
		}
		return
	}
	m := Miss{Bar: bar}
	if strategy.Entries.Long.Enabled {
		m.Failed = append(m.Failed, "long")
		miss.SlotFailures["long"]++
	}
	if strategy.Entries.Short.Enabled {
		m.Failed = append(m.Failed, "short")
		miss.SlotFailures["short"]++
	}
	miss.Bars = append(miss.Bars, m)
}

func addSides(sig *sim.Signals, d ir.Direction, bar int, stop, target core.Price, ex entryPlan) {
	switch d {
	case ir.DirectionShort:
		addSide(sig, core.SideShort, bar, stop, target, ex)
	default:
		addSide(sig, core.SideLong, bar, stop, target, ex)
	}
}

func addRelativeSides(sig *sim.Signals, d ir.Direction, bar int, stopDistance, targetDistance core.Distance, ex entryPlan) {
	side := core.SideLong
	if d == ir.DirectionShort {
		side = core.SideShort
	}
	addRelativeSide(sig, side, bar, stopDistance, targetDistance, ex)
}

func addSide(sig *sim.Signals, side core.Side, bar int, stop, target core.Price, ex entryPlan) {
	sig.Enter.Set(bar)
	sig.Side = append(sig.Side, side)
	sig.StopLevel = append(sig.StopLevel, stop)
	sig.TargetPlan = append(sig.TargetPlan, targetPlan(ex.management, target))
	addATR(sig, bar, ex)
}

func addRelativeSide(sig *sim.Signals, side core.Side, bar int, stopDistance, targetDistance core.Distance, ex entryPlan) {
	sig.Enter.Set(bar)
	sig.Side = append(sig.Side, side)
	sig.StopLevel = append(sig.StopLevel, 0)
	// The level is filled in by the simulator once the entry fill resolves the
	// target distance; the management carried here is independent of it.
	sig.TargetPlan = append(sig.TargetPlan, targetPlan(ex.management, 0))
	sig.StopSpec = append(sig.StopSpec, stopDistance)
	sig.TargetSpec = append(sig.TargetSpec, targetDistance)
	addATR(sig, bar, ex)
}

// addATR records the ATR the simulator will resolve this entry's distances
// against, and the order the signal becomes. Both are read at the signal bar,
// not at the fill: the fill happens on the next bar, whose range the signal
// cannot see.
func addATR(sig *sim.Signals, bar int, ex entryPlan) {
	if ex.hasATR {
		sig.ATR = append(sig.ATR, ex.value(bar))
	}
	if ex.order != nil && ex.order.Resting() {
		sig.Orders = append(sig.Orders, ex.orderAt(bar))
	}
}

// orderAt builds the resting order for a signal detected at bar. A level node
// gives the trigger outright; an offset gives it relative to the signal bar's
// close, which the simulator turns into a price once it knows the tick size.
func (e entryPlan) orderAt(bar int) sim.Order {
	order := sim.Order{Type: sim.OrderLimit}
	if e.order.Type == ir.OrderStop {
		order.Type = sim.OrderStop
	}
	if e.hasLevel {
		order.Level = e.level.Level[bar]
		return order
	}
	order.Spec, order.Ref = *e.order.Offset, e.close[bar]
	return order
}

// targetPlan carries the document's trade management onto the entry. A nil
// management is the pre-management behaviour: close the whole position at the
// target and leave the stop where it was placed.
func targetPlan(m *ir.Management, level core.Price) sim.TargetPlan {
	plan := sim.TargetPlan{Level: level}
	if m == nil {
		return plan
	}
	plan.Close = m.Close
	plan.MoveStopToBreakEven = m.BreakEven
	plan.TrailSpec = m.Trail
	return plan
}

// entryPlan is everything about a document's entry and exit handling that a
// signal carries into the simulator. It exists so that adding an order or exit
// concept does not add another positional argument to every signal-emitting
// function.
type entryPlan struct {
	order      *ir.Order
	close      []core.Price
	level      primitive.Value
	hasLevel   bool
	management *ir.Management
	// atr holds the ATR reading per entry-series bar. The simulator has no
	// indicators of its own, so a distance expressed as a multiple of ATR can
	// only be resolved from a series the engine supplies.
	atr    primitive.Value
	hasATR bool
}

// ready reports whether an entry detected at bar can be resolved into an order.
// An ATR distance read before the indicator has warmed up resolves to a zero
// distance, which sizes the position at zero lots and drops the entry without
// saying so. Refusing it here turns a silent gap into a counted near miss.
func (e entryPlan) ready(bar int) bool {
	if e.hasATR && (e.atr.Valid == nil || !e.atr.Valid.Get(bar)) {
		return false
	}
	// A resting order whose level primitive has not produced a reading yet has
	// no price to rest at. Entering at the market instead would be a different
	// strategy, so the signal is refused and counted.
	return !e.hasLevel || (e.level.Valid != nil && e.level.Valid.Get(bar))
}

// value returns the ATR at bar in ticks, or zero when the document never asks
// for one.
func (e entryPlan) value(bar int) core.Price {
	if !e.hasATR || bar >= len(e.atr.Level) {
		return 0
	}
	return e.atr.Level[bar]
}

// ATRPeriod is the lookback used to resolve a distance written as a multiple of
// ATR. A Distance carries no period of its own, so the engine commits to
// Wilder's standard 14 and says so wherever an ATR unit is offered, rather than
// letting each surface assume its own.
const ATRPeriod = 14

// resolvePlan gathers everything an entry needs beyond its own condition: the
// order it becomes, and the ATR its distances resolve against. The ATR is read
// through the same registry, cache and alignment as any other primitive, so it
// matches the atr detector exactly instead of reimplementing it.
func resolvePlan(ctx context.Context, strategy *ir.Strategy, data Data, values map[string]primitive.Value, reg *primitive.Registry) (entryPlan, error) {
	ex := entryPlan{management: strategy.Risk.Management, close: data.Series.Close}
	// Entries, and so the order, belong to schema v2. A v1 document hashes
	// without them, so honouring one here would let two documents with the same
	// identity run differently.
	if strategy.Schema == ir.SchemaV2 {
		ex.order = strategy.Entries.Order
	}
	if o := ex.order; o != nil && o.Node != "" {
		v, ok := values[o.Node]
		if !ok {
			return ex, fmt.Errorf("engine: order level: node %s was not evaluated", o.Node)
		}
		ex.level, ex.hasLevel = v, true
	}
	needs := strategy.Risk.Stop.NeedsATR() || strategy.Risk.Target.NeedsATR()
	if strategy.Risk.Management != nil && strategy.Risk.Management.Trail.NeedsATR() {
		needs = true
	}
	if !needs {
		return ex, nil
	}
	p, err := reg.Get("indicator", "atr")
	if err != nil {
		return ex, fmt.Errorf("engine: atr distance: %w", err)
	}
	v, _, err := primitive.EvaluateAligned(ctx, p, &assayv1.AtrParams{Period: ATRPeriod}, data.Series.TF, data.Series, data.Snapshot, data.Cache, data.Stale)
	if err != nil {
		return ex, fmt.Errorf("engine: atr distance: %w", err)
	}
	if v.Len() != data.Series.Len() {
		return ex, fmt.Errorf("engine: atr distance: reading cannot be aligned to the entry series")
	}
	ex.atr, ex.hasATR = v, true
	return ex, nil
}
