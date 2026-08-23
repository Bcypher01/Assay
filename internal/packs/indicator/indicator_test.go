package indicator

import (
	"context"
	"testing"

	"github.com/Bcypher01/Assay/internal/core"
	"github.com/Bcypher01/Assay/internal/primitive"
	assayv1 "github.com/Bcypher01/Assay/proto/assay/v1"
)

func testSeries() *core.Series {
	s := core.NewSeries("X", core.TFM1, 5)
	for i, p := range []core.Price{1, 2, 3, 4, 5} {
		s.Append(core.Time(int64(i)*60_000), p, p+1, p-1, p, 0)
	}
	return s
}

func TestSMAWarmupAndValues(t *testing.T) {
	p, err := primitive.DefaultRegistry.Get("indicator", "sma")
	if err != nil {
		t.Fatal(err)
	}
	v, _, err := primitive.Evaluate(context.Background(), p, &assayv1.MovingAverageParams{Period: 3}, core.TFM1, testSeries(), "fixture", nil)
	if err != nil {
		t.Fatal(err)
	}
	if v.Valid.Get(1) || !v.Valid.Get(2) || v.Level[2] != 2 || v.Level[4] != 4 {
		t.Fatalf("unexpected SMA: %#v", v)
	}
}
func TestRSIAndATRWarmup(t *testing.T) {
	p, _ := primitive.DefaultRegistry.Get("indicator", "rsi")
	v, _, err := primitive.Evaluate(context.Background(), p, &assayv1.RsiParams{Period: 2}, core.TFM1, testSeries(), "fixture", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !v.Valid.Get(2) || v.Number[2] != core.RatioFromInt(100) {
		t.Fatalf("RSI=%#v", v)
	}
	p, _ = primitive.DefaultRegistry.Get("indicator", "atr")
	v, _, err = primitive.Evaluate(context.Background(), p, &assayv1.AtrParams{Period: 2}, core.TFM1, testSeries(), "fixture", nil)
	if err != nil || !v.Valid.Get(2) {
		t.Fatalf("ATR error=%v value=%#v", err, v)
	}
}
