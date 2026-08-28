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
