package semantic

import (
	"bytes"
	"encoding/json"
	"testing"

	"google.golang.org/protobuf/types/known/structpb"

	"github.com/bcypher01/assay/internal/core"
	"github.com/bcypher01/assay/internal/primitive"
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

// topDownSeries builds the canonical ladder shape: a bearish candle (the
// future zone), a bullish displacement away from it, then a multi-bar return
// into that zone where the entry-timeframe shift must be looked for.
func topDownSeries() *core.Series {
	s := core.NewSeries("TEST", core.TFM1, 8)
	for i, bar := range [][4]core.Price{
		{12, 13, 9, 10},  // bearish, body 2 of range 4: becomes POI zone 9..13
		{11, 16, 11, 15}, // bullish displacement, body 4 of range 5, activates the zone
		{15, 16, 14, 15},
		{15, 16, 13, 14}, // bearish touch of the zone from above; future zone 13..16
		{14, 15, 12, 14}, // doji still inside the zone
		{13, 18, 13, 17}, // bullish shift inside the zone; displacement replaces it
		{18, 19, 17, 18},
		{18, 20, 17, 19},
	} {
		s.Append(core.Time(i*60_000), bar[0], bar[1], bar[2], bar[3], 1)
	}
	return s
}

func topDownParams() map[string]any {
	return map[string]any{"lookbackBars": 3, "toleranceTicks": 0, "expiryBars": 10, "displacementBodyMilli": 500, "useBody": false}
}

func TestPOIZoneHoldsStateAcrossEveryTouchingBar(t *testing.T) {
	instance, err := (POIZone{side: core.SideLong}).New(params(t, topDownParams()), core.TFM1)
	if err != nil {
		t.Fatal(err)
	}
	value, err := instance.Eval(&primitive.EvalCtx{Snapshot: "fixed"}, topDownSeries())
	if err != nil {
		t.Fatal(err)
	}
	// The zone is 9..13, created when bar 1 displaces. Bars 3, 4 and 5 all
	// reach into it; unlike OrderBlockRetest none of them consumes the zone.
	for _, bar := range []int{3, 4, 5} {
		if !value.Bool.Get(bar) {
			t.Fatalf("zone state missing at bar %d", bar)
		}
	}
	if value.Bool.Get(1) || value.Bool.Get(2) {
		t.Fatal("zone reported touched before price returned to it")
	}
	// Bar 5's displacement replaces the zone with 13..16 from bar 3; bar 6
	// stays above both and must read false.
	if value.Bool.Get(6) || value.Bool.Get(7) {
		t.Fatal("zone reported touched after price left it")
	}
}

func TestPOIZoneExpiresAndIsReplacedByNewerDisplacement(t *testing.T) {
	short := topDownParams()
	short["expiryBars"] = 2 // zone born at bar 1 expires after bar 3
	instance, err := (POIZone{side: core.SideLong}).New(params(t, short), core.TFM1)
	if err != nil {
		t.Fatal(err)
	}
	value, err := instance.Eval(&primitive.EvalCtx{}, topDownSeries())
	if err != nil {
		t.Fatal(err)
	}
	if value.Bool.Get(4) || value.Bool.Get(5) {
		t.Fatal("expired zone still reported as active")
	}

	// The bar-5 displacement must replace the exhausted zone with the last
	// bearish candle inside its lookback: bar 3, giving 13..16. Bar 6 returns
	// to touch that replacement zone exactly.
	s := topDownSeries()
	s.Low[6], s.Open[6] = 16, 16
	replacement, err := (POIZone{side: core.SideLong}).New(params(t, topDownParams()), core.TFM1)
	if err != nil {
		t.Fatal(err)
	}
	value, err = replacement.Eval(&primitive.EvalCtx{}, s)
	if err != nil {
		t.Fatal(err)
	}
	if !value.Bool.Get(6) {
		t.Fatal("replacement zone 13..16 not active where bar 6 touches it")
	}
}

func TestShiftCandleFiresOnlyOnDirectionalDisplacement(t *testing.T) {
	instance, err := (ShiftCandle{side: core.SideLong}).New(params(t, topDownParams()), core.TFM1)
	if err != nil {
		t.Fatal(err)
	}
	value, err := instance.Eval(&primitive.EvalCtx{Snapshot: "fixed"}, topDownSeries())
	if err != nil {
		t.Fatal(err)
	}
	// Bars 1 and 5 are the only bullish candles with a body of at least half
	// their range; bar 4 is a doji and must not qualify.
	if !value.Bool.Get(1) || !value.Bool.Get(5) {
		t.Fatal("directional displacement candles missing from the shift stream")
	}
	if value.Bool.Get(4) {
		t.Fatal("small-bodied candle accepted as a shift")
	}
	bear, err := (ShiftCandle{side: core.SideShort}).New(params(t, topDownParams()), core.TFM1)
	if err != nil {
		t.Fatal(err)
	}
	value, err = bear.Eval(&primitive.EvalCtx{}, topDownSeries())
	if err != nil {
		t.Fatal(err)
	}
	if !value.Bool.Get(0) || value.Bool.Get(1) {
		t.Fatal("bearish shift stream must fire on the bearish candle only")
	}
}

func TestTopDownDetectorsAreDeterministic(t *testing.T) {
	for _, test := range []struct {
		name      string
		primitive primitive.Primitive
	}{
		{"poi zone", POIZone{side: core.SideLong}},
		{"shift candle", ShiftCandle{side: core.SideLong}},
	} {
		t.Run(test.name, func(t *testing.T) {
			run := func() []byte {
				instance, err := test.primitive.New(params(t, topDownParams()), core.TFM1)
				if err != nil {
					t.Fatal(err)
				}
				value, err := instance.Eval(&primitive.EvalCtx{Snapshot: "fixed"}, topDownSeries())
				if err != nil {
					t.Fatal(err)
				}
				out, err := json.Marshal(value)
				if err != nil {
					t.Fatal(err)
				}
				return out
			}
			first, second := run(), run()
			if !bytes.Equal(first, second) {
				t.Fatal("detector output is not deterministic")
			}
		})
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
