package analytics

import (
	"context"
	"reflect"
	"testing"
)

func TestWalkForwardDoesNotOverlapAndMonteCarloIsSeeded(t *testing.T) {
	w, err := WalkForwardWindows(30, WFConfig{Train: 10, Test: 5, Step: 5})
	if err != nil || len(w) != 4 {
		t.Fatalf("windows=%#v err=%v", w, err)
	}
	for _, x := range w {
		if x.TrainTo != x.TestFrom || x.TrainTo > x.TestFrom {
			t.Fatalf("leaky window %#v", x)
		}
	}
	r := Returns{Pessimistic: []float64{1, -2, 3, -1}, Optimistic: []float64{2, -1, 4, 0}}
	a, _ := MonteCarlo(r, MCConfig{Samples: 20, BlockLength: 2, Seed: 7})
	b, _ := MonteCarlo(r, MCConfig{Samples: 20, BlockLength: 2, Seed: 7})
	if !reflect.DeepEqual(a, b) {
		t.Fatal("Monte Carlo is not reproducible")
	}
}

func TestWithStartingEquityReportsEndingBalanceWithoutChangingPnL(t *testing.T) {
	m, err := MetricsFor(Returns{Pessimistic: []float64{-4}, Optimistic: []float64{5}})
	if err != nil {
		t.Fatal(err)
	}
	got := WithStartingEquity(m, 5_000)
	if got.FinalEquity.Pessimistic != 4_996 || got.FinalEquity.Optimistic != 5_005 {
		t.Fatalf("ending balance = %+v, want 4996 to 5005", got.FinalEquity)
	}
	if m.FinalEquity.Pessimistic != -4 || m.FinalEquity.Optimistic != 5 {
		t.Fatalf("source PnL mutated = %+v", m.FinalEquity)
	}
}
func TestSweepOrderingAndBounds(t *testing.T) {
	axes := []ParamAxis{{Name: "a", Values: []string{"1", "2"}}, {Name: "b", Values: []string{"x", "y"}}}
	got := Sweep(context.Background(), axes, func(_ context.Context, v []string) (Returns, error) {
		return Returns{Pessimistic: []float64{float64(len(v[0]))}, Optimistic: []float64{2}}, nil
	})
	if len(got) != 4 || got[0].Values[0] != "1" || got[3].Values[1] != "y" {
		t.Fatalf("unordered sweep %#v", got)
	}
	if got[0].Metrics.FinalEquity.Pessimistic == got[0].Metrics.FinalEquity.Optimistic {
		t.Fatal("bounds collapsed")
	}
}
