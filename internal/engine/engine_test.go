package engine

import (
	"context"
	"testing"

	"github.com/Bcypher01/Assay/internal/core"
	"github.com/Bcypher01/Assay/internal/instrument"
	"github.com/Bcypher01/Assay/internal/ir"
	"github.com/Bcypher01/Assay/internal/primitive"
	"github.com/Bcypher01/Assay/internal/sim"
	assayv1 "github.com/Bcypher01/Assay/proto/assay/v1"
	"google.golang.org/protobuf/proto"
)

func TestRunUsesPOIWindowForLaterTrigger(t *testing.T) {
	reg := scriptedRegistry(t, map[string]primitive.Value{
		"bias":    boolValue(5, 0, 1, 2, 3, 4),
		"poi":     boolValue(5, 1),
		"trigger": boolValue(5, 2),
		"stop":    levelValue(5, 90),
		"target":  levelValue(5, 110),
	})

	result, misses, err := Run(context.Background(), strategy(ir.DirectionLong, 1), Data{
		Series:   engineSeries(),
		Config:   engineConfig(),
		Registry: reg,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Trades) != 1 || result.Trades[0].EntryBar != 3 {
		t.Fatalf("POI at bar 1 and trigger at bar 2 should enter on bar 3: %#v", result.Trades)
	}
	if failures := misses.SlotFailures; failures["poi"] != 3 || failures["trigger"] != 4 {
		t.Fatalf("near-miss counters should use the active POI window: %#v", failures)
	}
}

func TestRunDoesNotUseExpiredPOIWindow(t *testing.T) {
	reg := scriptedRegistry(t, map[string]primitive.Value{
		"bias":    boolValue(5, 0, 1, 2, 3, 4),
		"poi":     boolValue(5, 0),
		"trigger": boolValue(5, 2),
		"stop":    levelValue(5, 90),
		"target":  levelValue(5, 110),
	})

	result, misses, err := Run(context.Background(), strategy(ir.DirectionLong, 1), Data{
		Series: engineSeries(), Config: engineConfig(), Registry: reg,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Trades) != 0 {
		t.Fatalf("expired POI opened a trade: %#v", result.Trades)
	}
	if len(misses.Bars) != 5 || misses.Bars[2].Failed[0] != "poi" {
		t.Fatalf("expired POI should be recorded as a POI miss: %#v", misses)
	}
}

func TestRunUsesShortDirection(t *testing.T) {
	reg := scriptedRegistry(t, map[string]primitive.Value{
		"bias":    boolValue(5, 0, 1, 2, 3, 4),
		"poi":     boolValue(5, 1),
		"trigger": boolValue(5, 1),
		"stop":    levelValue(5, 110),
		"target":  levelValue(5, 90),
	})
	result, _, err := Run(context.Background(), strategy(ir.DirectionShort, 0), Data{
		Series: engineSeries(), Config: engineConfig(), Registry: reg,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Trades) != 1 || result.Trades[0].Side != core.SideShort {
		t.Fatalf("short strategy signal was not preserved: %#v", result.Trades)
	}
}

func TestRunV2UsesIndependentLongAndShortBranches(t *testing.T) {
	reg := scriptedRegistry(t, map[string]primitive.Value{
		"bullish": boolValue(5, 1),
		"bearish": boolValue(5, 3),
	})
	strategy := &ir.Strategy{
		Schema:     ir.SchemaV2,
		Name:       "crossover",
		Universe:   ir.Universe{Symbols: []string{"X"}},
		Timeframes: map[string]core.TF{"bias": core.TFM1, "poi": core.TFM1, "entry": core.TFM1},
		Risk:       ir.Risk{Stop: core.Distance{Unit: core.UnitTicks, Value: core.RatioFromInt(1)}, Target: core.Distance{Unit: core.UnitTicks, Value: core.RatioFromInt(1)}},
		Template:   ir.Template{ID: "ma-crossover", Version: 1},
		Entries: ir.Entries{Timing: ir.EntryTimingNextOpen,
			Long:  ir.EntryBranch{Enabled: true, Name: "Bullish crossover", When: ir.Expression{Node: "bullish"}},
			Short: ir.EntryBranch{Enabled: true, Name: "Bearish crossover", When: ir.Expression{Node: "bearish"}},
		},
		Nodes: []ir.Node{{ID: "bullish", Pack: "test", Type: "bullish"}, {ID: "bearish", Pack: "test", Type: "bearish"}},
	}
	result, _, err := Run(context.Background(), strategy, Data{Series: engineSeries(), Config: engineConfig(), Registry: reg})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Trades) != 2 || result.Trades[0].Side != core.SideLong || result.Trades[1].Side != core.SideShort {
		t.Fatalf("v2 branches did not preserve sides: %#v", result.Trades)
	}
}

func TestRunV2EvaluatesNumberComparison(t *testing.T) {
	reg := scriptedRegistry(t, map[string]primitive.Value{
		"rsi": numberValue([]core.Ratio{core.RatioFromInt(35), core.RatioFromInt(25), core.RatioFromInt(40), core.RatioFromInt(50), core.RatioFromInt(60)}),
	})
	strategy := &ir.Strategy{
		Schema:     ir.SchemaV2,
		Name:       "rsi pullback",
		Universe:   ir.Universe{Symbols: []string{"X"}},
		Timeframes: map[string]core.TF{"bias": core.TFM1, "poi": core.TFM1, "entry": core.TFM1},
		Risk:       ir.Risk{Stop: core.Distance{Unit: core.UnitTicks, Value: core.RatioFromInt(1)}, Target: core.Distance{Unit: core.UnitTicks, Value: core.RatioFromInt(1)}},
		Template:   ir.Template{ID: "rsi-pullback", Version: 1},
		Entries: ir.Entries{Timing: ir.EntryTimingNextOpen,
			Long: ir.EntryBranch{Enabled: true, Name: "RSI below 30", When: ir.Expression{Comparison: &ir.NumberComparison{
				Node: "rsi", Operator: ir.ComparisonLessThan, Threshold: core.RatioFromInt(30),
			}}},
		},
		Nodes: []ir.Node{{ID: "rsi", Pack: "test", Type: "rsi"}},
	}
	result, _, err := Run(context.Background(), strategy, Data{Series: engineSeries(), Config: engineConfig(), Registry: reg})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Trades) != 1 || result.Trades[0].EntryBar != 2 || result.Trades[0].Side != core.SideLong {
		t.Fatalf("numeric comparison did not create its next-open long entry: %#v", result.Trades)
	}
}

func TestHoldsExpressionNumberCrossComparison(t *testing.T) {
	values := map[string]primitive.Value{
		"rsi": numberValue([]core.Ratio{core.RatioFromInt(25), core.RatioFromInt(35), core.RatioFromInt(25)}),
	}
	above := ir.Expression{Comparison: &ir.NumberComparison{Node: "rsi", Operator: ir.ComparisonCrossesAbove, Threshold: core.RatioFromInt(30)}}
	below := ir.Expression{Comparison: &ir.NumberComparison{Node: "rsi", Operator: ir.ComparisonCrossesBelow, Threshold: core.RatioFromInt(30)}}
	if !holdsExpression(engineSeries(), above, values, 1) {
		t.Fatal("numeric cross above was not recognised")
	}
	if !holdsExpression(engineSeries(), below, values, 2) {
		t.Fatal("numeric cross below was not recognised")
	}
}

func TestRunMovingAverageCrossoverTemplate(t *testing.T) {
	series := core.NewSeries("EUR/USD", core.TFD1, 75)
	for i := 0; i < 75; i++ {
		price := core.Price(100)
		if i >= 50 {
			price = 200
		}
		series.Append(core.Time(int64(i)*core.TFD1.Millis()), price, price, price, price, 0)
	}
	config := sim.Config{
		Spec: instrument.Spec{Symbol: "EUR/USD", TickSize: "1", PipTicks: 1, Gap: instrument.GapFillAtOpen},
		Risk: sim.RiskModel{InitialEquity: 1_000, FixedLots: core.One},
	}
	result, _, err := Run(context.Background(), ir.NewMovingAverageCrossover(), Data{Series: series, Config: config})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Trades) != 1 || result.Trades[0].Side != core.SideLong || result.Trades[0].EntryBar != 51 {
		t.Fatalf("template did not enter on the bullish cross's next open: %#v", result.Trades)
	}
}

