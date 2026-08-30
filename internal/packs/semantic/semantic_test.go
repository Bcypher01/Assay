package semantic

import (
	"bytes"
	"encoding/json"
	"testing"

	"github.com/Bcypher01/Assay/internal/core"
	"github.com/Bcypher01/Assay/internal/primitive"
	"google.golang.org/protobuf/types/known/structpb"
)

func testSeries() *core.Series {
	s := core.NewSeries("TEST", core.TFM1, 7)
	for i, bar := range [][4]core.Price{
		{9, 10, 8, 9},
		{10, 11, 8, 9},
		{9, 12, 9, 12},
		{12, 13, 11, 13},
		{13, 14, 10, 13},
		{13, 14, 12, 14},
		{14, 15, 13, 15},
	} {
		s.Append(core.Time(i*60_000), bar[0], bar[1], bar[2], bar[3], 1)
	}
	return s
}

func params(t *testing.T, values map[string]any) *structpb.Struct {
	t.Helper()
	p, err := structpb.NewStruct(values)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestSemanticDetectorsFindLaterRetest(t *testing.T) {
	tests := []struct {
		name      string
		primitive primitive.Primitive
		params    map[string]any
		wantBar   int
	}{
		{"break and retest", BreakRetest{side: core.SideLong}, map[string]any{"lookbackBars": 3, "retestBars": 3, "toleranceTicks": 1, "expiryBars": 10, "displacementBodyMilli": 500, "confirmByClose": true}, 4},
		{"order block retest", OrderBlockRetest{side: core.SideLong}, map[string]any{"lookbackBars": 3, "retestBars": 3, "toleranceTicks": 0, "expiryBars": 10, "displacementBodyMilli": 500}, 3},
		{"breaker retest", BreakerRetest{side: core.SideLong}, map[string]any{"lookbackBars": 2, "retestBars": 3, "toleranceTicks": 0, "expiryBars": 10, "displacementBodyMilli": 500}, 3},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			instance, err := test.primitive.New(params(t, test.params), core.TFM1)
			if err != nil {
				t.Fatal(err)
			}
			value, err := instance.Eval(&primitive.EvalCtx{Snapshot: "fixed"}, testSeries())
			if err != nil {
				t.Fatal(err)
			}
			if !value.Bool.Get(test.wantBar) {
				t.Fatalf("signal missing at bar %d", test.wantBar)
			}
			first, _ := json.Marshal(value)
			instance, err = test.primitive.New(params(t, test.params), core.TFM1)
			if err != nil {
				t.Fatal(err)
			}
			value, err = instance.Eval(&primitive.EvalCtx{Snapshot: "fixed"}, testSeries())
			if err != nil {
				t.Fatal(err)
			}
			second, _ := json.Marshal(value)
			if !bytes.Equal(first, second) {
				t.Fatal("detector output is not deterministic")
			}
		})
	}
}

func TestBreakRetestDoesNotSignalOnBreakBar(t *testing.T) {
	instance, err := (BreakRetest{side: core.SideLong}).New(params(t, map[string]any{"lookbackBars": 3, "retestBars": 3, "expiryBars": 10, "displacementBodyMilli": 500}), core.TFM1)
	if err != nil {
		t.Fatal(err)
	}
	value, err := instance.Eval(&primitive.EvalCtx{}, testSeries())
	if err != nil {
		t.Fatal(err)
	}
	if value.Bool.Get(3) {
		t.Fatal("break bar was incorrectly treated as its own retest")
	}
}

func TestFairValueGapDrawingCompilesToLaterRetest(t *testing.T) {
	s := core.NewSeries("TEST", core.TFM1, 6)
	for i, bar := range [][4]core.Price{
		{9, 10, 8, 9},
		{10, 12, 9, 11},
		{13, 15, 13, 14}, // bullish FVG from 10 to 13
		{14, 15, 12, 14}, // first return into the gap
		{14, 18, 14, 17},
		{17, 19, 16, 18},
	} {
		s.Append(core.Time(i*60_000), bar[0], bar[1], bar[2], bar[3], 1)
	}
	instance, err := (FairValueGapRetest{side: core.SideLong}).New(params(t, map[string]any{
		"minSizeTicks": 2, "expiryBars": 10, "mitigationRule": "first_touch",
	}), core.TFM1)
	if err != nil {
		t.Fatal(err)
	}
	value, err := instance.Eval(&primitive.EvalCtx{}, s)
	if err != nil {
		t.Fatal(err)
	}
	if !value.Bool.Get(3) || value.Bool.Get(2) {
		t.Fatalf("signals = %v, want only a later retest at bar 3", value.Bool)
	}
}

