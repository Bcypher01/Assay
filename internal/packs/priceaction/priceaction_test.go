package priceaction

import (
	"context"
	"testing"

	"github.com/Bcypher01/Assay/internal/core"
	"github.com/Bcypher01/Assay/internal/primitive"
	assayv1 "github.com/Bcypher01/Assay/proto/assay/v1"
)

func fixture() *core.Series {
	s := core.NewSeries("X", core.TFM1, 5)
	for i, p := range []core.Price{2, 4, 1, 4, 2} {
		s.Append(core.Time(i*60_000), p, p+1, p-1, p, 1)
	}
	return s
}

func TestSwingIsOnlyValidAfterBothSidesAreKnown(t *testing.T) {
	p, err := primitive.DefaultRegistry.Get("priceaction", "swing_high")
	if err != nil {
		t.Fatal(err)
	}
	v, _, err := primitive.Evaluate(context.Background(), p, &assayv1.SwingParams{Left: 1, Right: 1}, core.TFM1, fixture(), "fixture", nil)
	if err != nil {
		t.Fatal(err)
	}
	if v.Valid.Get(0) || !v.Valid.Get(1) || !v.Bool.Get(1) || v.Valid.Get(4) {
		t.Fatalf("unexpected swing output: %#v", v)
	}
}

func TestEngulfingDirectionalVariants(t *testing.T) {
	s := core.NewSeries("X", core.TFM1, 5)
	for i, bar := range []struct{ open, close core.Price }{
		{10, 9},  // first candle has no predecessor
		{8, 11},  // bullish engulfing
		{11, 12}, // bullish predecessor
		{13, 10}, // bearish engulfing
		{10, 10}, // neither
	} {
		high, low := core.Max(bar.open, bar.close)+1, core.Min(bar.open, bar.close)-1
		s.Append(core.Time(int64(i)*core.TFM1.Millis()), bar.open, high, low, bar.close, 0)
	}
	cases := []struct {
		typ  string
		bars []int
	}{
		{typ: "engulfing", bars: []int{1, 3}},
		{typ: "engulfing_bullish", bars: []int{1}},
		{typ: "engulfing_bearish", bars: []int{3}},
	}
	for _, tc := range cases {
		t.Run(tc.typ, func(t *testing.T) {
			p, err := primitive.DefaultRegistry.Get("priceaction", tc.typ)
			if err != nil {
				t.Fatal(err)
			}
			v, _, err := primitive.Evaluate(context.Background(), p, &assayv1.SampleParams{}, core.TFM1, s, "fixture", nil)
			if err != nil {
				t.Fatal(err)
			}
			for i := 0; i < s.Len(); i++ {
				want := false
				for _, bar := range tc.bars {
					want = want || i == bar
				}
				if got := v.Bool.Get(i); got != want {
					t.Fatalf("bar %d = %t, want %t", i, got, want)
				}
			}
		})
	}
}