func TestHoldsExpressionTemporalOperators(t *testing.T) {
	values := map[string]primitive.Value{
		"event":   boolValue(5, 1, 2),
		"trigger": boolValue(5, 3),
	}
	event := ir.Expression{Node: "event"}
	trigger := ir.Expression{Node: "trigger"}
	if !holdsExpression(engineSeries(), ir.Expression{Within: &ir.OccurredWithin{Condition: event, Bars: 1}}, values, 3) {
		t.Fatal("event within one bar was not recognised")
	}
	if holdsExpression(engineSeries(), ir.Expression{Within: &ir.OccurredWithin{Condition: event, Bars: 1}}, values, 4) {
		t.Fatal("expired event was still within one bar")
	}
	if !holdsExpression(engineSeries(), ir.Expression{Persists: &ir.RemainsTrue{Condition: event, Bars: 2}}, values, 2) {
		t.Fatal("two-bar persistence was not recognised")
	}
	if holdsExpression(engineSeries(), ir.Expression{Persists: &ir.RemainsTrue{Condition: event, Bars: 3}}, values, 2) {
		t.Fatal("persistence ignored missing earlier bar")
	}
	if !holdsExpression(engineSeries(), ir.Expression{After: &ir.OccursAfter{Event: trigger, Predecessor: event, WithinBars: 1}}, values, 3) {
		t.Fatal("event after predecessor was not recognised")
	}
	if holdsExpression(engineSeries(), ir.Expression{After: &ir.OccursAfter{Event: trigger, Predecessor: event, WithinBars: 1}}, values, 2) {
		t.Fatal("after expression allowed the predecessor as its event")
	}
}

