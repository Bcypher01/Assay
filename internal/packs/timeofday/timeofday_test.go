package timeofday

import (
	"context"
	"testing"

	"github.com/Bcypher01/Assay/internal/core"
	"github.com/Bcypher01/Assay/internal/primitive"
	assayv1 "github.com/Bcypher01/Assay/proto/assay/v1"
)

func TestSessionCrossesMidnight(t *testing.T) {
	s := core.NewSeries("X", core.TFH1, 3)
	for _, t := range []core.Time{core.MustParseTime("2025-01-01T22:00:00Z"), core.MustParseTime("2025-01-01T23:00:00Z"), core.MustParseTime("2025-01-02T00:00:00Z")} {
		s.Append(t, 1, 1, 1, 1, 0)
	}
	p, _ := primitive.DefaultRegistry.Get("timeofday", "session")
	v, _, err := primitive.Evaluate(context.Background(), p, &assayv1.SessionParams{StartMinuteUtc: 23 * 60, EndMinuteUtc: 60}, core.TFH1, s, "fixture", nil)
	if err != nil {
		t.Fatal(err)
	}
	if v.Bool.Get(0) || !v.Bool.Get(1) || !v.Bool.Get(2) {
		t.Fatalf("unexpected session: %#v", v)
	}
}
