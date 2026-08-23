package pipeline

import (
	"context"
	"testing"
	"time"

	"github.com/Bcypher01/Assay/internal/core"
	"github.com/Bcypher01/Assay/internal/store"
)

func TestRunnerPublishesAndSecondRunIsNoop(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	fetcher := &fakeFetcher{series: buildSeries("BINANCE:BTCUSDT", core.TFM1)}
	r := Runner{
		Fetcher:   fetcher,
		Publisher: NewLocalPublisher(dir),
		Now:       func() time.Time { return time.Date(2024, 2, 15, 0, 0, 0, 0, time.UTC) },
	}
	job := Job{
		Symbols:      []string{"BINANCE:BTCUSDT"},
		TFs:          []core.TF{core.TFM1},
		From:         core.MustParseTime("2024-01-01T00:00:00Z"),
		To:           core.MustParseTime("2024-02-01T00:00:00Z"),
		BuildVersion: "test-build",
	}

	first, err := r.Run(ctx, job)
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	if first.Published != 1 || first.Skipped != 0 || fetcher.calls != 1 {
		t.Fatalf("first report=%+v calls=%d", first, fetcher.calls)
	}
	second, err := r.Run(ctx, job)
	if err != nil {
		t.Fatalf("second run: %v", err)
	}
	if second.Published != 0 || second.Skipped != 1 || fetcher.calls != 1 {
		t.Fatalf("second report=%+v calls=%d", second, fetcher.calls)
	}

	loaded, manifests, err := store.NewLocalStore(dir).Load(ctx, "BINANCE:BTCUSDT", core.TFM1, core.MinTime, core.MaxTime)
	if err != nil {
		t.Fatalf("load published: %v", err)
	}
	if loaded.Len() != fetcher.series.Len() {
		t.Fatalf("loaded %d bars, want %d", loaded.Len(), fetcher.series.Len())
	}
	if len(manifests) != 1 || manifests[0].BuildVersion != "test-build" || manifests[0].Source != "fake" {
		t.Fatalf("bad manifests: %+v", manifests)
	}
}

func TestRunnerFiltersTimeframesPerSymbol(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	fetcher := &multiFakeFetcher{series: map[string]map[core.TF]*core.Series{
		"BINANCE:BTCUSDT": {
			core.TFH1: buildSeriesAt("BINANCE:BTCUSDT", core.TFH1, "2024-01-01T00:00:00Z", "2024-01-01T01:00:00Z"),
			core.TFD1: buildSeriesAt("BINANCE:BTCUSDT", core.TFD1, "2024-01-01T00:00:00Z", "2024-01-02T00:00:00Z"),
		},
		"DUKAS:EURUSD": {
			core.TFD1: buildSeriesAt("DUKAS:EURUSD", core.TFD1, "2024-01-01T00:00:00Z", "2024-01-02T00:00:00Z"),
		},
	}}
	job := Job{
		Symbols: []string{"BINANCE:BTCUSDT", "DUKAS:EURUSD"},
		TFs:     []core.TF{core.TFH1, core.TFD1},
		TFsForSymbol: func(symbol string, requested []core.TF) ([]core.TF, error) {
			if symbol == "DUKAS:EURUSD" {
				return []core.TF{core.TFD1}, nil
			}
			return requested, nil
		},
		From:         core.MustParseTime("2024-01-01T00:00:00Z"),
		To:           core.MustParseTime("2024-02-01T00:00:00Z"),
		BuildVersion: "test-build",
	}
	report, err := (Runner{
		Fetcher:   fetcher,
		Publisher: NewLocalPublisher(dir),
		Now:       func() time.Time { return time.Date(2024, 2, 15, 0, 0, 0, 0, time.UTC) },
	}).Run(ctx, job)
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if report.Published != 3 {
		t.Fatalf("published %d, want 3", report.Published)
	}
	if fetcher.calls["DUKAS:EURUSD/h1"] != 0 {
		t.Fatal("FX h1 should have been filtered out")
	}
	_, manifests, err := store.NewLocalStore(dir).Load(ctx, "DUKAS:EURUSD", core.TFD1, core.MinTime, core.MaxTime)
	if err != nil {
		t.Fatalf("load FX: %v", err)
	}
	if len(manifests) != 1 || manifests[0].Source != "multi" {
		t.Fatalf("bad FX manifest: %+v", manifests)
	}
}

type fakeFetcher struct {
	series *core.Series
	calls  int
}

func (f *fakeFetcher) Name() string { return "fake" }

func (f *fakeFetcher) Available(ctx context.Context, sym string) (core.Time, core.Time, []core.TF, error) {
	return f.series.Time[0], f.series.Time[f.series.Len()-1], []core.TF{f.series.TF}, nil
}

func (f *fakeFetcher) Fetch(ctx context.Context, sym string, tf core.TF, from, to core.Time) (*core.Series, error) {
	f.calls++
	return f.series.Slice(from, to), nil
}

func buildSeries(symbol string, tf core.TF) *core.Series {
	s := core.NewSeries(symbol, tf, 2)
	s.Append(core.MustParseTime("2024-01-01T00:00:00Z"), 100, 101, 99, 100, 10)
	s.Append(core.MustParseTime("2024-01-01T00:01:00Z"), 100, 102, 100, 101, 11)
	return s
}

type multiFakeFetcher struct {
	series map[string]map[core.TF]*core.Series
	calls  map[string]int
}

func (f *multiFakeFetcher) Name() string { return "multi" }

func (f *multiFakeFetcher) Available(ctx context.Context, sym string) (core.Time, core.Time, []core.TF, error) {
	return 0, 0, nil, nil
}

func (f *multiFakeFetcher) Fetch(ctx context.Context, sym string, tf core.TF, from, to core.Time) (*core.Series, error) {
	if f.calls == nil {
		f.calls = make(map[string]int)
	}
	f.calls[sym+"/"+tf.String()]++
	return f.series[sym][tf].Slice(from, to), nil
}

func buildSeriesAt(symbol string, tf core.TF, first, second string) *core.Series {
	s := core.NewSeries(symbol, tf, 2)
	s.Append(core.MustParseTime(first), 100, 101, 99, 100, 10)
	s.Append(core.MustParseTime(second), 100, 102, 100, 101, 11)
	return s
}
