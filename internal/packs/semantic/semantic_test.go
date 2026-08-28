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
