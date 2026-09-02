package smc

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	assayv1 "github.com/bcypher01/assay/gen/go/assay/v1"
	"github.com/bcypher01/assay/internal/core"
	"github.com/bcypher01/assay/internal/primitive"
)

func TestFairValueGapMitigationAndExpiry(t *testing.T) {
	s := series([][4]core.Price{{10, 10, 8, 9}, {9, 12, 9, 11}, {12, 15, 13, 14}, {14, 15, 12, 13}})
	p := FairValueGap{}
	inst, err := p.New(&assayv1.FairValueGapParams{MinSizeTicks: 2, ExpiryBars: 3, MitigationRule: assayv1.MitigationRule_MITIGATION_RULE_FIRST_TOUCH}, core.TFM1)
	if err != nil {
		t.Fatal(err)
	}
	v, err := inst.Eval(&primitive.EvalCtx{}, s)
	if err != nil {
		t.Fatal(err)
	}
	if !v.Valid.Get(2) || v.Zone[2].Low != 10 || v.Zone[2].High != 13 {
		t.Fatalf("unexpected FVG: %#v", v.Zone[2])
	}
	if !v.Valid.Get(3) || !v.Zone[3].Mitigated || v.Zone[3].MitigatedAt != s.Time[3] {
		t.Fatal("mitigation transition was not reported")
	}

	s = series([][4]core.Price{{10, 10, 8, 9}, {9, 12, 9, 11}, {12, 15, 13, 14}, {14, 16, 11, 15}, {15, 16, 11, 14}, {14, 16, 11, 15}})
	inst, err = p.New(&assayv1.FairValueGapParams{MinSizeTicks: 2, ExpiryBars: 2, MitigationRule: assayv1.MitigationRule_MITIGATION_RULE_FULL_FILL}, core.TFM1)
	if err != nil {
		t.Fatal(err)
	}
	v, err = inst.Eval(&primitive.EvalCtx{}, s)
	if err != nil {
		t.Fatal(err)
	}
	if !v.Valid.Get(4) || v.Valid.Get(5) {
		t.Fatalf("expiry validity at 4/5 = %v/%v", v.Valid.Get(4), v.Valid.Get(5))
	}
}

func TestDisplacementRequiresConsecutiveGapBars(t *testing.T) {
	s := series([][4]core.Price{{10, 11, 9, 10}, {12, 16, 12, 16}, {17, 21, 17, 21}, {21, 22, 20, 21}})
	inst, err := Displacement{}.New(&assayv1.DisplacementParams{MinBodyRatioMilli: 900, MinConsecutiveBars: 2, RequireGap: true}, core.TFM1)
	if err != nil {
		t.Fatal(err)
	}
	v, err := inst.Eval(&primitive.EvalCtx{}, s)
	if err != nil {
		t.Fatal(err)
	}
	if v.Bool.Get(1) || !v.Bool.Get(2) {
		t.Fatalf("displacement bits %v/%v", v.Bool.Get(1), v.Bool.Get(2))
	}
}

func TestLiquiditySweepConfirmsOnKnownClose(t *testing.T) {
	s := series([][4]core.Price{{10, 12, 8, 10}, {10, 15, 10, 14}, {14, 14, 9, 11}})
	inst, err := LiquiditySweep{}.New(&assayv1.SweepParams{WickThroughTicks: 2, CloseBackBars: 2}, core.TFM1)
	if err != nil {
		t.Fatal(err)
	}
	v, err := inst.Eval(&primitive.EvalCtx{}, s)
	if err != nil {
		t.Fatal(err)
	}
	if v.Bool.Get(1) || !v.Bool.Get(2) {
		t.Fatalf("sweep emitted at %v/%v; must emit only at confirmation", v.Bool.Get(1), v.Bool.Get(2))
	}
}

