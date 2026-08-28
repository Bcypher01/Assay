package curated

import (
	"context"
	"testing"

	"github.com/Bcypher01/Assay/internal/core"
	"github.com/Bcypher01/Assay/internal/eventsource"
)

func TestCatalogueIsValidAndScopeable(t *testing.T) {
	events, err := (Provider{}).Events(context.Background(), eventsource.Scope{From: core.MustParseTime("2022-01-01T00:00:00Z"), To: core.MustParseTime("2023-01-01T00:00:00Z")})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 || events[1].ID != "curated:russia-ukraine-2022" {
		t.Fatalf("events = %+v", events)
	}
}

func TestCatalogueMeetsInitialCoverageFloor(t *testing.T) {
	events, err := (Provider{}).Events(context.Background(), eventsource.Scope{})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) < 20 {
		t.Fatalf("catalogue has %d events, want at least 20", len(events))
	}
}
