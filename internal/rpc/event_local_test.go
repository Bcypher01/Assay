package rpc

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"
	assayv1 "github.com/bcypher01/assay/gen/go/assay/v1"
	"github.com/bcypher01/assay/internal/core"
	"github.com/bcypher01/assay/internal/event"
	"github.com/bcypher01/assay/internal/instrument"
	"github.com/bcypher01/assay/internal/store"
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
	actual, forecast, previous := event.Decimal("250"), event.Decimal("200"), event.Decimal("180")
	e := event.Event{ID: "cpi", OccurredAt: base, KnowledgeAt: base, Precision: event.PrecisionExact, Kind: event.KindUSCPI, Title: "CPI", Jurisdiction: "US", Currencies: []string{"USD"}, Impact: event.ImpactHigh, Status: event.StatusReleased, Actual: &actual, Forecast: &forecast, Previous: &previous, Source: event.SourceRef{Provider: "test", RecordID: "cpi"}}
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
	if q.Msg.Events[0].Actual != "250" || q.Msg.Events[0].Forecast != "200" || q.Msg.Events[0].Previous != "180" {
		t.Fatalf("event values %#v", q.Msg.Events[0])
	}
	r, err := server.RunEventStudy(ctx, connect.NewRequest(&assayv1.EventStudyRequest{
		Snapshot: mf.ContentHash, Symbol: s.Symbol, Timeframe: "m1",
		Filter: &assayv1.EventFilter{FromMs: int64(base), ToMs: int64(base + 180000)}, Windows: []string{"m1"},
		Trade: &assayv1.EventTradeConfig{
			EntryTiming: "after_release", DirectionRule: "follow_surprise", EntryDelay: "m1", HoldFor: "m1",
			SurpriseFilter: "all", StartingEquity: 1_000_000, PositionSize: 1_000_000, CostPerTrade: 500,
		},
	}))
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Msg.EligibleEvents) != 1 || r.Msg.EventSnapshot != mf.ContentHash {
		t.Fatalf("study %#v", r.Msg)
	}
	if !r.Msg.EligibleEvents[0].HasSurprise || r.Msg.TradeSummary == nil || r.Msg.TradeSummary.Trades != 1 || len(r.Msg.Trades) != 1 || r.Msg.Trades[0].Side != "short" {
		t.Fatalf("trade study %#v", r.Msg)
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
