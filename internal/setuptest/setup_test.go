package setuptest

import (
	"context"
	"testing"

	"github.com/bcypher01/assay/internal/core"
	"github.com/bcypher01/assay/internal/instrument"
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

func TestCompileLowerTimeframeEntry(t *testing.T) {
	d := drawing()
	d.Timeframe = "h1"
	d.EntryTimeframe = "m15"
	strategy, err := Compile(d)
	if err != nil {
		t.Fatal(err)
	}
	if strategy.Timeframes["bias"] != core.TFH1 || strategy.Timeframes["poi"] != core.TFH1 || strategy.Timeframes["entry"] != core.TFM15 {
		t.Fatalf("timeframes = %#v", strategy.Timeframes)
	}
	if len(strategy.Nodes) != 1 || strategy.Nodes[0].Timeframe != core.TFH1 {
		t.Fatalf("pattern node must stay on the drawing timeframe: %#v", strategy.Nodes)
	}
}

func TestEmptyEntryTimeframeMatchesExplicitSame(t *testing.T) {
	a := drawing()
	b := drawing()
	b.EntryTimeframe = b.Timeframe
	left, err := Compile(a)
	if err != nil {
		t.Fatal(err)
	}
	right, err := Compile(b)
	if err != nil {
		t.Fatal(err)
	}
	if left.Hash() != right.Hash() {
		t.Fatalf("empty and explicit-same entry timeframe changed identity: %s != %s", left.Hash(), right.Hash())
	}
}

func TestValidateRejectsEntryTimeframeCoarserThanPattern(t *testing.T) {
	d := drawing()
	d.EntryTimeframe = "m5" // coarser than the m1 pattern timeframe
	found := false
	for _, e := range d.Validate() {
		if e.Path == "entryTimeframe" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected an entryTimeframe error, got %#v", d.Validate())
	}
}

// ladderDrawing is a long top-down setup: an H1 point of interest (9..13)
// marked on the hourly chart, entered on M15 when a bullish shift candle
// fires inside the zone.
func ladderDrawing() Drawing {
	return Drawing{
		Kind: POIShiftEntry, Side: "long", Symbol: "DUKAS:EURUSD",
		Timeframe: "h1", EntryTimeframe: "m15",
		AnchorTimeMS: 7_200_000, LevelLow: 9, LevelHigh: 13,
		Entry: 13, Stop: 11, Target: 17,
		LookbackBars: 3, RetestBars: 3, ToleranceTicks: 0, ExpiryBars: 50,
		DisplacementBodyMilli: 600, ConfirmByClose: true, RiskPerTrade: .005,
	}
}

// ladderBase is an M15 series whose H1 aggregates are exactly ladderDetection:
// hour 0 forms the bearish zone candle, hour 1 displaces away from it, hour 2
// returns into the zone and closes on a bullish shift candle.
func ladderBase() *core.Series {
	s := core.NewSeries("DUKAS:EURUSD", core.TFM15, 8)
	for i, bar := range [][4]core.Price{
		{12, 13, 11, 11}, {11, 12, 10, 10}, {10, 11, 9, 9}, {9, 11, 9, 10},
		{10, 11, 10, 11}, {11, 13, 11, 13}, {13, 15, 13, 15}, {15, 15, 14, 15},
		{15, 16, 14, 14}, {14, 15, 12, 12}, {12, 14, 12, 14}, {13, 17, 13, 17},
		{13, 16, 12, 15}, {15, 18, 14, 17},
	} {
		s.Append(core.Time(i*900_000), bar[0], bar[1], bar[2], bar[3], 1)
	}
	return s
}

// ladderDetection is the H1 chart the drawing was made on.
func ladderDetection() *core.Series {
	s := core.NewSeries("DUKAS:EURUSD", core.TFH1, 8)
	for i, bar := range [][4]core.Price{
		{12, 13, 9, 10}, {10, 15, 10, 15}, {15, 17, 12, 17}, {17, 18, 16, 17},
	} {
		s.Append(core.Time(i*3_600_000), bar[0], bar[1], bar[2], bar[3], 1)
	}
	return s
}

func TestCompilePOIShiftBuildsATwoNodeLadder(t *testing.T) {
	d := ladderDrawing()
	strategy, err := Compile(d)
	if err != nil {
		t.Fatal(err)
	}
	if strategy.Timeframes["bias"] != core.TFH1 || strategy.Timeframes["poi"] != core.TFH1 || strategy.Timeframes["entry"] != core.TFM15 {
		t.Fatalf("timeframes = %#v", strategy.Timeframes)
	}
	if len(strategy.Nodes) != 2 {
		t.Fatalf("nodes = %#v", strategy.Nodes)
	}
	zone, shift := strategy.Nodes[0], strategy.Nodes[1]
	if zone.Type != "poi_zone_bullish" || zone.Timeframe != core.TFH1 {
		t.Fatalf("zone node = %#v", zone)
	}
	if shift.Type != "shift_candle_bullish" || shift.Timeframe != core.TFM15 {
		t.Fatalf("shift node = %#v", shift)
	}
	when := strategy.Entries.Long.When
	if len(when.All) != 2 || when.All[0].Node != "poi" || when.All[1].Node != "shift" {
		t.Fatalf("entry condition = %#v", when)
	}
	if strategy.Template.ID != "semantic-drawing/"+string(POIShiftEntry) {
		t.Fatalf("template = %#v", strategy.Template)
	}
}

func TestCompilePOIShiftExcludesAbsoluteDrawingCoordinates(t *testing.T) {
	a := ladderDrawing()
	b := ladderDrawing()
	b.AnchorTimeMS += 900_000
	b.LevelLow, b.LevelHigh = 109, 113
	b.Entry, b.Stop, b.Target = 113, 111, 117
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

func TestValidateRejectsPOIShiftWithoutAFinerEntry(t *testing.T) {
	d := ladderDrawing()
	d.EntryTimeframe = ""
	if errs := d.Validate(); len(errs) == 0 || errs[0].Path != "entryTimeframe" {
		t.Fatalf("errors = %#v", errs)
	}
}

func TestEvaluateMultiTimeframePOIShiftMatchesItsOwnExample(t *testing.T) {
	registry, err := instrument.Default()
	if err != nil {
		t.Fatal(err)
	}
	spec, err := registry.Get("DUKAS:EURUSD")
	if err != nil {
		t.Fatal(err)
	}
	preview, err := EvaluateMultiTimeframe(context.Background(), ladderDrawing(), ladderBase(), ladderDetection(), *spec, "snapshot", "snapshot")
	if err != nil {
		t.Fatal(err)
	}
	if !preview.SampleMatched || !preview.RuleMatched || !preview.DrawingMatched {
		t.Fatalf("match state = rule:%v drawing:%v sample:%v warnings:%v", preview.RuleMatched, preview.DrawingMatched, preview.SampleMatched, preview.Warnings)
	}
	if len(preview.Matches) != 1 {
		t.Fatalf("matches = %#v", preview.Matches)
	}
	match := preview.Matches[0]
	if match.SignalTimeMS != ladderDrawing().AnchorTimeMS {
		t.Fatalf("signal time = %d", match.SignalTimeMS)
	}
	if match.LevelLow != 9 || match.LevelHigh != 13 {
		t.Fatalf("match zone = %d..%d, want the drawn 9..13", match.LevelLow, match.LevelHigh)
	}
	if match.EntryTimeMS != 10_800_000 {
		t.Fatalf("entry time = %d, want the bar after the M15 shift", match.EntryTimeMS)
	}
}

func TestEvaluateMultiTimeframePOIShiftNeedsTheZone(t *testing.T) {
	registry, err := instrument.Default()
	if err != nil {
		t.Fatal(err)
	}
	spec, err := registry.Get("DUKAS:EURUSD")
	if err != nil {
		t.Fatal(err)
	}
	// The same shift candle away from any zone must not trade: without the
	// higher-timeframe condition this is a naked displacement strategy.
	base := ladderBase()
	for i := 4; i < base.Len(); i++ {
		base.High[i] += 20
		base.Low[i] += 20
		base.Open[i] += 20
		base.Close[i] += 20
	}
	preview, err := EvaluateMultiTimeframe(context.Background(), ladderDrawing(), base, ladderDetection(), *spec, "snapshot", "snapshot")
	if err != nil {
		t.Fatal(err)
	}
	if preview.RuleMatched || len(preview.Matches) != 0 {
		t.Fatalf("shift away from the zone traded: %#v", preview)
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
