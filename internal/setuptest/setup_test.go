package setuptest

import (
	"context"
	"testing"

	"github.com/Bcypher01/Assay/internal/core"
	"github.com/Bcypher01/Assay/internal/instrument"
)

func drawing() Drawing {
	return Drawing{
		Kind: BreakRetest, Side: "long", Symbol: "DUKAS:EURUSD", Timeframe: "m1",
		AnchorTimeMS: 240_000, LevelLow: 12, LevelHigh: 12,
		Entry: 13, Stop: 8, Target: 23,
		LookbackBars: 3, RetestBars: 3, ToleranceTicks: 1, ExpiryBars: 10,
		DisplacementBodyMilli: 500, ConfirmByClose: true, RiskPerTrade: .005,
	}
}

func series() *core.Series {
	s := core.NewSeries("DUKAS:EURUSD", core.TFM1, 7)
	for i, bar := range [][4]core.Price{
		{9, 10, 8, 9}, {10, 11, 8, 9}, {9, 12, 9, 12}, {12, 13, 11, 13},
		{13, 14, 11, 13}, {13, 24, 12, 23}, {23, 24, 22, 23},
	} {
		s.Append(core.Time(i*60_000), bar[0], bar[1], bar[2], bar[3], 1)
	}
	return s
}

func TestCompileExcludesAbsoluteDrawingCoordinates(t *testing.T) {
	a := drawing()
	b := drawing()
	b.AnchorTimeMS += 60_000
	b.LevelLow, b.LevelHigh = 100, 110
	b.Entry, b.Stop, b.Target = 113, 108, 123
	left, err := Compile(a)
	if err != nil {
		t.Fatal(err)
	}
	right, err := Compile(b)
	if err != nil {
		t.Fatal(err)
	}
	if left.Hash() != right.Hash() {
		t.Fatalf("example coordinates changed executable strategy: %s != %s", left.Hash(), right.Hash())
	}
}

func TestEvaluateReportsSampleMatchAndCompiledStrategy(t *testing.T) {
	registry, err := instrument.Default()
	if err != nil {
		t.Fatal(err)
	}
	spec, err := registry.Get("DUKAS:EURUSD")
	if err != nil {
		t.Fatal(err)
	}
	preview, err := Evaluate(context.Background(), drawing(), series(), *spec, "snapshot")
	if err != nil {
		t.Fatal(err)
	}
	if !preview.SampleMatched || len(preview.Matches) == 0 || len(preview.StrategyJSON) == 0 || preview.StrategyHash == "" {
		t.Fatalf("preview = %#v", preview)
	}
	if preview.Matches[0].SignalTimeMS != drawing().AnchorTimeMS {
		t.Fatalf("signal time = %d", preview.Matches[0].SignalTimeMS)
	}
	match := preview.Matches[0]
	if match.ExitTimeMSPessimistic < match.EntryTimeMS || match.ExitPessimistic == 0 {
		t.Fatalf("exit marker data = %#v", preview.Matches[0])
	}
}

func TestEvaluateRejectsAZoneThatDoesNotMatchDetectedStructure(t *testing.T) {
	registry, err := instrument.Default()
	if err != nil {
		t.Fatal(err)
	}
	spec, err := registry.Get("DUKAS:EURUSD")
	if err != nil {
		t.Fatal(err)
	}
	d := drawing()
	d.LevelLow, d.LevelHigh = 30, 31
	preview, err := Evaluate(context.Background(), d, series(), *spec, "snapshot")
	if err != nil {
		t.Fatal(err)
	}
	if !preview.RuleMatched || preview.DrawingMatched || preview.SampleMatched {
		t.Fatalf("preview match state = rule:%v drawing:%v sample:%v", preview.RuleMatched, preview.DrawingMatched, preview.SampleMatched)
	}
}

func TestDrawingValidationRejectsInvalidTradeGeometry(t *testing.T) {
	d := drawing()
	d.Stop = d.Target
	if errs := d.Validate(); len(errs) == 0 || errs[0].Path != "stop" {
		t.Fatalf("errors = %#v", errs)
	}
}

func TestCompileVisualFVGStrategy(t *testing.T) {
	d := drawing()
	d.Kind = FairValueGapRetest
	d.MinSizeTicks = 2
	d.MitigationRule = "half_fill"
	d.RiskUnit = "percent"
	strategy, err := Compile(d)
	if err != nil {
		t.Fatal(err)
	}
	if len(strategy.Nodes) != 1 || strategy.Nodes[0].Type != "fair_value_gap_retest_bullish" {
		t.Fatalf("nodes = %#v", strategy.Nodes)
	}
	if strategy.Template.ID != "semantic-drawing/fair_value_gap_retest" {
		t.Fatalf("template = %#v", strategy.Template)
	}
	if strategy.Risk.Stop.Unit != core.UnitPercent || strategy.Risk.Target.Unit != core.UnitPercent {
		t.Fatalf("risk = %#v", strategy.Risk)
	}
}

func TestCompileDrawnPrecedentStrategy(t *testing.T) {
	d := drawing()
	d.Kind = DrawnPrecedent
	d.RiskUnit = "percent"
	d.SimilarityMilli = 850
	d.Precedent = []PrecedentBar{
		{OpenMilli: 250, HighMilli: 500, LowMilli: 0, CloseMilli: 250},
		{OpenMilli: 500, HighMilli: 750, LowMilli: 0, CloseMilli: 250},
		{OpenMilli: 250, HighMilli: 1000, LowMilli: 250, CloseMilli: 1000},
	}
	strategy, err := Compile(d)
	if err != nil {
		t.Fatal(err)
	}
	if len(strategy.Nodes) != 1 || strategy.Nodes[0].Type != "drawn_precedent_bullish" {
		t.Fatalf("nodes = %#v", strategy.Nodes)
	}
	if strategy.Template.ID != "semantic-drawing/drawn_precedent" {
		t.Fatalf("template = %#v", strategy.Template)
	}
}

func TestEvaluateDrawnPrecedentRecognizesItsOwnExample(t *testing.T) {
	registry, err := instrument.Default()
	if err != nil {
		t.Fatal(err)
	}
	spec, err := registry.Get("DUKAS:EURUSD")
	if err != nil {
		t.Fatal(err)
	}
	d := drawing()
	d.Kind = DrawnPrecedent
	d.AnchorTimeMS = 120_000
	d.SimilarityMilli = 1000
	d.Precedent = []PrecedentBar{
		{OpenMilli: 250, HighMilli: 500, LowMilli: 0, CloseMilli: 250},
		{OpenMilli: 500, HighMilli: 750, LowMilli: 0, CloseMilli: 250},
		{OpenMilli: 250, HighMilli: 1000, LowMilli: 250, CloseMilli: 1000},
	}
	preview, err := Evaluate(context.Background(), d, series(), *spec, "snapshot")
	if err != nil {
		t.Fatal(err)
	}
	if !preview.SampleMatched || !preview.RuleMatched || !preview.DrawingMatched {
		t.Fatalf("match state = rule:%v drawing:%v sample:%v", preview.RuleMatched, preview.DrawingMatched, preview.SampleMatched)
	}
	if len(preview.Matches) != 0 || preview.Metrics.Trades != 0 {
		t.Fatalf("source example counted as historical evidence: matches=%d trades=%d", len(preview.Matches), preview.Metrics.Trades)
	}
}
