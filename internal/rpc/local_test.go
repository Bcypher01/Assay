package rpc

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Bcypher01/Assay/internal/core"
	"github.com/Bcypher01/Assay/internal/instrument"
	"github.com/Bcypher01/Assay/internal/ir"
	"github.com/Bcypher01/Assay/internal/library"
	_ "github.com/Bcypher01/Assay/internal/packs/indicator"
	"github.com/Bcypher01/Assay/internal/store"
	"github.com/Bcypher01/Assay/internal/venue"
	assayv1 "github.com/Bcypher01/Assay/proto/assay/v1"
)

func TestLocalRunMissingCacheDoesNotFetch(t *testing.T) {
	ctx := context.Background()
	cache := store.NewLocalStore(t.TempDir())
	registry, err := instrument.Default()
	if err != nil {
		t.Fatal(err)
	}
	fallback := &countingFetcher{}
	loader := library.New(cache, nil, fallback)
	server, err := NewLocalServerWithServices(cache, registry, loader, nil)
	if err != nil {
		t.Fatal(err)
	}
	strategy := testStrategyJSON(t)
	summary, err := server.RunFn(ctx, &assayv1.RunRequest{
		StrategyJson: strategy,
		FromMs:       int64(core.MustParseTime("2024-01-01T00:00:00Z")),
		ToMs:         int64(core.MustParseTime("2024-02-01T00:00:00Z")),
	}, func(*assayv1.RunUpdate) error { return nil })
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if fallback.fetches != 0 {
		t.Fatalf("run fetched external data %d times", fallback.fetches)
	}
	if summary == nil || summary.ErrorCode != "DATA_MISSING" {
		t.Fatalf("summary = %+v, want DATA_MISSING", summary)
	}
}

func TestLocalDataStatusExposesMissingPartitions(t *testing.T) {
	ctx := context.Background()
	cache := store.NewLocalStore(t.TempDir())
	registry, err := instrument.Default()
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewLocalServerWithServices(cache, registry, library.New(cache, nil, &countingFetcher{}), nil)
	if err != nil {
		t.Fatal(err)
	}
	status, err := server.DataStatusFn(ctx, &assayv1.EnsureDataRequest{
		Symbol:    "DUKAS:EURUSD",
		Timeframe: "d1",
		FromMs:    int64(core.MustParseTime("2024-01-01T00:00:00Z")),
		ToMs:      int64(core.MustParseTime("2024-03-01T00:00:00Z")),
	})
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if status.Readiness != assayv1.DataReadiness_DATA_READINESS_MISSING || status.ErrorCode != "DATA_MISSING" {
		t.Fatalf("status = %+v", status)
	}
	if len(status.MissingPartitions) != 2 {
		t.Fatalf("missing partitions = %d, want 2", len(status.MissingPartitions))
	}
}

type countingFetcher struct {
	fetches int
}

func (f *countingFetcher) Name() string { return "test-source" }

func (f *countingFetcher) Available(context.Context, string) (core.Time, core.Time, []core.TF, error) {
	return core.MinTime, core.MaxTime, []core.TF{core.TFD1}, nil
}

func (f *countingFetcher) Fetch(context.Context, string, core.TF, core.Time, core.Time) (*core.Series, error) {
	f.fetches++
	return nil, venue.ErrUnavailable
}

func testStrategyJSON(t *testing.T) []byte {
	t.Helper()
	strategy := ir.Strategy{
		Schema:    ir.SchemaV1,
		Name:      "missing cache",
		Universe:  ir.Universe{Symbols: []string{"DUKAS:EURUSD"}},
		Direction: ir.DirectionLong,
		Timeframes: map[string]core.TF{
			"bias":  core.TFD1,
			"poi":   core.TFD1,
			"entry": core.TFD1,
		},
		Risk: ir.Risk{
			Stop:   core.Distance{Unit: core.UnitPips, Value: core.RatioFromInt(5)},
			Target: core.Distance{Unit: core.UnitPips, Value: core.RatioFromInt(10)},
		},
		Nodes: []ir.Node{
			{ID: "entry", Pack: "indicator", Type: "ma_touch", Timeframe: core.TFD1, Params: []byte(`{}`)},
		},
		Slots: ir.Slots{
			Trigger: ir.SlotSpec{All: []string{"entry"}},
		},
	}
	b, err := json.Marshal(strategy)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
