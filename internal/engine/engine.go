package engine

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/Bcypher01/Assay/internal/core"
	"github.com/Bcypher01/Assay/internal/ir"
	_ "github.com/Bcypher01/Assay/internal/packs/indicator"
	_ "github.com/Bcypher01/Assay/internal/packs/priceaction"
	_ "github.com/Bcypher01/Assay/internal/packs/smc"
	_ "github.com/Bcypher01/Assay/internal/packs/timeofday"
	"github.com/Bcypher01/Assay/internal/primitive"
	"github.com/Bcypher01/Assay/internal/sim"
	assayv1 "github.com/Bcypher01/Assay/proto/assay/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

type Data struct {
	Series, Fine *core.Series
	Config       sim.Config
	Snapshot     string
	Cache        primitive.Cache
	Registry     *primitive.Registry
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
		v, _, e := primitive.Evaluate(ctx, p, params, tf, data.Series, data.Snapshot, data.Cache)
		if e != nil {
			return result, miss, e
		}
		if v.Len() != data.Series.Len() {
			return result, miss, fmt.Errorf("engine: node %s timeframe output cannot be aligned to entry series", n.ID)
		}
		values[n.ID] = v
	}
	sig := sim.Signals{Enter: core.NewBitset(data.Series.Len())}
	useRelativeExits := strategy.Slots.Invalidation == "" && strategy.Slots.Target == ""
	poiAt := -1
	for bar := 0; bar < data.Series.Len(); bar++ {
		if strategy.Schema == ir.SchemaV2 {
			runV2Bar(strategy, data.Series, values, &sig, &miss, bar, useRelativeExits)
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
		if strategy.Slots.Trigger.WithinBars > 0 {
			poiOK = poiAt >= 0 && bar-poiAt <= strategy.Slots.Trigger.WithinBars
			triggerOK = trigger && poiOK
		}
		if bias && poiOK && triggerOK {
			if useRelativeExits {
				addRelativeSides(&sig, strategy.Direction, bar, strategy.Risk.Stop, strategy.Risk.Target)
			} else {
				addSides(&sig, strategy.Direction, bar, values[strategy.Slots.Invalidation].Level[bar], values[strategy.Slots.Target].Level[bar])
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
		for age := 0; age <= expression.Within.Bars && age <= bar; age++ {
			if holdsExpression(series, expression.Within.Condition, vals, bar-age) {
				return true
			}
		}
		return false
	}
	if expression.Persists != nil {
		if expression.Persists.Bars > bar+1 {
			return false
		}
		for age := 0; age < expression.Persists.Bars; age++ {
			if !holdsExpression(series, expression.Persists.Condition, vals, bar-age) {
				return false
			}
		}
		return true
	}
	if expression.After != nil {
		if !holdsExpression(series, expression.After.Event, vals, bar) {
			return false
		}
		for age := 1; age <= expression.After.WithinBars && age <= bar; age++ {
			if holdsExpression(series, expression.After.Predecessor, vals, bar-age) {
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

func runV2Bar(strategy *ir.Strategy, series *core.Series, values map[string]primitive.Value, sig *sim.Signals, miss *NearMiss, bar int, relativeExits bool) {
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
		side := core.SideLong
		if short {
			side = core.SideShort
		}
		if relativeExits {
			addRelativeSide(sig, side, bar, strategy.Risk.Stop, strategy.Risk.Target)
		} else {
			addSide(sig, side, bar, values[strategy.Slots.Invalidation].Level[bar], values[strategy.Slots.Target].Level[bar])
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

func addSides(sig *sim.Signals, d ir.Direction, bar int, stop, target core.Price) {
	switch d {
	case ir.DirectionShort:
		addSide(sig, core.SideShort, bar, stop, target)
	default:
		addSide(sig, core.SideLong, bar, stop, target)
	}
}

func addRelativeSides(sig *sim.Signals, d ir.Direction, bar int, stopDistance, targetDistance core.Distance) {
	side := core.SideLong
	if d == ir.DirectionShort {
		side = core.SideShort
	}
	addRelativeSide(sig, side, bar, stopDistance, targetDistance)
}

func addSide(sig *sim.Signals, side core.Side, bar int, stop, target core.Price) {
	sig.Enter.Set(bar)
	sig.Side = append(sig.Side, side)
	sig.StopLevel = append(sig.StopLevel, stop)
	sig.TargetPlan = append(sig.TargetPlan, sim.TargetPlan{Level: target})
}

func addRelativeSide(sig *sim.Signals, side core.Side, bar int, stopDistance, targetDistance core.Distance) {
	sig.Enter.Set(bar)
	sig.Side = append(sig.Side, side)
	sig.StopLevel = append(sig.StopLevel, 0)
	sig.TargetPlan = append(sig.TargetPlan, sim.TargetPlan{})
	sig.StopSpec = append(sig.StopSpec, stopDistance)
	sig.TargetSpec = append(sig.TargetSpec, targetDistance)
}
