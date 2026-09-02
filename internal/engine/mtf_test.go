package engine

import (
	"context"
	"encoding/json"
	"testing"

	"google.golang.org/protobuf/proto"

	assayv1 "github.com/bcypher01/assay/gen/go/assay/v1"
	"github.com/bcypher01/assay/internal/core"
	"github.com/bcypher01/assay/internal/ir"
	"github.com/bcypher01/assay/internal/primitive"
)

// ladderSeries is two days of M15 bars from 2024-01-01, so that H4 and D1
// nodes both have several completed bars over it.
func ladderSeries(n int) *core.Series {
	base := core.MustParseTime("2024-01-01T00:00:00Z")
	s := core.NewSeries("X", core.TFM15, n)
	for i := 0; i < n; i++ {
		s.Append(base+core.Time(int64(i)*core.TFM15.Millis()), 100, 101, 99, 100, 0)
	}
	return s
}

// barPrimitive emits a boolean that is true on the bars its predicate selects,
// sized to whatever series it is evaluated over. Unlike the fixed-length
// scripted primitives it can be run at a coarser timeframe than the entry
// series, which is the whole point of these tests.
type barPrimitive struct {
	typ  string
	when func(i int) bool
}

func (p barPrimitive) Spec() primitive.Spec {
	return primitive.Spec{Pack: "test", Type: p.typ, Out: primitive.KindBool, Params: (*assayv1.SampleParams)(nil).ProtoReflect().Descriptor(), Timeframed: true}
}
func (p barPrimitive) New(proto.Message, core.TF) (primitive.Instance, error) {
	return barInstance{p}, nil
}

type barInstance struct{ p barPrimitive }

func (x barInstance) Eval(_ *primitive.EvalCtx, s *core.Series) (primitive.Value, error) {
	n := s.Len()
	v := primitive.Value{Kind: primitive.KindBool, Bool: core.NewBitset(n), Valid: core.NewBitset(n)}
	for i := 0; i < n; i++ {
		v.Valid.Set(i)
		if x.p.when(i) {
			v.Bool.Set(i)
		}
	}
	return v, nil
}
func (barInstance) Overlays(core.Time, core.Time) []primitive.Overlay { return nil }

func ladderRegistry(t *testing.T, primitives ...barPrimitive) *primitive.Registry {
	t.Helper()
	reg := primitive.NewRegistry()
	for _, p := range primitives {
		if err := reg.Register(p); err != nil {
			t.Fatal(err)
		}
	}
	return reg
}

// ladderStrategy is bias on biasTF and trigger on the entry timeframe, joined
// by a single all-expression.
func ladderStrategy(biasTF, entryTF core.TF) *ir.Strategy {
	return &ir.Strategy{
		Schema:     ir.SchemaV2,
		Name:       "ladder",
		Universe:   ir.Universe{Symbols: []string{"X"}},
		Timeframes: map[string]core.TF{"bias": biasTF, "poi": entryTF, "entry": entryTF},
		Risk:       ir.Risk{Stop: core.Distance{Unit: core.UnitTicks, Value: core.RatioFromInt(1)}, Target: core.Distance{Unit: core.UnitTicks, Value: core.RatioFromInt(1)}},
		Template:   ir.Template{ID: "ladder", Version: 1},
		Nodes: []ir.Node{
			{ID: "bias", Pack: "test", Type: "bias", Timeframe: biasTF},
			{ID: "trigger", Pack: "test", Type: "trigger", Timeframe: entryTF},
		},
		Entries: ir.Entries{Timing: ir.EntryTimingNextOpen, Long: ir.EntryBranch{
			Enabled: true, Name: "Coarse bias with fine trigger",
			When: ir.Expression{All: []ir.Expression{{Node: "bias"}, {Node: "trigger"}}},
		}},
	}
}

// A ladder is the workflow the IR has always described and the engine has
// never been able to execute: form a bias on a coarse timeframe, trigger on a
// fine one.
func TestRunProjectsCoarseBiasOntoEntryBars(t *testing.T) {
	reg := ladderRegistry(t,
		barPrimitive{typ: "bias", when: func(i int) bool { return i == 0 }},
		barPrimitive{typ: "trigger", when: func(int) bool { return true }},
	)
	_, _, err := Run(context.Background(), ladderStrategy(core.TFH4, core.TFM15), Data{
		Series: ladderSeries(192), Config: engineConfig(), Registry: reg,
	})
	if err != nil {
		t.Fatalf("a genuine H4/M15 ladder must run: %v", err)
	}
}

