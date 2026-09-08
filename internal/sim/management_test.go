package sim

import (
	"context"
	"testing"

	"github.com/bcypher01/assay/internal/core"
	"github.com/bcypher01/assay/internal/instrument"
)

func longAt(bar, n int, stop core.Price, plan TargetPlan) Signals {
	b := core.NewBitset(n)
	b.Set(bar)
	return Signals{
		Enter:      b,
		Side:       []core.Side{core.SideLong},
		StopLevel:  []core.Price{stop},
		TargetPlan: []TargetPlan{plan},
	}
}

func runOne(t *testing.T, s *core.Series, sig Signals, cfg Config) RunResult {
	t.Helper()
	var out RunResult
	if err := Run(context.Background(), s, nil, sig, cfg, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Trades) != 1 {
		t.Fatalf("want one trade, got %d", len(out.Trades))
	}
	return out
}

// A partial close realises part of the position at the target and leaves the
// rest running, so the trade survives the bar that touched the target.
func TestPartialCloseLeavesRemainderOpen(t *testing.T) {
	// Enter at bar 1 open (100), touch the target on bar 2, drift on bar 3.
	s := fixtureSeries(core.TFM1, [][4]core.Price{
		{100, 100, 100, 100},
		{100, 100, 100, 100},
		{100, 106, 100, 104},
		{104, 105, 103, 104},
	})
	full := runOne(t, s, longAt(0, 4, 95, TargetPlan{Level: 105}), fixtureConfig())
	partial := runOne(t, s, longAt(0, 4, 95, TargetPlan{Level: 105, Close: core.One / 2}), fixtureConfig())

	if got := full.Trades[0].Pessimistic.ExitBar; got != 2 {
		t.Fatalf("a full target should close on the target bar, exited at %d", got)
	}
	if got := partial.Trades[0].Pessimistic.ExitBar; got != 3 {
		t.Fatalf("a partial target should leave the remainder open past the target bar, exited at %d", got)
	}
	if partial.Trades[0].Pessimistic.Net == full.Trades[0].Pessimistic.Net {
		t.Fatal("a half close settled identically to a full close")
	}
}

// Break-even only exists to protect what a partial close left behind: after the
// partial, the remainder must not be able to lose.
func TestBreakEvenProtectsTheRemainder(t *testing.T) {
	// Target on bar 2, then a slide back through the original stop on bar 3.
	s := fixtureSeries(core.TFM1, [][4]core.Price{
		{100, 100, 100, 100},
		{100, 100, 100, 100},
		{100, 106, 100, 104},
		{104, 104, 90, 92},
	})
	plan := TargetPlan{Level: 105, Close: core.One / 2}
	loose := runOne(t, s, longAt(0, 4, 95, plan), fixtureConfig())

	plan.MoveStopToBreakEven = true
	protected := runOne(t, s, longAt(0, 4, 95, plan), fixtureConfig())

	if got := protected.Trades[0].Pessimistic.Net; got <= loose.Trades[0].Pessimistic.Net {
		t.Fatalf("break-even did not protect the remainder: %d vs %d", got, loose.Trades[0].Pessimistic.Net)
	}
	if protected.Trades[0].Pessimistic.Net <= 0 {
		t.Fatalf("a position half closed in profit and stopped at entry must not lose: %d", protected.Trades[0].Pessimistic.Net)
	}
}

// A trail ratchets the stop behind the running high and never loosens it.
func TestTrailRatchetsAndNeverLoosens(t *testing.T) {
	s := fixtureSeries(core.TFM1, [][4]core.Price{
		{100, 100, 100, 100},
		{100, 100, 100, 100},
		{100, 120, 100, 118},
		{118, 118, 105, 106},
	})
	flat := runOne(t, s, longAt(0, 4, 95, TargetPlan{}), fixtureConfig())
	trailed := runOne(t, s, longAt(0, 4, 95, TargetPlan{TrailDistance: 5}), fixtureConfig())

	if flat.Trades[0].Pessimistic.ExitBar != 3 {
		t.Fatalf("the untrailed position should have run to the last bar, exited at %d", flat.Trades[0].Pessimistic.ExitBar)
	}
	// The bar-2 high of 120 pulls the stop to 115, which bar 3 takes out.
	if got := trailed.Trades[0].Pessimistic.ExitPrice; got != 115 {
		t.Fatalf("want a trailed stop at 115, exited at %d", got)
	}
	if trailed.Trades[0].Pessimistic.Net <= flat.Trades[0].Pessimistic.Net {
		t.Fatal("trailing the stop through a reversal should have kept more of the move")
	}
}

// A trail carrying a unit is resolved from the entry fill, exactly as the stop
// and target distances are, and lands on the same stop as the tick form.
func TestTrailSpecResolvesFromTheEntryFill(t *testing.T) {
	s := fixtureSeries(core.TFM1, [][4]core.Price{
		{100, 100, 100, 100},
		{100, 100, 100, 100},
		{100, 120, 100, 118},
		{118, 118, 105, 106},
	})
	cfg := fixtureConfig()
	cfg.Spec.PipTicks = 10

	ticks := runOne(t, s, longAt(0, 4, 95, TargetPlan{TrailDistance: 50}), cfg)
	pips := runOne(t, s, longAt(0, 4, 95, TargetPlan{TrailSpec: core.Distance{Unit: core.UnitPips, Value: core.RatioFromInt(5)}}), cfg)

	if pips.Trades[0].Pessimistic.ExitPrice != ticks.Trades[0].Pessimistic.ExitPrice {
		t.Fatalf("5 pips at 10 ticks each should trail like 50 ticks: %d vs %d",
			pips.Trades[0].Pessimistic.ExitPrice, ticks.Trades[0].Pessimistic.ExitPrice)
	}
}

// Management must not disturb the run's determinism guarantee.
func TestManagedRunIsDeterministic(t *testing.T) {
	s := fixtureSeries(core.TFM1, [][4]core.Price{
		{100, 100, 100, 100},
		{100, 100, 100, 100},
		{100, 106, 100, 104},
		{104, 118, 103, 117},
		{117, 117, 104, 105},
	})
	plan := TargetPlan{Level: 105, Close: core.One / 2, MoveStopToBreakEven: true, TrailSpec: core.Distance{Unit: core.UnitTicks, Value: core.RatioFromInt(6)}}
	cfg := Config{Spec: instrument.Spec{Symbol: "X", TickSize: "1", Gap: instrument.GapFillAtOpen}, Risk: RiskModel{InitialEquity: 1_000, FixedLots: core.One}}

	var a, b RunResult
	if err := Run(context.Background(), s, nil, longAt(0, 5, 95, plan), cfg, &a); err != nil {
		t.Fatal(err)
	}
	if err := Run(context.Background(), s, nil, longAt(0, 5, 95, plan), cfg, &b); err != nil {
		t.Fatal(err)
	}
	if a.Trades[0] != b.Trades[0] {
		t.Fatalf("a managed run was not reproducible:\n%#v\n%#v", a.Trades[0], b.Trades[0])
	}
}
