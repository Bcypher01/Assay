package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	assayv1 "github.com/bcypher01/assay/gen/go/assay/v1"
	"github.com/bcypher01/assay/internal/analytics"
	"github.com/bcypher01/assay/internal/core"
	"github.com/bcypher01/assay/internal/instrument"
	"github.com/bcypher01/assay/internal/ir"
	"github.com/bcypher01/assay/internal/library"
	_ "github.com/bcypher01/assay/internal/packs/indicator"
	"github.com/bcypher01/assay/internal/runstore"
	"github.com/bcypher01/assay/internal/setuptest"
	"github.com/bcypher01/assay/internal/sim"
	"github.com/bcypher01/assay/internal/store"
	"github.com/bcypher01/assay/internal/venue"
)

func TestSetupChartHTTPAllowsPreparedDukascopyH4Candles(t *testing.T) {
	ctx := context.Background()
	cache := store.NewLocalStore(t.TempDir())
	registry := instrument.MustDefault()
	series := core.NewSeries("DUKAS:EURUSD", core.TFH4, 6)
	base := core.MustParseTime("2024-06-03T00:00:00Z")
	for i := 0; i < 6; i++ {
		open := core.Price(108500 + i*20)
		series.Append(base+core.Time(int64(i)*core.TFH4.Millis()), open, open+35, open-25, open+15, int64(100+i))
	}
	if err := cache.Write(ctx, series, store.Manifest{
		Symbol: series.Symbol, TF: series.TF, Month: "2024-06", Source: "dukascopy-bid", BuildVersion: "test",
	}); err != nil {
		t.Fatal(err)
	}

	fallback := &countingFetcher{}
	loader := library.New(cache, nil, fallback)
	loader.Registry = registry
	server, err := NewLocalServerWithServices(cache, registry, loader, nil)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(setuptest.ChartRequest{
		Symbol: "DUKAS:EURUSD", Timeframe: "h4",
		FromMS: base.Millis(), ToMS: (base + core.Time(core.TFD1.Millis())).Millis(),
		MaxBars: 300, Prepare: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/setup/chart", bytes.NewReader(body))
	res := httptest.NewRecorder()
	NewHandler(server).ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("setup chart status=%d body=%s", res.Code, res.Body.String())
	}
	var chart setuptest.Chart
	if err := json.Unmarshal(res.Body.Bytes(), &chart); err != nil {
		t.Fatal(err)
	}
	if chart.Timeframe != "h4" || len(chart.Bars) != 6 {
		t.Fatalf("chart timeframe=%q bars=%d", chart.Timeframe, len(chart.Bars))
	}
	for i, bar := range chart.Bars {
		if bar.Open == bar.Close || bar.Volume == 0 || bar.Low >= bar.High {
			t.Fatalf("bar %d is not a usable candlestick: %+v", i, bar)
		}
	}
	if fallback.fetches != 0 {
		t.Fatalf("warm prepared chart unexpectedly fetched %d times", fallback.fetches)
	}
}

func TestLocalSetupTestChartPreviewAndPersistedRun(t *testing.T) {
	ctx := context.Background()
	cache := store.NewLocalStore(t.TempDir())
	history, err := runstore.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	registry, err := instrument.Default()
	if err != nil {
		t.Fatal(err)
	}
	series := core.NewSeries("DUKAS:EURUSD", core.TFD1, 7)
	base := core.MustParseTime("2026-08-01T00:00:00Z")
	for i, bar := range [][4]core.Price{
		{100009, 100010, 100008, 100009}, {100010, 100011, 100008, 100009},
		{100009, 100012, 100009, 100012}, {100012, 100013, 100011, 100013},
		{100013, 100014, 100011, 100013}, {100013, 100024, 100012, 100023},
		{100023, 100024, 100022, 100023},
	} {
		series.Append(base+core.Time(int64(i)*core.TFD1.Millis()), bar[0], bar[1], bar[2], bar[3], 1)
	}
	if err := cache.Write(ctx, series, store.Manifest{Symbol: series.Symbol, TF: series.TF, Month: "2026-08", Source: "fixture", BuildVersion: "test"}); err != nil {
		t.Fatal(err)
	}
	server, err := NewLocalServerWithServices(cache, registry, nil, history)
	if err != nil {
		t.Fatal(err)
	}
	from, to := base.Millis(), (base + core.Time(7*core.TFD1.Millis())).Millis()
	chart, err := server.SetupChartFn(ctx, setuptest.ChartRequest{Symbol: series.Symbol, Timeframe: "d1", FromMS: from, ToMS: to, MaxBars: 300})
	if err != nil {
		t.Fatal(err)
	}
	if len(chart.Bars) != 7 || chart.Snapshot == "" || chart.TickSize != "0.00001" {
		t.Fatalf("chart = %#v", chart)
	}
	drawing := setuptest.Drawing{Kind: setuptest.BreakRetest, Side: "long", Symbol: series.Symbol, Timeframe: "d1", AnchorTimeMS: series.Time[4].Millis(), LevelLow: 100012, LevelHigh: 100012, Entry: 100013, Stop: 100008, Target: 100023, LookbackBars: 3, RetestBars: 3, ToleranceTicks: 1, ExpiryBars: 10, DisplacementBodyMilli: 500, ConfirmByClose: true, RiskPerTrade: .005}
	preview, err := server.SetupPreviewFn(ctx, setuptest.PreviewRequest{Drawing: drawing, FromMS: from, ToMS: to})
	if err != nil {
		t.Fatal(err)
	}
	if !preview.SampleMatched || len(preview.Matches) == 0 {
		t.Fatalf("preview = %#v", preview)
	}
	summary, err := server.SetupRunFn(ctx, setuptest.RunRequest{Drawing: drawing, FromMS: from, ToMS: to, Assumptions: setuptest.Assumptions{InitialEquity: 1_000_000, RiskPerTrade: .005}})
	if err != nil {
		t.Fatal(err)
	}
	if summary.RunId == "" || summary.Trades == 0 {
		t.Fatalf("summary = %#v", summary)
	}
}

func TestLocalRunMissingCacheDoesNotFetch(t *testing.T) {
	ctx := context.Background()
	cache := store.NewLocalStore(t.TempDir())
	registry, err := instrument.Default()
	if err != nil {
		t.Fatal(err)
	}
	fallback := &countingFetcher{}
	loader := library.New(cache, nil, fallback)
	server, err := NewLocalServerWithServices(cache, registry, loader, nil)
	if err != nil {
		t.Fatal(err)
	}
	strategy := testStrategyJSON(t)
	summary, err := server.RunFn(ctx, &assayv1.RunRequest{
		StrategyJson: strategy,
		FromMs:       int64(core.MustParseTime("2024-01-01T00:00:00Z")),
		ToMs:         int64(core.MustParseTime("2024-02-01T00:00:00Z")),
	}, func(*assayv1.RunUpdate) error { return nil })
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if fallback.fetches != 0 {
		t.Fatalf("run fetched external data %d times", fallback.fetches)
	}
	if summary == nil || summary.ErrorCode != "DATA_MISSING" {
		t.Fatalf("summary = %+v, want DATA_MISSING", summary)
	}
}

func TestLocalDataStatusExposesMissingPartitions(t *testing.T) {
	ctx := context.Background()
	cache := store.NewLocalStore(t.TempDir())
	registry, err := instrument.Default()
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewLocalServerWithServices(cache, registry, library.New(cache, nil, &countingFetcher{}), nil)
	if err != nil {
		t.Fatal(err)
	}
	status, err := server.DataStatusFn(ctx, &assayv1.EnsureDataRequest{
		Symbol:    "DUKAS:EURUSD",
		Timeframe: "d1",
		FromMs:    int64(core.MustParseTime("2024-01-01T00:00:00Z")),
		ToMs:      int64(core.MustParseTime("2024-03-01T00:00:00Z")),
	})
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status.Readiness != assayv1.DataReadiness_DATA_READINESS_MISSING || status.ErrorCode != "DATA_MISSING" {
		t.Fatalf("status = %+v", status)
	}
	if len(status.MissingPartitions) != 2 {
		t.Fatalf("missing partitions = %d, want 2", len(status.MissingPartitions))
	}
}

type countingFetcher struct {
	fetches int
}

func (f *countingFetcher) Name() string { return "test-source" }

func (f *countingFetcher) Available(context.Context, string) (core.Time, core.Time, []core.TF, error) {
	return core.MinTime, core.MaxTime, []core.TF{core.TFD1}, nil
}

func (f *countingFetcher) Fetch(context.Context, string, core.TF, core.Time, core.Time) (*core.Series, error) {
	f.fetches++
	return nil, venue.ErrUnavailable
}

func testStrategyJSON(t *testing.T) []byte {
	t.Helper()
	strategy := ir.Strategy{
		Schema:    ir.SchemaV1,
		Name:      "missing cache",
		Universe:  ir.Universe{Symbols: []string{"DUKAS:EURUSD"}},
		Direction: ir.DirectionLong,
		Timeframes: map[string]core.TF{
			"bias":  core.TFD1,
			"poi":   core.TFD1,
			"entry": core.TFD1,
		},
		Risk: ir.Risk{
			Stop:   core.Distance{Unit: core.UnitPips, Value: core.RatioFromInt(5)},
			Target: core.Distance{Unit: core.UnitPips, Value: core.RatioFromInt(10)},
		},
		Nodes: []ir.Node{
			{ID: "entry", Pack: "indicator", Type: "ma_touch", Timeframe: core.TFD1, Params: []byte(`{}`)},
		},
		Slots: ir.Slots{
			Trigger: ir.SlotSpec{All: []string{"entry"}},
		},
	}
	b, err := json.Marshal(strategy)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestRunConfigAppliesExplicitAssumptions(t *testing.T) {
	strategy := ir.NewMovingAverageCrossover()
	cfg, err := runConfig(instrument.Spec{Costs: instrument.CostModel{SpreadTicks: 2}}, strategy, &assayv1.StudyAssumptions{
		InitialEquity: 1_000_000, RiskPerTrade: 0.01, SpreadTicks: 4,
		CommissionBps: 10, SlippageTicks: 3, FillPolicy: "next_open",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Risk.InitialEquity != 1_000_000 || cfg.Spec.Costs.SpreadTicks != 4 || cfg.Slippage.Ticks != 3 {
		t.Fatalf("config = %#v", cfg)
	}
	if cfg.Spec.Costs.CommissionMode != instrument.CommissionBps {
		t.Fatalf("commission mode = %v", cfg.Spec.Costs.CommissionMode)
	}
}

func TestRunConfigRejectsUnsupportedFill(t *testing.T) {
	_, err := runConfig(instrument.Spec{}, ir.NewMovingAverageCrossover(), &assayv1.StudyAssumptions{InitialEquity: 1, FillPolicy: "close"})
	if err == nil {
		t.Fatal("expected unsupported fill policy error")
	}
}

func TestRunCostFreeUsesEffectiveInstrumentDefaults(t *testing.T) {
	registry, err := instrument.Default()
	if err != nil {
		t.Fatal(err)
	}
	strategy, err := json.Marshal(ir.NewMovingAverageCrossover())
	if err != nil {
		t.Fatal(err)
	}
	free, known := runCostFree(registry, &assayv1.RunRequest{StrategyJson: strategy})
	if !known || free {
		t.Fatalf("defaults reported free=%v known=%v; expected known non-zero costs", free, known)
	}
}

// A walk-forward test window is what makes a result out-of-sample. Trades that
// entered while the rules were still being fitted must not leak into the unseen
// sample, and a window with no trades must stay empty rather than inherit the
// full period's wins.
func TestPooledWindowReturnsKeepsUnseenSampleHonest(t *testing.T) {
	trades := []sim.Trade{
		{EntryBar: 2, Pessimistic: sim.Outcome{Net: 100}, Optimistic: sim.Outcome{Net: 100}}, // fitted window
		{EntryBar: 6, Pessimistic: sim.Outcome{Net: 10}, Optimistic: sim.Outcome{Net: 20}},   // unseen window
		{EntryBar: 9, Pessimistic: sim.Outcome{Net: -4}, Optimistic: sim.Outcome{Net: 6}},    // unseen window
	}
	full, err := analytics.MetricsFor(tradeReturns(trades))
	if err != nil {
		t.Fatal(err)
	}
	unseen, err := analytics.MetricsFor(pooledWindowReturns(trades, [][2]int{{5, 7}, {8, 12}}))
	if err != nil {
		t.Fatal(err)
	}
	if full.Trades != 3 || unseen.Trades != 2 {
		t.Fatalf("full trades = %d, unseen trades = %d", full.Trades, unseen.Trades)
	}
	if full.Expectancy.Pessimistic == unseen.Expectancy.Pessimistic {
		t.Fatalf("unseen expectancy borrowed the fitted trade: %v", unseen.Expectancy)
	}

	empty, err := analytics.MetricsFor(pooledWindowReturns(trades, [][2]int{{20, 30}}))
	if err != nil {
		t.Fatal(err)
	}
	if empty.Trades != 0 || empty.Expectancy.Pessimistic != 0 || empty.Expectancy.Optimistic != 0 {
		t.Fatalf("empty window = %#v", empty)
	}
}