// The first H4 bar closes at 04:00, the same instant as the M15 bar spanning
// 03:45-04:00. That bar may act on it: the close is fully determined by data
// at or before 04:00. Reading it one bar later would be a systematic lag.
func TestRunSeesCoarseBarOnItsCoincidentClose(t *testing.T) {
	reg := ladderRegistry(t,
		barPrimitive{typ: "bias", when: func(i int) bool { return i == 0 }},
		barPrimitive{typ: "trigger", when: func(int) bool { return true }},
	)
	result, _, err := Run(context.Background(), ladderStrategy(core.TFH4, core.TFM15), Data{
		Series: ladderSeries(192), Config: engineConfig(), Registry: reg,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Trades) == 0 {
		t.Fatal("the projected bias never held")
	}
	// Signal on M15 bar 15 (closing 04:00), filled at the next open on bar 16.
	if result.Trades[0].EntryBar != 16 {
		t.Fatalf("first entry bar = %d, want 16: the bias must be visible on the entry bar that closes with the H4 bar", result.Trades[0].EntryBar)
	}
}

// Warmup is invalid, never zero-filled: before the first coarse bar closes
// there is no bias, and a strategy that requires one cannot enter.
func TestRunHasNoEntriesBeforeFirstCoarseClose(t *testing.T) {
	reg := ladderRegistry(t,
		barPrimitive{typ: "bias", when: func(int) bool { return true }},
		barPrimitive{typ: "trigger", when: func(int) bool { return true }},
	)
	result, _, err := Run(context.Background(), ladderStrategy(core.TFD1, core.TFM15), Data{
		Series: ladderSeries(192), Config: engineConfig(), Registry: reg,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, trade := range result.Trades {
		if trade.EntryBar <= 95 {
			t.Fatalf("entry on bar %d precedes the first completed D1 bar", trade.EntryBar)
		}
	}
}

// A node finer than the entry series cannot be aggregated from it. It is
// refused rather than silently degraded to the entry timeframe.
func TestRunRefusesNodeFinerThanEntrySeries(t *testing.T) {
	reg := ladderRegistry(t,
		barPrimitive{typ: "bias", when: func(int) bool { return true }},
		barPrimitive{typ: "trigger", when: func(int) bool { return true }},
	)
	strategy := ladderStrategy(core.TFH4, core.TFM15)
	strategy.Nodes[1].Timeframe = core.TFM5
	if _, _, err := Run(context.Background(), strategy, Data{
		Series: ladderSeries(192), Config: engineConfig(), Registry: reg,
	}); err == nil {
		t.Fatal("an M5 node under an M15 entry series must be refused")
	}
}

// A run is a pure function of (IR document, data snapshot, seed). Projection
// must not change that.
func TestRunLadderIsDeterministic(t *testing.T) {
	run := func() []byte {
		t.Helper()
		reg := ladderRegistry(t,
			barPrimitive{typ: "bias", when: func(i int) bool { return i%2 == 0 }},
			barPrimitive{typ: "trigger", when: func(i int) bool { return i%3 == 0 }},
		)
		result, miss, err := Run(context.Background(), ladderStrategy(core.TFH4, core.TFM15), Data{
			Series: ladderSeries(192), Config: engineConfig(), Registry: reg,
			Cache: primitive.NewMemoCache(32),
		})
		if err != nil {
			t.Fatal(err)
		}
		b, err := json.Marshal(struct {
			Result any
			Miss   any
		}{result, miss})
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	first, second := run(), run()
	if string(first) != string(second) {
		t.Fatal("two runs of the same ladder strategy produced different output")
	}
}

// A window is a span of time, not a count of entry bars. The two agree on a
// gapless series, which is what makes the legacy bar count safe to convert.
func TestDurationWindowsMatchTheirBarCounts(t *testing.T) {
	series := ladderSeries(10)
	values := map[string]primitive.Value{"a": boolValue(10, 0)}
	condition := ir.Expression{Node: "a"}
	for _, c := range []struct {
		name string
		span ir.Expression
		bars ir.Expression
	}{
		{
			name: "within",
			span: ir.Expression{Within: &ir.OccurredWithin{Condition: condition, Window: core.Hour}},
			bars: ir.Expression{Within: &ir.OccurredWithin{Condition: condition, Bars: 4}},
		},
		{
			name: "after",
			span: ir.Expression{After: &ir.OccursAfter{Event: ir.Expression{Node: "a"}, Predecessor: condition, Within: core.Hour}},
			bars: ir.Expression{After: &ir.OccursAfter{Event: ir.Expression{Node: "a"}, Predecessor: condition, WithinBars: 4}},
		},
	} {
		for bar := 0; bar < series.Len(); bar++ {
			if holdsExpression(series, c.span, values, bar) != holdsExpression(series, c.bars, values, bar) {
				t.Fatalf("%s: 1h and 4 M15 bars disagree at bar %d", c.name, bar)
			}
		}
	}
}

func TestDurationWindowBounds(t *testing.T) {
	series := ladderSeries(10)
	values := map[string]primitive.Value{"a": boolValue(10, 0)}
	within := ir.Expression{Within: &ir.OccurredWithin{Condition: ir.Expression{Node: "a"}, Window: core.Hour}}
	// Bar 0 holds; an hour later, bar 4, is the last bar still inside the window.
	if !holdsExpression(series, within, values, 4) {
		t.Fatal("a condition an hour old is inside a one-hour window")
	}
	if holdsExpression(series, within, values, 5) {
		t.Fatal("a condition 75 minutes old is outside a one-hour window")
	}
}

// A window the series does not cover is not an unbroken condition: the bars
// before the series began were never observed.
func TestPersistsRequiresTheWholeWindow(t *testing.T) {
	series := ladderSeries(10)
	values := map[string]primitive.Value{"a": boolValue(10, 0, 1, 2, 3, 4)}
	persists := ir.Expression{Persists: &ir.RemainsTrue{Condition: ir.Expression{Node: "a"}, Window: core.Hour}}
	if holdsExpression(series, persists, values, 2) {
		t.Fatal("only 45 minutes of history cannot satisfy a one-hour window")
	}
	if !holdsExpression(series, persists, values, 3) {
		t.Fatal("four M15 bars cover a one-hour window")
	}
	if holdsExpression(series, persists, values, 5) {
		t.Fatal("the condition failed inside the window")
	}
}

// A cross on a projected value fires on the entry bar that closes with the
// coarse bar producing it, and on no other bar.
//
// The concern this settles is that consecutive entry bars carry equal
// projected values, so a bar-to-bar comparison could only ever fire at the
// step. The inclusive close-time join puts that step on the coincident-close
// bar, which is the instant the coarse bar closed — so the comparison needs no
// special case, and forbidding cross predicates across timeframes would break
// the most useful ladder there is: entry price crossing a higher-timeframe
// level.
func TestProjectedCrossFiresOnTheCoarseClose(t *testing.T) {
	series := ladderSeries(96)
	coarse := core.NewSeries("X", core.TFH4, 3)
	base := core.MustParseTime("2024-01-01T00:00:00Z")
	for i := 0; i < 3; i++ {
		coarse.Append(base+core.Time(int64(i)*core.TFH4.Millis()), 100, 101, 99, 100, 0)
	}
	value := primitive.Value{
		Kind:   primitive.KindNumber,
		Valid:  core.NewBitset(3),
		Number: []core.Ratio{core.RatioFromInt(40), core.RatioFromInt(60), core.RatioFromInt(45)},
	}
	for i := 0; i < 3; i++ {
		value.Valid.Set(i)
	}
	projected, err := primitive.Project(value, coarse, series, primitive.StalePolicy{})
	if err != nil {
		t.Fatal(err)
	}
	expression := ir.Expression{Comparison: &ir.NumberComparison{
		Node: "rsi", Operator: ir.ComparisonCrossesAbove, Threshold: core.RatioFromInt(50),
	}}
	values := map[string]primitive.Value{"rsi": projected}
	var fired []int
	for bar := 0; bar < series.Len(); bar++ {
		if holdsExpression(series, expression, values, bar) {
			fired = append(fired, bar)
		}
	}
	// Bar 31 spans 07:45-08:00 and closes with the second H4 bar.
	if len(fired) != 1 || fired[0] != 31 {
		t.Fatalf("projected cross fired on bars %v, want only bar 31", fired)
	}
}
