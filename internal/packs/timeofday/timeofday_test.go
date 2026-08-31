package timeofday

import (
	"context"
	"testing"

	assayv1 "github.com/bcypher01/assay/gen/go/assay/v1"
	"github.com/bcypher01/assay/internal/core"
	"github.com/bcypher01/assay/internal/primitive"
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

func TestSessionRangeAppearsOnlyAfterSessionCloses(t *testing.T) {
	s := core.NewSeries("X", core.TFH1, 7)
	for _, bar := range []struct {
		t         string
		high, low core.Price
	}{
		{"2025-01-01T07:00:00Z", 4, 2},
		{"2025-01-01T08:00:00Z", 5, 2},
		{"2025-01-01T09:00:00Z", 7, 1},
		{"2025-01-01T10:00:00Z", 8, 3},
		{"2025-01-01T11:00:00Z", 9, 4},
		{"2025-01-02T08:00:00Z", 6, 2},
		{"2025-01-02T10:00:00Z", 7, 1},
	} {
		s.Append(core.MustParseTime(bar.t), bar.low, bar.high, bar.low, bar.low, 0)
	}
	params := &assayv1.SessionParams{StartMinuteUtc: 8 * 60, EndMinuteUtc: 10 * 60}
	for _, tc := range []struct {
		typ  string
		want []core.Price
	}{
		{"session_high", []core.Price{0, 0, 0, 7, 7, 0, 6}},
		{"session_low", []core.Price{0, 0, 0, 1, 1, 0, 2}},
	} {
		t.Run(tc.typ, func(t *testing.T) {
			p, err := primitive.DefaultRegistry.Get("timeofday", tc.typ)
			if err != nil {
				t.Fatal(err)
			}
			v, _, err := primitive.Evaluate(context.Background(), p, params, core.TFH1, s, "fixture", nil)
			if err != nil {
				t.Fatal(err)
			}
			for i, want := range tc.want {
				valid := i == 3 || i == 4 || i == 6
				if v.Level[i] != want || v.Valid.Get(i) != valid {
					t.Fatalf("bar %d = (%d, %t), want (%d, %t)", i, v.Level[i], v.Valid.Get(i), want, valid)
				}
			}
		})
	}
}

func TestSessionRangeResetsAcrossDataGap(t *testing.T) {
	s := core.NewSeries("X", core.TFH1, 3)
	for _, bar := range []struct {
		t    string
		high core.Price
	}{
		{"2025-01-01T08:00:00Z", 9},
		{"2025-01-02T08:00:00Z", 6},
		{"2025-01-02T10:00:00Z", 7},
	} {
		s.Append(core.MustParseTime(bar.t), bar.high, bar.high, bar.high, bar.high, 0)
	}
	p, err := primitive.DefaultRegistry.Get("timeofday", "session_high")
	if err != nil {
		t.Fatal(err)
	}
	v, _, err := primitive.Evaluate(context.Background(), p, &assayv1.SessionParams{StartMinuteUtc: 8 * 60, EndMinuteUtc: 10 * 60}, core.TFH1, s, "fixture", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !v.Valid.Get(2) || v.Level[2] != 6 {
		t.Fatalf("gapped session range = (%d, %t), want (6, true)", v.Level[2], v.Valid.Get(2))
	}
}
