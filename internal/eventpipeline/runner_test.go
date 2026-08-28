package eventpipeline

import (
	"context"
	"testing"

	"github.com/Bcypher01/Assay/internal/core"
	"github.com/Bcypher01/Assay/internal/event"
	"github.com/Bcypher01/Assay/internal/eventsource"
)

func TestRunnerCreatesLoadableSnapshot(t *testing.T) {
	s := event.NewStore(t.TempDir())
	m := event.Event{ID: "test:1", OccurredAt: core.MustParseTime("2024-01-01T13:30:00Z"), Precision: event.PrecisionExact, Kind: event.KindUSCPI, Title: "CPI", Jurisdiction: "US", Currencies: []string{"USD"}, Impact: event.ImpactHigh, Status: event.StatusReleased, KnowledgeAt: core.MustParseTime("2024-01-01T13:30:00Z"), Source: event.SourceRef{Provider: "fake", RecordID: "1"}}
	mf, err := (Runner{Provider: fakeProvider{events: []event.Event{m}}, Store: s}).Run(context.Background(), Job{BuildVersion: "test"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load(context.Background(), mf.ContentHash); err != nil {
		t.Fatal(err)
	}
}

type fakeProvider struct{ events []event.Event }

func (fakeProvider) Name() string { return "fake" }
func (p fakeProvider) Events(context.Context, eventsource.Scope) ([]event.Event, error) {
	return p.events, nil
}