func TestHoldsExpressionPriceAndZonePredicates(t *testing.T) {
	series := core.NewSeries("X", core.TFM1, 4)
	for i, bar := range []struct{ open, high, low, close core.Price }{
		{90, 95, 85, 90},
		{100, 105, 95, 100},
		{110, 115, 105, 110},
		{90, 95, 85, 90},
	} {
		series.Append(core.Time(int64(i)*core.TFM1.Millis()), bar.open, bar.high, bar.low, bar.close, 0)
	}
	values := map[string]primitive.Value{
		"level": levelValue(4, 100),
		"zone":  zoneValue([]primitive.Zone{{Low: 95, High: 105}, {Low: 95, High: 105}, {Low: 95, High: 105}, {Low: 95, High: 105}}),
	}
	if !holdsExpression(series, ir.Expression{Price: &ir.PriceLevelPredicate{Node: "level", Source: ir.PriceClose, Relation: ir.PriceAbove}}, values, 2) {
		t.Fatal("price above was not recognised")
	}
	if !holdsExpression(series, ir.Expression{Price: &ir.PriceLevelPredicate{Node: "level", Source: ir.PriceClose, Relation: ir.PriceBelow}}, values, 0) {
		t.Fatal("price below was not recognised")
	}
	if !holdsExpression(series, ir.Expression{Price: &ir.PriceLevelPredicate{Node: "level", Source: ir.PriceClose, Relation: ir.PriceCrossesAbove}}, values, 2) {
		t.Fatal("price cross above was not recognised")
	}
	if !holdsExpression(series, ir.Expression{Price: &ir.PriceLevelPredicate{Node: "level", Source: ir.PriceClose, Relation: ir.PriceCrossesBelow}}, values, 3) {
		t.Fatal("price cross below was not recognised")
	}
	if !holdsExpression(series, ir.Expression{Zone: &ir.ZonePredicate{Node: "zone", Relation: ir.ZoneInside}}, values, 1) {
		t.Fatal("zone containment was not recognised")
	}
	if !holdsExpression(series, ir.Expression{Zone: &ir.ZonePredicate{Node: "zone", Relation: ir.ZoneTouches}}, values, 1) {
		t.Fatal("zone touch was not recognised")
	}
	if !holdsExpression(series, ir.Expression{Zone: &ir.ZonePredicate{Node: "zone", Relation: ir.ZoneLeaves}}, values, 2) {
		t.Fatal("zone exit was not recognised")
	}
}

func TestRunRejectsNilInputs(t *testing.T) {
	if _, _, err := Run(context.Background(), nil, Data{}); err == nil {
		t.Fatal("nil strategy was accepted")
	}
	if _, _, err := Run(context.Background(), strategy(ir.DirectionLong, 0), Data{}); err == nil {
		t.Fatal("nil series was accepted")
	}
}

func TestRunUsesCandleTimeframeForLegacyNodeWithoutTimeframe(t *testing.T) {
	reg := scriptedRegistry(t, map[string]primitive.Value{
		"bias":    boolValue(5, 0, 1, 2, 3, 4),
		"poi":     boolValue(5, 0, 1, 2, 3, 4),
		"trigger": boolValue(5, 0, 1, 2, 3, 4),
		"stop":    levelValue(5, 90), "target": levelValue(5, 110),
	})
	strategy := strategy(ir.DirectionLong, 0)
	// The zero value is how an omitted JSON node timeframe decodes.
	strategy.Nodes[0].Timeframe = core.TFTick

	if _, _, err := Run(context.Background(), strategy, Data{
		Series: engineSeries(), Config: engineConfig(), Registry: reg,
	}); err != nil {
		t.Fatalf("legacy node without timeframe should use candle timeframe: %v", err)
	}
}

func TestReferencedNodesExcludesUnwiredBuildingBlocks(t *testing.T) {
	used := referencedNodes(ir.Slots{
		Trigger: ir.SlotSpec{All: []string{"entry"}},
	})
	if !used["entry"] || used["unused-breaker"] {
		t.Fatalf("unexpected active-node set: %#v", used)
	}
}

