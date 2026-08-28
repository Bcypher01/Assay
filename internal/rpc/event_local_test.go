package rpc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"
	"github.com/Bcypher01/Assay/internal/core"
	"github.com/Bcypher01/Assay/internal/event"
	"github.com/Bcypher01/Assay/internal/instrument"
	"github.com/Bcypher01/Assay/internal/store"
	assayv1 "github.com/Bcypher01/Assay/proto/assay/v1"
)

func TestLocalEventServicesReadSnapshotAndPreparedBars(t *testing.T) {
	ctx, dir := context.Background(), t.TempDir()
	cache := store.NewLocalStore(dir + "/bars")
	s := core.NewSeries("DUKAS:EURUSD", core.TFM1, 3)
	base := core.MustParseTime("2024-01-02T13:30:00Z")
	for i := 0; i < 3; i++ {
		s.Append(base+core.Time(i*60_000), 100, 102, 99, core.Price(100+i), 1)
	}
	if err := cache.Write(ctx, s, store.Manifest{Symbol: s.Symbol, TF: s.TF, Month: "2024-01", Source: "test", BuildVersion: "test"}); err != nil {
		t.Fatal(err)
	}
	events := event.NewStore(dir + "/events")
	e := event.Event{ID: "cpi", OccurredAt: base, KnowledgeAt: base, Precision: event.PrecisionExact, Kind: event.KindUSCPI, Title: "CPI", Jurisdiction: "US", Currencies: []string{"USD"}, Impact: event.ImpactHigh, Status: event.StatusReleased, Source: event.SourceRef{Provider: "test", RecordID: "cpi"}}
	mf, err := events.Write(ctx, []event.Event{e}, event.Manifest{Source: "test", BuildVersion: "test"})
	if err != nil {
		t.Fatal(err)
	}
	reg, err := instrument.Default()
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewLocalServerWithEventServices(cache, reg, nil, nil, events)
	if err != nil {
		t.Fatal(err)
	}
	q, err := server.QueryEvents(ctx, connect.NewRequest(&assayv1.QueryEventsRequest{Snapshot: mf.ContentHash}))
	if err != nil || len(q.Msg.Events) != 1 {
		t.Fatalf("query %v %#v", err, q)
	}
	r, err := server.RunEventStudy(ctx, connect.NewRequest(&assayv1.EventStudyRequest{Snapshot: mf.ContentHash, Symbol: s.Symbol, Timeframe: "m1", Filter: &assayv1.EventFilter{FromMs: int64(base), ToMs: int64(base + 180000)}, Windows: []string{"m1"}}))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Msg.EligibleEvents) != 1 || r.Msg.EventSnapshot != mf.ContentHash {
		t.Fatalf("study %#v", r.Msg)
	}
}

func TestLocalEventSnapshotCatalogue(t *testing.T) {
	ctx, dir := context.Background(), t.TempDir()
	events := event.NewStore(dir + "/events")
	at := core.MustParseTime("2024-01-02T13:30:00Z")
	mf, err := events.Write(ctx, []event.Event{{ID: "cpi", OccurredAt: at, KnowledgeAt: at, Precision: event.PrecisionExact, Kind: event.KindUSCPI, Title: "CPI", Jurisdiction: "US", Currencies: []string{"USD"}, Impact: event.ImpactHigh, Status: event.StatusReleased, Source: event.SourceRef{Provider: "BLS", RecordID: "cpi"}}}, event.Manifest{Source: "BLS", BuildVersion: "test"})
	if err != nil {
		t.Fatal(err)
	}
	reg, err := instrument.Default()
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewLocalServerWithEventServices(store.NewLocalStore(dir+"/bars"), reg, nil, nil, events)
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(NewHandler(server))
	defer httpServer.Close()
	res, err := http.Get(httpServer.URL + "/event-snapshots")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", res.StatusCode)
	}
	var got struct {
		Snapshots []event.SnapshotMetadata `json:"snapshots"`
	}
	if err := json.NewDecoder(res.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got.Snapshots) != 1 || got.Snapshots[0].ContentHash != mf.ContentHash || got.Snapshots[0].Source != "BLS" {
		t.Fatalf("catalogue = %#v", got)
	}
}