func TestDirectionalLiquiditySweepFiltersByReversalSide(t *testing.T) {
	params := &assayv1.SweepParams{WickThroughTicks: 2, CloseBackBars: 2}
	bearish, err := LiquiditySweepBearish{}.New(params, core.TFM1)
	if err != nil {
		t.Fatal(err)
	}
	v, err := bearish.Eval(&primitive.EvalCtx{}, series([][4]core.Price{{10, 12, 8, 10}, {10, 15, 10, 14}, {14, 14, 9, 11}}))
	if err != nil || !v.Bool.Get(2) {
		t.Fatalf("bearish sweep = %#v, %v", v.Bool, err)
	}

	bullish, err := LiquiditySweepBullish{}.New(params, core.TFM1)
	if err != nil {
		t.Fatal(err)
	}
	v, err = bullish.Eval(&primitive.EvalCtx{}, series([][4]core.Price{{10, 12, 8, 10}, {10, 10, 5, 6}, {6, 11, 6, 9}}))
	if err != nil || !v.Bool.Get(2) {
		t.Fatalf("bullish sweep = %#v, %v", v.Bool, err)
	}
}

func TestDirectionalMarketStructureFiltersByBreakSide(t *testing.T) {
	params := &assayv1.MarketStructureParams{SwingLookback: 1}
	bullish, err := MarketStructureBullish{}.New(params, core.TFM1)
	if err != nil {
		t.Fatal(err)
	}
	v, err := bullish.Eval(&primitive.EvalCtx{}, series([][4]core.Price{{9, 10, 8, 9}, {10, 12, 9, 11}, {11, 11, 9, 10}, {12, 13, 10, 12}}))
	if err != nil || !v.Bool.Get(3) {
		t.Fatalf("bullish structure = %#v, %v", v.Bool, err)
	}

	bearish, err := MarketStructureBearish{}.New(params, core.TFM1)
	if err != nil {
		t.Fatal(err)
	}
	v, err = bearish.Eval(&primitive.EvalCtx{}, series([][4]core.Price{{9, 10, 8, 9}, {8, 9, 6, 7}, {7, 9, 7, 8}, {6, 7, 5, 6}}))
	if err != nil || !v.Bool.Get(3) {
		t.Fatalf("bearish structure = %#v, %v", v.Bool, err)
	}
}

func TestPackRegistersAllImplementedPrimitives(t *testing.T) {
	for _, name := range []string{"market_structure", "market_structure_bullish", "market_structure_bearish", "displacement", "fair_value_gap", "order_block", "breaker", "liquidity_pool", "liquidity_sweep", "liquidity_sweep_bullish", "liquidity_sweep_bearish", "premium_discount"} {
		if _, err := primitive.DefaultRegistry.Get("smc", name); err != nil {
			t.Errorf("%s not registered: %v", name, err)
		}
	}
}

func TestBreakerUsesSafeDefaultForLegacyEmptyParameters(t *testing.T) {
	params := &assayv1.BreakerParams{}
	if _, err := (Breaker{}).New(params, core.TFM1); err != nil {
		t.Fatalf("empty legacy parameters should receive defaults: %v", err)
	}
	if params.InvalidatingTouches != 1 {
		t.Fatalf("default touches = %d, want 1", params.InvalidatingTouches)
	}
}

