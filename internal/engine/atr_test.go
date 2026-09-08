package engine

import (
	"context"
	"testing"

	"github.com/bcypher01/assay/internal/core"
	"github.com/bcypher01/assay/internal/ir"
	"github.com/bcypher01/assay/internal/primitive"
)

// atrSeries has a constant four-tick true range, so a warmed-up ATR reads
// exactly 4 and the resolved distances are checkable by hand.
func atrSeries(n int) *core.Series {
	s := core.NewSeries("X", core.TFM1, n)
	for i := 0; i < n; i++ {
		s.Append(core.Time(int64(i)*core.TFM1.Millis()), 100, 102, 98, 100, 0)
	}
	return s
}

func atrRegistry(t *testing.T, values map[string]primitive.Value) *primitive.Registry {
	t.Helper()
	reg := scriptedRegistry(t, values)
	p, err := primitive.DefaultRegistry.Get("indicator", "atr")
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Register(p); err != nil {
		t.Fatal(err)
	}
	return reg
}

func atrStrategy(stop, target core.Ratio) *ir.Strategy {
	s := strategy(ir.DirectionLong, 0)
	// Empty level slots select the distance-based exits an ATR unit applies to.
	s.Slots.Invalidation, s.Slots.Target = "", ""
	s.Nodes = s.Nodes[:3]
	s.Risk = ir.Risk{
		Stop:   core.Distance{Unit: core.UnitATR, Value: stop},
		Target: core.Distance{Unit: core.UnitATR, Value: target},
	}
	return s
}

// Before the engine supplied an ATR reading, the simulator resolved every ATR
// distance to zero ticks, sized the position at zero lots and dropped the entry
// without a word: an ATR study reported no trades and no reason.
func TestATRDistancesProduceTrades(t *testing.T) {
	const signal = 20
	reg := atrRegistry(t, map[string]primitive.Value{
		"bias":    boolValue(40, signal),
		"poi":     boolValue(40, signal),
		"trigger": boolValue(40, signal),
	})
	result, _, err := Run(context.Background(), atrStrategy(core.RatioFromInt(1), core.RatioFromInt(2)), Data{
		Series:   atrSeries(40),
		Config:   engineConfig(),
		Registry: reg,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Trades) != 1 {
		t.Fatalf("an ATR-based study produced no trade: %#v", result.Trades)
	}
	// Entry fills at the bar-21 open of 100 against a four-tick ATR.
	got := result.Trades[0]
	if got.EntryPrice != 100 || got.Stop != 96 || got.Target != 108 {
		t.Fatalf("ATR distances did not resolve to 1x and 2x of a four-tick range: %#v", got)
	}
}

// A rule that fires before the ATR has warmed up must be reported, not dropped.
// Silently sizing it to nothing is what made the old behaviour hard to notice.
func TestATRWarmupIsReportedNotDropped(t *testing.T) {
	const signal = 5 // inside the 14-bar ATR warm-up
	reg := atrRegistry(t, map[string]primitive.Value{
		"bias":    boolValue(40, signal),
		"poi":     boolValue(40, signal),
		"trigger": boolValue(40, signal),
	})
	result, misses, err := Run(context.Background(), atrStrategy(core.RatioFromInt(1), core.RatioFromInt(2)), Data{
		Series:   atrSeries(40),
		Config:   engineConfig(),
		Registry: reg,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Trades) != 0 {
		t.Fatalf("an entry inside the ATR warm-up should not trade: %#v", result.Trades)
	}
	if misses.SlotFailures["atr_warmup"] != 1 {
		t.Fatalf("the skipped entry was not reported: %#v", misses.SlotFailures)
	}
}

// A document that never mentions ATR must not pay for one, and must produce
// exactly the signals it did before ATR resolution existed.
func TestNoATRDistanceLeavesSignalsUntouched(t *testing.T) {
	reg := atrRegistry(t, map[string]primitive.Value{
		"bias":    boolValue(40, 20),
		"poi":     boolValue(40, 20),
		"trigger": boolValue(40, 20),
	})
	s := atrStrategy(core.RatioFromInt(1), core.RatioFromInt(2))
	s.Risk = ir.Risk{
		Stop:   core.Distance{Unit: core.UnitTicks, Value: core.RatioFromInt(4)},
		Target: core.Distance{Unit: core.UnitTicks, Value: core.RatioFromInt(8)},
	}
	result, misses, err := Run(context.Background(), s, Data{
		Series:   atrSeries(40),
		Config:   engineConfig(),
		Registry: reg,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Trades) != 1 || result.Trades[0].Stop != 96 || result.Trades[0].Target != 108 {
		t.Fatalf("tick distances changed: %#v", result.Trades)
	}
	if misses.SlotFailures["atr_warmup"] != 0 {
		t.Fatal("a document with no ATR distance reported an ATR warm-up")
	}
}
