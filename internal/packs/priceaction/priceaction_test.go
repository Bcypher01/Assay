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