func TestFairValueGapIsDeterministic(t *testing.T) {
	s := series([][4]core.Price{{10, 10, 8, 9}, {9, 12, 9, 11}, {12, 15, 13, 14}})
	p := FairValueGap{}
	params := &assayv1.FairValueGapParams{MinSizeTicks: 2, ExpiryBars: 2, MitigationRule: assayv1.MitigationRule_MITIGATION_RULE_FULL_FILL}
	a, _, err := primitive.Evaluate(context.Background(), p, params, core.TFM1, s, "snapshot", nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := primitive.Evaluate(context.Background(), p, params, core.TFM1, s, "snapshot", nil)
	if err != nil {
		t.Fatal(err)
	}
	if a.Zone[2] != b.Zone[2] {
		t.Fatal("same input produced different zone")
	}
}

func TestGoldenScoreSweepAndDisagreement(t *testing.T) {
	f := GoldenFixture{Version: 1, WindowID: "pilot-001", Annotator: "annotator-a", Instrument: "X", Timeframe: core.TFM1, Primitive: "fair_value_gap", Params: json.RawMessage(`{"minSizeTicks":"2","mitigationRule":"MITIGATION_RULE_FULL_FILL","expiryBars":"2"}`), Bars: []GoldenBar{{Time: 0, Open: 10, High: 10, Low: 8, Close: 9}, {Time: 60_000, Open: 9, High: 12, Low: 9, Close: 11}, {Time: 120_000, Open: 13, High: 15, Low: 13, Close: 14}}, Objects: []AnnotatedObject{{Type: "zone", Bar: 2, Low: 10, High: 13, Side: core.SideLong}}}
	r, err := Score(context.Background(), f, 0)
	if err != nil {
		t.Fatal(err)
	}
	if r.Agreement.F1 != 1 {
		t.Fatalf("F1 = %v", r.Agreement.F1)
	}
	sweep, err := Sweep(context.Background(), f, []SweepCandidate{{Name: "default", Params: f.Params}, {Name: "too_large", Params: json.RawMessage(`{"minSizeTicks":"9","mitigationRule":"MITIGATION_RULE_FULL_FILL","expiryBars":"2"}`)}}, 0)
	if err != nil || sweep[0].Report.Agreement.F1 != 1 || sweep[1].Report.Agreement.F1 != 0 {
		t.Fatalf("sweep=%#v err=%v", sweep, err)
	}
	other := f
	other.Annotator = "annotator-b"
	other.Objects = nil
	d := Disagreements([]GoldenFixture{f, other}, 0)
	if len(d) == 0 || len(d[0].MissingFrom) != 1 || d[0].MissingFrom[0] != "annotator-b" {
		t.Fatalf("disagreements=%#v", d)
	}
}

func TestGoldenFixtureTemplateLoads(t *testing.T) {
	f, err := LoadGoldenFixture("testdata/golden/pilot-fvg.json")
	if err != nil {
		t.Fatal(err)
	}
	if f.WindowID != "pilot-001" || len(f.Objects) != 1 {
		t.Fatalf("fixture = %#v", f)
	}
}

func TestGoldenPilotLoadsAndSummarizesDeterministically(t *testing.T) {
	dir := t.TempDir()
	fixture, err := os.ReadFile("testdata/golden/pilot-fvg.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "z.json"), fixture, 0o600); err != nil {
		t.Fatal(err)
	}
	second := string(fixture)
	second = replaceOnce(second, "example-annotator", "annotator-b")
	if err := os.WriteFile(filepath.Join(dir, "a.json"), []byte(second), 0o600); err != nil {
		t.Fatal(err)
	}
	fixtures, err := LoadGoldenFixtures(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(fixtures) != 2 || fixtures[0].Annotator != "annotator-b" {
		t.Fatalf("fixtures = %#v", fixtures)
	}
	reports, summaries, err := ScoreAll(context.Background(), fixtures, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(reports) != 2 || len(summaries) != 2 {
		t.Fatalf("reports=%#v summaries=%#v", reports, summaries)
	}
	for _, summary := range summaries {
		if summary.Windows != 1 || summary.Agreement.F1 != 1 {
			t.Fatalf("summary = %#v", summary)
		}
	}
	reversed := []GoldenFixture{fixtures[1], fixtures[0]}
	againReports, againSummaries, err := ScoreAll(context.Background(), reversed, 0)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(reports, againReports) || !reflect.DeepEqual(summaries, againSummaries) {
		t.Fatal("pilot report changed with fixture order")
	}
}

func TestGoldenPilotRejectsDuplicateSubmission(t *testing.T) {
	dir := t.TempDir()
	fixture, err := os.ReadFile("testdata/golden/pilot-fvg.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"one.json", "two.json"} {
		if err := os.WriteFile(filepath.Join(dir, name), fixture, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := LoadGoldenFixtures(dir); err == nil {
		t.Fatal("LoadGoldenFixtures accepted duplicate submission")
	}
}

func series(bars [][4]core.Price) *core.Series {
	s := core.NewSeries("X", core.TFM1, len(bars))
	for i, b := range bars {
		s.Append(core.Time(i*60_000), b[0], b[1], b[2], b[3], 1)
	}
	return s
}

func replaceOnce(s, old, new string) string {
	for i := 0; i+len(old) <= len(s); i++ {
		if s[i:i+len(old)] == old {
			return s[:i] + new + s[i+len(old):]
		}
	}
	return s
}
