package eventstudy

import (
	"github.com/Bcypher01/Assay/internal/core"
	"github.com/Bcypher01/Assay/internal/event"
	"testing"
)

func TestRunAlignsAfterKnowledgeAndExcludesImprecise(t *testing.T) {
	s := core.NewSeries("EURUSD", core.TFM1, 4)
	for i, p := range []core.Price{100, 110, 120, 130} {
		s.Append(core.Time(i*60_000), p, p, p, p, 1)
	}
	exact := event.Event{ID: "x", OccurredAt: 0, KnowledgeAt: core.Time(30_000), Precision: event.PrecisionExact, Kind: event.KindUSCPI, Title: "x", Jurisdiction: "US", Currencies: []string{"USD"}, Impact: event.ImpactHigh, Status: event.StatusReleased, Source: event.SourceRef{Provider: "x", RecordID: "x"}}
	day := exact
	day.ID = "d"
	day.Precision = event.PrecisionDay
	day.OccurredAt = 0
	got, err := Run(Request{Events: event.Snapshot{Manifest: event.Manifest{ContentHash: "events"}, Events: []event.Event{exact, day}}, Series: s, Windows: []core.TF{core.TFM1}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Eligible) != 1 || got.Eligible[0].AlignedBar != 60_000 || got.Eligible[0].Returns["m1"] != float64(10)/110 {
		t.Fatalf("%+v", got)
	}
	if len(got.Excluded) != 1 {
		t.Fatalf("%+v", got.Excluded)
	}
}

func TestRunExcludesIncompleteWindowRatherThanTruncatingIt(t *testing.T) {
	s := core.NewSeries("EURUSD", core.TFM15, 2)
	s.Append(core.MustParseTime("2024-01-01T10:00:00Z"), 100, 100, 100, 100, 1)
	s.Append(core.MustParseTime("2024-01-01T10:15:00Z"), 101, 101, 101, 101, 1)
	e := event.Event{ID: "x", OccurredAt: s.Time[0], KnowledgeAt: s.Time[0], Precision: event.PrecisionExact, Kind: event.KindUSCPI, Title: "x", Jurisdiction: "US", Currencies: []string{"USD"}, Impact: event.ImpactHigh, Status: event.StatusReleased, Source: event.SourceRef{Provider: "x", RecordID: "x"}}
	got, err := Run(Request{Events: event.Snapshot{Events: []event.Event{e}}, Series: s, Windows: []core.TF{core.TFH1}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Eligible) != 0 || len(got.Excluded) != 1 || got.Excluded[0].Reason != "insufficient bars for requested forward window" {
		t.Fatalf("%+v", got)
	}
}
