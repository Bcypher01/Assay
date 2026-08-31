package event

import (
	"context"
	"errors"
	"testing"

	"github.com/bcypher01/assay/internal/core"
)

func TestStoreRoundTripIsContentAddressedAndDeterministic(t *testing.T) {
	a := testEvent("b", "2024-02-01T13:30:00Z")
	b := testEvent("a", "2024-01-01T13:30:00Z")
	s := NewStore(t.TempDir())
	first, err := s.Write(context.Background(), []Event{a, b}, Manifest{Source: "test", BuildVersion: "v1"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Write(context.Background(), []Event{b, a}, Manifest{Source: "test", BuildVersion: "v1"})
	if err != nil {
		t.Fatal(err)
	}
	if first.ContentHash != second.ContentHash {
		t.Fatalf("hashes differ: %s %s", first.ContentHash, second.ContentHash)
	}
	loaded, err := s.Load(context.Background(), first.ContentHash)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Events) != 2 || loaded.Events[0].ID != "a" || loaded.Manifest.Rows != 2 {
		t.Fatalf("loaded = %+v", loaded)
	}
}

func TestStoreListReturnsVerifiedCatalogueMetadata(t *testing.T) {
	ctx := context.Background()
	s := NewStore(t.TempDir())
	first, err := s.Write(ctx, []Event{testEvent("first", "2024-01-01T13:30:00Z")}, Manifest{Source: "BLS", BuildVersion: "v1"})
	if err != nil {
		t.Fatal(err)
	}
	items, err := s.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].ContentHash != first.ContentHash || items[0].Source != "BLS" || items[0].Rows != 1 || items[0].First == 0 || items[0].CreatedAt == 0 {
		t.Fatalf("catalogue = %#v", items)
	}
}

func TestQueryRespectsKnowledgeTime(t *testing.T) {
	early := testEvent("early", "2024-01-01T10:00:00Z")
	late := testEvent("late", "2024-01-01T11:00:00Z")
	late.KnowledgeAt = core.MustParseTime("2024-01-01T12:00:00Z")
	s := Snapshot{Events: []Event{late, early}}
	got := s.Query(Filter{Kinds: []Kind{KindUSCPI}, AsOf: core.MustParseTime("2024-01-01T11:30:00Z")})
	if len(got) != 1 || got[0].ID != "early" {
		t.Fatalf("query = %+v", got)
	}
}

func TestValidateRejectsInventedIntradayTimestamp(t *testing.T) {
	e := testEvent("day", "2024-01-01T13:30:00Z")
	e.Precision = PrecisionDay
	if err := e.Validate(); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("error = %v", err)
	}
}

func TestValidateRequiresRevisionLineage(t *testing.T) {
	e := testEvent("revision", "2024-01-01T13:30:00Z")
	e.Status = StatusRevised
	if err := e.Validate(); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("error = %v", err)
	}
	e.RevisionOf = "original"
	if err := e.Validate(); err != nil {
		t.Fatal(err)
	}
}

func testEvent(id, at string) Event {
	t := core.MustParseTime(at)
	return Event{ID: id, OccurredAt: t, Precision: PrecisionExact, Kind: KindUSCPI, Title: id, Jurisdiction: "US", Currencies: []string{"USD"}, Impact: ImpactHigh, Status: StatusReleased, KnowledgeAt: t, Source: SourceRef{Provider: "test", RecordID: id}}
}
