package engine

import (
	"context"
	"testing"

	"github.com/bcypher01/assay/internal/core"
	"github.com/bcypher01/assay/internal/ir"
	"github.com/bcypher01/assay/internal/primitive"
	"github.com/bcypher01/assay/internal/sim"
)

// orderSeries signals on bar 1 and gives bar 2 a range wide enough to reach a
// trigger on either side of the close.
func orderSeries() *core.Series {
	s := core.NewSeries("X", core.TFM1, 4)
	bars := [][4]core.Price{
		{100, 101, 99, 100},
		{100, 101, 99, 100},
		{100, 110, 90, 100},
		{100, 101, 99, 100},
	}
	for i, b := range bars {
		s.Append(core.Time(int64(i)*core.TFM1.Millis()), b[0], b[1], b[2], b[3], 0)
	}
	return s
}

func orderStrategy(order *ir.Order) *ir.Strategy {
	return &ir.Strategy{
		Schema:     ir.SchemaV2,
		Name:       "orders",
		Universe:   ir.Universe{Symbols: []string{"X"}},
		Timeframes: map[string]core.TF{"bias": core.TFM1, "poi": core.TFM1, "entry": core.TFM1},
		Risk: ir.Risk{
			Stop:   core.Distance{Unit: core.UnitTicks, Value: core.RatioFromInt(20)},
			Target: core.Distance{Unit: core.UnitTicks, Value: core.RatioFromInt(40)},
		},
		Template: ir.Template{ID: "orders", Version: 1},
		Nodes: []ir.Node{
			{ID: "trigger", Pack: "test", Type: "trigger"},
			{ID: "level", Pack: "test", Type: "level"},
		},
		Entries: ir.Entries{
			Timing: ir.EntryTimingNextOpen,
			Order:  order,
			Long:   ir.EntryBranch{Enabled: true, Name: "long", When: ir.Expression{Node: "trigger"}},
		},
	}
}

func orderRegistry(t *testing.T, level core.Price) *primitive.Registry {
	t.Helper()
	return scriptedRegistry(t, map[string]primitive.Value{
		"trigger": boolValue(4, 1),
		"level":   levelValue(4, level),
	})
}

func runOrder(t *testing.T, order *ir.Order, level core.Price) sim.RunResult {
	t.Helper()
	result, _, err := Run(context.Background(), orderStrategy(order), Data{
		Series:   orderSeries(),
		Config:   engineConfig(),
		Registry: orderRegistry(t, level),
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

// A limit rests below the market and fills only if price trades back to it.
func TestLimitOrderFillsAtItsLevel(t *testing.T) {
	got := runOrder(t, &ir.Order{Type: ir.OrderLimit, Node: "level"}, 95)
	if len(got.Trades) != 1 {
		t.Fatalf("the bar low of 90 should have filled a limit at 95: %#v", got.Trades)
	}
	if got.Trades[0].EntryPrice != 95 {
		t.Fatalf("want a fill at the limit, got %d", got.Trades[0].EntryPrice)
	}
}

// A stop rests above the market and fills only on the breakout.
func TestStopOrderFillsAtItsLevel(t *testing.T) {
	got := runOrder(t, &ir.Order{Type: ir.OrderStop, Node: "level"}, 105)
	if len(got.Trades) != 1 || got.Trades[0].EntryPrice != 105 {
		t.Fatalf("the bar high of 110 should have filled a stop at 105: %#v", got.Trades)
	}
}

// An order the bar never reaches must be counted, not quietly forgotten. A
// limit study that trades far less often than it signals is telling the truth
// about the strategy, and the run has to say so.
func TestUnreachedOrderIsReported(t *testing.T) {
	got := runOrder(t, &ir.Order{Type: ir.OrderLimit, Node: "level"}, 50)
	if len(got.Trades) != 0 {
		t.Fatalf("a limit at 50 under a low of 90 must not fill: %#v", got.Trades)
	}
	if got.Unfilled != 1 {
		t.Fatalf("the unfilled entry was not reported: %#v", got)
	}
}

// An offset order needs no level primitive: it rests that far from the signal
// bar's close, on the side its type implies.
func TestOffsetOrdersRestOnTheCorrectSide(t *testing.T) {
	ticks := func(n int64) *core.Distance {
		return &core.Distance{Unit: core.UnitTicks, Value: core.RatioFromInt(n)}
	}
	limit := runOrder(t, &ir.Order{Type: ir.OrderLimit, Offset: ticks(5)}, 0)
	stop := runOrder(t, &ir.Order{Type: ir.OrderStop, Offset: ticks(5)}, 0)

	// The signal bar closes at 100, so a long limit rests at 95 and a long stop
	// at 105. Both are inside the following bar's 90-110 range.
	if len(limit.Trades) != 1 || limit.Trades[0].EntryPrice != 95 {
		t.Fatalf("a long limit should rest below the close: %#v", limit.Trades)
	}
	if len(stop.Trades) != 1 || stop.Trades[0].EntryPrice != 105 {
		t.Fatalf("a long stop should rest above the close: %#v", stop.Trades)
	}
}

// A document with no order is a market entry at the next bar open, exactly as
// before order types existed.
func TestAbsentOrderStaysAMarketEntry(t *testing.T) {
	got := runOrder(t, nil, 0)
	if len(got.Trades) != 1 || got.Trades[0].EntryPrice != 100 {
		t.Fatalf("want the next bar open, got %#v", got.Trades)
	}
	if got.Unfilled != 0 {
		t.Fatal("a market entry cannot go unfilled")
	}
}