func TestDrawnPrecedentMatchesItsNormalizedExample(t *testing.T) {
	pattern := []any{
		map[string]any{"openMilli": 250, "highMilli": 500, "lowMilli": 0, "closeMilli": 250},
		map[string]any{"openMilli": 500, "highMilli": 750, "lowMilli": 0, "closeMilli": 250},
		map[string]any{"openMilli": 250, "highMilli": 1000, "lowMilli": 250, "closeMilli": 1000},
	}
	instance, err := (DrawnPrecedent{side: core.SideLong}).New(params(t, map[string]any{
		"precedent": pattern, "similarityMilli": 1000,
	}), core.TFM1)
	if err != nil {
		t.Fatal(err)
	}
	value, err := instance.Eval(&primitive.EvalCtx{}, testSeries())
	if err != nil {
		t.Fatal(err)
	}
	if !value.Bool.Get(2) {
		t.Fatal("the exact drawn precedent did not match its own final candle")
	}
}

func TestPrecedentScoreAcceptsCloseStructuralMatch(t *testing.T) {
	expected := []PrecedentCandle{
		{Open: 120, High: 220, Low: 80, Close: 190},
		{Open: 190, High: 360, Low: 170, Close: 330},
		{Open: 330, High: 390, Low: 250, Close: 280},
		{Open: 280, High: 610, Low: 270, Close: 570},
		{Open: 570, High: 760, Low: 530, Close: 720},
		{Open: 720, High: 930, Low: 690, Close: 900},
	}
	candidate := []PrecedentCandle{
		{Open: 125, High: 225, Low: 75, Close: 195},
		{Open: 195, High: 355, Low: 175, Close: 325},
		{Open: 325, High: 395, Low: 245, Close: 275},
		{Open: 275, High: 605, Low: 265, Close: 565},
		{Open: 565, High: 765, Low: 525, Close: 715},
		{Open: 715, High: 935, Low: 685, Close: 895},
	}
	score := ScorePrecedent(expected, candidate, 900)
	if !score.Accepted || score.Overall < 900 {
		t.Fatalf("close structural match rejected: %#v", score)
	}
}

func TestPrecedentScoreRejectsOppositeAndShuffledStructures(t *testing.T) {
	expected := []PrecedentCandle{
		{Open: 100, High: 230, Low: 70, Close: 210},
		{Open: 210, High: 390, Low: 190, Close: 360},
		{Open: 360, High: 410, Low: 260, Close: 290},
		{Open: 290, High: 650, Low: 280, Close: 610},
		{Open: 610, High: 790, Low: 580, Close: 750},
		{Open: 750, High: 950, Low: 720, Close: 920},
	}
	opposite := []PrecedentCandle{
		{Open: 900, High: 930, Low: 770, Close: 790},
		{Open: 790, High: 810, Low: 610, Close: 640},
		{Open: 640, High: 740, Low: 590, Close: 710},
		{Open: 710, High: 720, Low: 350, Close: 390},
		{Open: 390, High: 420, Low: 210, Close: 250},
		{Open: 250, High: 280, Low: 50, Close: 80},
	}
	shuffled := []PrecedentCandle{expected[0], expected[3], expected[1], expected[5], expected[2], expected[4]}
	if score := ScorePrecedent(expected, opposite, 900); score.Accepted {
		t.Fatalf("opposite structure accepted: %#v", score)
	}
	if score := ScorePrecedent(expected, shuffled, 900); score.Accepted {
		t.Fatalf("shuffled structure accepted: %#v", score)
	}
}

func TestPrecedentScoreRejectsSingleSevereCandleMismatch(t *testing.T) {
	expected := []PrecedentCandle{
		{Open: 100, High: 220, Low: 80, Close: 200},
		{Open: 200, High: 350, Low: 180, Close: 330},
		{Open: 330, High: 480, Low: 310, Close: 460},
		{Open: 460, High: 620, Low: 440, Close: 600},
		{Open: 600, High: 780, Low: 580, Close: 760},
		{Open: 760, High: 940, Low: 740, Close: 920},
	}
	candidate := append([]PrecedentCandle(nil), expected...)
	candidate[2] = PrecedentCandle{Open: 460, High: 480, Low: 310, Close: 330}
	if score := ScorePrecedent(expected, candidate, 900); score.Accepted {
		t.Fatalf("severe candle mismatch accepted: %#v", score)
	}
}
