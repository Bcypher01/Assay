package eventstudy

import (
	"math"
	"strings"
	"testing"

	"github.com/Bcypher01/Assay/internal/core"
	"github.com/Bcypher01/Assay/internal/event"
	"github.com/Bcypher01/Assay/internal/instrument"
)

func TestRunAlignsAfterKnowledgeAndExcludesImprecise(t *testing.T) {
	s := core.NewSeries("EURUSD", core.TFM1, 4)
	for i, p := range []core.Price{100, 110, 120, 130} {
		s.Append(core.Time(i*60_000), p, p, p, p, 1)
	}
	exact := event.Event{ID: "x", OccurredAt: 0, KnowledgeAt: core.Time(30_000), Precision: event.PrecisionExact, Kind: event.KindUSCPI, Title: "x", Jurisdiction: "US", Currencies: []string{"USD"}, Impact: event.ImpactHigh, Status: event.StatusReleased, Source: event.SourceRef{Provider: "x", RecordID: "x"}}
	day := exact
	day.ID = "d"
	day.Precision = event.PrecisionDay
	day.OccurredAt = 0
	got, err := Run(Request{Events: event.Snapshot{Manifest: event.Manifest{ContentHash: "events"}, Events: []event.Event{exact, day}}, Series: s, Windows: []core.TF{core.TFM1}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Eligible) != 1 || got.Eligible[0].AlignedBar != 60_000 || got.Eligible[0].Returns["m1"] != float64(10)/110 {
		t.Fatalf("%+v", got)
	}
	if len(got.Excluded) != 1 {
		t.Fatalf("%+v", got.Excluded)
	}
}

func TestAutomaticTradeCostsScaleWithExposure(t *testing.T) {
	spec := &instrument.Spec{Costs: instrument.CostModel{SpreadTicks: 8, SlippageMode: instrument.SlippageFixed, SlippageTicks: 3}}
	config := TradeConfig{PositionSize: 10_000, EstimateCosts: true, CostSpec: spec}
	if got := tradeCosts(config, 116_000); got != 1 {
		t.Fatalf("$100 estimated costs = %d cents, want 1", got)
	}
	config.PositionSize = 1_000_000
	if got := tradeCosts(config, 116_000); got != 121 {
		t.Fatalf("$10,000 estimated costs = %d cents, want 121", got)
	}
}

func TestTradeSimulationUsesNextBarAndAppliesCosts(t *testing.T) {
	base := core.MustParseTime("2024-01-05T13:30:00Z")
	s := core.NewSeries("DUKAS:EURUSD", core.TFM1, 4)
	s.Append(base, 100, 101, 99, 100, 1)
	s.Append(base+60_000, 100, 103, 100, 102, 1)
	s.Append(base+120_000, 102, 105, 102, 104, 1)
	s.Append(base+180_000, 104, 104, 104, 104, 1)
	actual, forecast := event.Decimal("250"), event.Decimal("200")
	e := event.Event{ID: "nfp", OccurredAt: base, KnowledgeAt: base, Precision: event.PrecisionExact, Kind: event.KindUSNonfarmPayrolls, Actual: &actual, Forecast: &forecast}

	got, err := Run(Request{
		Events: event.Snapshot{Events: []event.Event{e}}, Series: s, Windows: []core.TF{core.TFM1},
		Trade: &TradeConfig{EntryTiming: EntryAfterRelease, DirectionRule: DirectionShort, EntryDelay: core.TFTick, HoldFor: core.TFM1, SurpriseFilter: SurpriseAll, StartingEquity: 100_000, PositionSize: 10_000, CostPerTrade: 25},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Trades) != 1 {
		t.Fatalf("trades = %+v", got.Trades)
	}
	trade := got.Trades[0]
	if trade.EntryTime != base+60_000 || trade.ExitTime != base+120_000 {
		t.Fatalf("entry/exit = %v/%v", trade.EntryTime, trade.ExitTime)
	}
	if trade.GrossPnL != -200 || trade.Costs != 25 || trade.NetPnL != -225 {
		t.Fatalf("pnl = %+v", trade)
	}
	if got.TradeSummary.EndingEquity != 99_775 || got.TradeSummary.MaxDrawdown != 225 {
		t.Fatalf("summary = %+v", got.TradeSummary)
	}
}

func TestTradeSimulationIsPessimisticWhenStopAndTargetShareBar(t *testing.T) {
	base := core.MustParseTime("2024-01-05T13:30:00Z")
	s := core.NewSeries("DUKAS:EURUSD", core.TFM1, 3)
	s.Append(base, 100, 100, 100, 100, 1)
	s.Append(base+60_000, 100, 102, 98, 100, 1)
	s.Append(base+120_000, 100, 100, 100, 100, 1)
	e := event.Event{ID: "nfp", OccurredAt: base, KnowledgeAt: base, Precision: event.PrecisionExact, Kind: event.KindUSNonfarmPayrolls}

	got, err := Run(Request{
		Events: event.Snapshot{Events: []event.Event{e}}, Series: s, Windows: []core.TF{core.TFM1},
		Trade: &TradeConfig{EntryTiming: EntryAfterRelease, DirectionRule: DirectionLong, HoldFor: core.TFM1, SurpriseFilter: SurpriseAll, PositionSize: 10_000, StopLoss: 0.01, TakeProfit: 0.01},
	})
	if err != nil {
		t.Fatal(err)
	}
	trade := got.Trades[0]
	if trade.ExitReason != "stop" || !trade.Ambiguous || trade.StrategyReturn != -0.01 {
		t.Fatalf("trade = %+v", trade)
	}
}

func TestTradeSimulationRejectsCoarseExecutionBars(t *testing.T) {
	s := core.NewSeries("DUKAS:EURUSD", core.TFH1, 2)
	s.Append(0, 100, 100, 100, 100, 1)
	s.Append(core.Time(core.TFH1.Millis()), 100, 100, 100, 100, 1)
	e := event.Event{ID: "nfp", OccurredAt: 0, KnowledgeAt: 0, Precision: event.PrecisionExact, Kind: event.KindUSNonfarmPayrolls}
	_, err := Run(Request{Events: event.Snapshot{Events: []event.Event{e}}, Series: s, Windows: []core.TF{core.TFH1}, Trade: &TradeConfig{EntryTiming: EntryAfterRelease, DirectionRule: DirectionLong, HoldFor: core.TFH1, PositionSize: 1}})
	if err == nil || !strings.Contains(err.Error(), "m5 bars or finer") {
		t.Fatalf("error = %v", err)
	}
}

func TestSurpriseZScoreUsesOnlyEarlierReleases(t *testing.T) {
	decimal := func(value string) *event.Decimal { v := event.Decimal(value); return &v }
	events := []event.Event{
		{ID: "one", Kind: event.KindUSNonfarmPayrolls, Actual: decimal("110"), Forecast: decimal("100")},
		{ID: "two", Kind: event.KindUSNonfarmPayrolls, Actual: decimal("130"), Forecast: decimal("100")},
		{ID: "three", Kind: event.KindUSNonfarmPayrolls, Actual: decimal("150"), Forecast: decimal("100")},
	}
	got := surpriseHistory(events)
	if got["one"].Z != 0 || got["two"].Z != 0 || got["three"].Z != 3 {
		t.Fatalf("surprises = %+v", got)
	}
}

func TestRunExcludesIncompleteWindowRatherThanTruncatingIt(t *testing.T) {
	s := core.NewSeries("EURUSD", core.TFM15, 2)
	s.Append(core.MustParseTime("2024-01-01T10:00:00Z"), 100, 100, 100, 100, 1)
	s.Append(core.MustParseTime("2024-01-01T10:15:00Z"), 101, 101, 101, 101, 1)
	e := event.Event{ID: "x", OccurredAt: s.Time[0], KnowledgeAt: s.Time[0], Precision: event.PrecisionExact, Kind: event.KindUSCPI, Title: "x", Jurisdiction: "US", Currencies: []string{"USD"}, Impact: event.ImpactHigh, Status: event.StatusReleased, Source: event.SourceRef{Provider: "x", RecordID: "x"}}
	got, err := Run(Request{Events: event.Snapshot{Events: []event.Event{e}}, Series: s, Windows: []core.TF{core.TFH1}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Eligible) != 0 || len(got.Excluded) != 1 || got.Excluded[0].Reason != "insufficient bars for requested forward window" {
		t.Fatalf("%+v", got)
	}
}

func TestRunReportsAbsoluteDirectionAndBaselineMetrics(t *testing.T) {
	base := core.MustParseTime("2024-01-05T13:00:00Z")
	s := core.NewSeries("DUKAS:GBPUSD", core.TFH1, 0)
	for i := -4 * 7 * 24; i <= 4; i++ {
		price := core.Price(10000)
		if i == 4 {
			price = 10100
		}
		for week := 1; week <= 4; week++ {
			if i == -week*7*24+4 {
				price = 10010
			}
		}
		s.Append(base+core.Time(int64(i)*core.TFH1.Millis()), price, price, price, price, 1)
	}
	e := event.Event{ID: "nfp", OccurredAt: base, KnowledgeAt: base, Precision: event.PrecisionExact}
	got, err := Run(Request{Events: event.Snapshot{Events: []event.Event{e}}, Series: s, Windows: []core.TF{core.TFH4}})
	if err != nil {
		t.Fatal(err)
	}
	row := got.Windows[0]
	if math.Abs(row.MedianAbs-0.01) > 1e-12 || row.UpCount != 1 || row.DownCount != 0 || math.Abs(row.ImpactRatio-10) > 1e-12 {
		t.Fatalf("window = %+v", row)
	}
}

func TestRunRejectsWindowFinerThanBars(t *testing.T) {
	s := core.NewSeries("EURUSD", core.TFD1, 2)
	s.Append(0, 100, 100, 100, 100, 1)
	s.Append(core.Time(core.TFD1.Millis()), 101, 101, 101, 101, 1)
	_, err := Run(Request{Series: s, Windows: []core.TF{core.TFH1}})
	if err == nil || !strings.Contains(err.Error(), "h1 window requires h1 bars or finer") {
		t.Fatalf("error = %v", err)
	}
}