func TestDecodeParamsAppliesDefaultsToLegacyMarketStructure(t *testing.T) {
	p, err := primitive.DefaultRegistry.Get("smc", "market_structure")
	if err != nil {
		t.Fatal(err)
	}
	params, err := decodeParams(p, nil)
	if err != nil {
		t.Fatal(err)
	}
	x, ok := params.(*assayv1.MarketStructureParams)
	if !ok || x.SwingLookback != 5 {
		t.Fatalf("market structure defaults = %#v", params)
	}
}

func strategy(direction ir.Direction, withinBars int) *ir.Strategy {
	return &ir.Strategy{
		Schema:    ir.SchemaV1,
		Name:      "fixture",
		Universe:  ir.Universe{Symbols: []string{"X"}},
		Direction: direction,
		Timeframes: map[string]core.TF{
			"bias": core.TFM1, "poi": core.TFM1, "entry": core.TFM1,
		},
		Risk: ir.Risk{Stop: core.Distance{Unit: core.UnitTicks, Value: core.RatioFromInt(1)}, Target: core.Distance{Unit: core.UnitTicks, Value: core.RatioFromInt(1)}},
		Nodes: []ir.Node{
			{ID: "bias", Pack: "test", Type: "bias"}, {ID: "poi", Pack: "test", Type: "poi"}, {ID: "trigger", Pack: "test", Type: "trigger"},
			{ID: "stop", Pack: "test", Type: "stop"}, {ID: "target", Pack: "test", Type: "target"},
		},
		Slots: ir.Slots{Bias: ir.SlotSpec{All: []string{"bias"}}, POI: ir.SlotSpec{All: []string{"poi"}}, Trigger: ir.SlotSpec{All: []string{"trigger"}, WithinBars: withinBars}, Invalidation: "stop", Target: "target"},
	}
}

func engineSeries() *core.Series {
	s := core.NewSeries("X", core.TFM1, 5)
	for i := 0; i < 5; i++ {
		s.Append(core.Time(int64(i)*core.TFM1.Millis()), 100, 101, 99, 100, 0)
	}
	return s
}

func engineConfig() sim.Config {
	return sim.Config{Spec: instrument.Spec{Symbol: "X", TickSize: "1", Gap: instrument.GapFillAtOpen}, Risk: sim.RiskModel{InitialEquity: 1_000, FixedLots: core.One}}
}

func boolValue(n int, set ...int) primitive.Value {
	b, valid := core.NewBitset(n), core.NewBitset(n)
	for i := 0; i < n; i++ {
		valid.Set(i)
	}
	for _, i := range set {
		b.Set(i)
	}
	return primitive.Value{Kind: primitive.KindBool, Bool: b, Valid: valid}
}

func levelValue(n int, level core.Price) primitive.Value {
	values := make([]core.Price, n)
	valid := core.NewBitset(n)
	for i := range values {
		values[i] = level
		valid.Set(i)
	}
	return primitive.Value{Kind: primitive.KindLevel, Level: values, Valid: valid}
}

func numberValue(values []core.Ratio) primitive.Value {
	valid := core.NewBitset(len(values))
	for i := range values {
		valid.Set(i)
	}
	return primitive.Value{Kind: primitive.KindNumber, Number: values, Valid: valid}
}

func zoneValue(values []primitive.Zone) primitive.Value {
	valid := core.NewBitset(len(values))
	for i := range values {
		valid.Set(i)
	}
	return primitive.Value{Kind: primitive.KindZone, Zone: values, Valid: valid}
}

func scriptedRegistry(t *testing.T, values map[string]primitive.Value) *primitive.Registry {
	t.Helper()
	reg := primitive.NewRegistry()
	for typ, value := range values {
		if err := reg.Register(scriptedPrimitive{typ: typ, value: value}); err != nil {
			t.Fatal(err)
		}
	}
	return reg
}

type scriptedPrimitive struct {
	typ   string
	value primitive.Value
}

func (p scriptedPrimitive) Spec() primitive.Spec {
	return primitive.Spec{Pack: "test", Type: p.typ, Out: p.value.Kind, Params: (*assayv1.SampleParams)(nil).ProtoReflect().Descriptor(), Timeframed: true}
}
func (p scriptedPrimitive) New(proto.Message, core.TF) (primitive.Instance, error) {
	return scriptedInstance{p.value}, nil
}

type scriptedInstance struct{ value primitive.Value }

func (x scriptedInstance) Eval(*primitive.EvalCtx, *core.Series) (primitive.Value, error) {
	return x.value, nil
}
func (scriptedInstance) Overlays(core.Time, core.Time) []primitive.Overlay { return nil }
