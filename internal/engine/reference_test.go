package engine

import (
	"context"
	"testing"

	"github.com/bcypher01/assay/internal/core"
	"github.com/bcypher01/assay/internal/ir"
	"github.com/bcypher01/assay/internal/primitive"
)

// m15Days builds a fifteen-minute series covering whole UTC days, so a daily
// node can be aggregated from it and projected back down.
func m15Days(days [][]core.Price) *core.Series {
	perDay := 96
	s := core.NewSeries("X", core.TFM15, len(days)*perDay)
	start := core.MustParseTime("2025-01-06T00:00:00Z")
	for d, closes := range days {
		for i := 0; i < perDay; i++ {
			p := closes[i%len(closes)]
			t := start + core.Time(int64(d*perDay+i)*core.TFM15.Millis())
			s.Append(t, p, p, p, p, 1)
		}
	}
	return s
}

// The end-to-end case Workstream 3 exists for: a daily high, read from an M15
// chart, crossed by an M15 close. It exercises the level detector, the coarse
// aggregation, the projection back onto the entry index, and the price
// predicate in one run — none of which is provable from unit tests alone.
func TestPreviousDayHighTemplateTrades(t *testing.T) {
	// Day one holds 100. Day two opens at 90 and closes through 100.
	series := m15Days([][]core.Price{
		{100},
		{90, 90, 105},
	})
	strategy := ir.NewPreviousDayHighBreak()
	strategy.Universe.Symbols = []string{"X"}

	result, _, err := Run(context.Background(), strategy, Data{
		Series:   series,
		Config:   engineConfig(),
		Snapshot: "fixture",
		Registry: primitive.DefaultRegistry,
		Stale:    primitive.StalePolicy{Hold: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Trades) == 0 {
		t.Fatal("a close through the previous day's high produced no trade")
	}
	// Nothing may fire on day one: no daily bar has closed yet, so there is no
	// previous-day high to cross.
	for _, trade := range result.Trades {
		if trade.EntryBar < 96 {
			t.Fatalf("a trade fired before the first daily bar closed: bar %d", trade.EntryBar)
		}
	}
}

// The same template on a series that never breaks the level must produce
// nothing — a detector that fires either way proves nothing at all.
func TestPreviousDayHighTemplateStaysFlatBelowTheLevel(t *testing.T) {
	series := m15Days([][]core.Price{
		{100},
		{90, 95, 99},
	})
	strategy := ir.NewPreviousDayHighBreak()
	strategy.Universe.Symbols = []string{"X"}

	result, _, err := Run(context.Background(), strategy, Data{
		Series:   series,
		Config:   engineConfig(),
		Snapshot: "fixture",
		Registry: primitive.DefaultRegistry,
		Stale:    primitive.StalePolicy{Hold: true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Trades) != 0 {
		t.Fatalf("price never crossed the level, yet it traded: %#v", result.Trades)
	}
}

// The determinism gate for the whole reference path.
func TestReferenceTemplateRunIsDeterministic(t *testing.T) {
	series := m15Days([][]core.Price{{100}, {90, 90, 105}, {101, 108, 99}})
	strategy := ir.NewPreviousDayHighBreak()
	strategy.Universe.Symbols = []string{"X"}
	data := Data{
		Series:   series,
		Config:   engineConfig(),
		Snapshot: "fixture",
		Registry: primitive.DefaultRegistry,
		Stale:    primitive.StalePolicy{Hold: true},
	}
	first, _, err := Run(context.Background(), strategy, data)
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := Run(context.Background(), strategy, data)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Trades) != len(second.Trades) {
		t.Fatalf("trade counts differ: %d and %d", len(first.Trades), len(second.Trades))
	}
	for i := range first.Trades {
		if first.Trades[i] != second.Trades[i] {
			t.Fatalf("trade %d differs:\n%#v\n%#v", i, first.Trades[i], second.Trades[i])
		}
	}
}

// parameterMessage is a hand-maintained switch, so a newly registered
// primitive's parameters can be missing from it and fail only at run time,
// with an error that names a proto schema rather than the thing the user did.
// Every primitive the registry serves must be decodable before that happens.
func TestEveryRegisteredPrimitiveHasDecodableParameters(t *testing.T) {
	for _, spec := range primitive.DefaultRegistry.Specs() {
		p, err := primitive.DefaultRegistry.Get(spec.Pack, spec.Type)
		if err != nil {
			t.Fatalf("%s/%s: %v", spec.Pack, spec.Type, err)
		}
		name := string(p.Spec().Params.FullName())
		if _, err := parameterMessage(name); err != nil {
			t.Errorf("%s/%s parameters (%s) cannot be decoded: %v — add the case to parameterMessage", spec.Pack, spec.Type, name, err)
		}
	}
}
