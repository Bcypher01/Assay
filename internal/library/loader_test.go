package library

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/Bcypher01/Assay/internal/core"
	"github.com/Bcypher01/Assay/internal/pipeline"
	"github.com/Bcypher01/Assay/internal/store"
)

func TestDeriveRequestRejectsUnsupportedResolution(t *testing.T) {
	_, err := DeriveRequest(nil,
		[]string{"DUKAS:EURUSD"},
		[]core.TF{core.TFH4},
		core.MustParseTime("2024-01-01T00:00:00Z"),
		core.MustParseTime("2024-02-01T00:00:00Z"),
	)
	if !errors.Is(err, ErrUnsupportedResolution) {
		t.Fatalf("expected ErrUnsupportedResolution, got %v", err)
	}
	if got := err.Error(); got != "library: unsupported resolution: h4 for DUKAS:EURUSD" {
		t.Fatalf("error did not name timeframe and instrument: %q", got)
	}

	_, err = DeriveRequest(nil,
		[]string{"BINANCE:BTCUSDT"},
		[]core.TF{core.TFM5},
		core.MustParseTime("2024-01-01T00:00:00Z"),
		core.MustParseTime("2024-02-01T00:00:00Z"),
	)
	if !errors.Is(err, ErrUnsupportedResolution) {
		t.Fatalf("expected crypto m5 to be unsupported, got %v", err)
	}
}

func TestDeriveRequestAllowsAdjustedPublishedPolicy(t *testing.T) {
	r, err := DeriveRequest(nil,
		[]string{"BINANCE:BTCUSDT"},
		[]core.TF{core.TFH4, core.TFM15, core.TFD1},
		core.MustParseTime("2024-01-01T00:00:00Z"),
		core.MustParseTime("2024-02-01T00:00:00Z"),
	)
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	if len(r.TFs) != 3 || r.TFs[0] != core.TFM15 || r.TFs[1] != core.TFH4 || r.TFs[2] != core.TFD1 {
		t.Fatalf("unexpected TF derivation: %v", r.TFs)
	}
}

func TestEnsureColdDownloadsVerifiesAndLoads(t *testing.T) {
	ctx := context.Background()
	published := t.TempDir()
	cache := store.NewLocalStore(t.TempDir())
	objects := &countingObjects{ObjectStore: pipeline.NewLocalPublisher(published)}
	want := publishFixture(t, ctx, published, "BINANCE:BTCUSDT", core.TFM15, "2024-01")

	loader := New(cache, objects, nil)
	req := Request{
		Symbols: []string{"BINANCE:BTCUSDT"},
		TFs:     []core.TF{core.TFM15},
		From:    core.MustParseTime("2024-01-01T00:00:00Z"),
		To:      core.MustParseTime("2024-02-01T00:00:00Z"),
	}
	var stages []string
	if err := loader.Ensure(ctx, req, func(p Progress) { stages = append(stages, p.Stage) }); err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if objects.heads != 1 || objects.gets != 1 {
		t.Fatalf("network calls heads=%d gets=%d, want 1/1", objects.heads, objects.gets)
	}
	got, manifests, err := loader.Load(ctx, "BINANCE:BTCUSDT", core.TFM15, core.MinTime, core.MaxTime)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	assertSeriesEqual(t, want, got)
	if len(manifests) != 1 || manifests[0].ContentHash == "" {
		t.Fatalf("bad manifests: %+v", manifests)
	}
	if len(stages) == 0 {
		t.Fatal("expected progress callbacks")
	}
}

func TestEnsureWarmCacheMakesZeroNetworkCalls(t *testing.T) {
	ctx := context.Background()
	cache := store.NewLocalStore(t.TempDir())
	series := buildMonth("BINANCE:BTCUSDT", core.TFM15, "2024-01", 4)
	if err := cache.Write(ctx, series, store.Manifest{
		Symbol: "BINANCE:BTCUSDT", TF: core.TFM15, Month: "2024-01", Source: "fixture", BuildVersion: "test",
	}); err != nil {
		t.Fatalf("seed cache: %v", err)
	}

	loader := New(cache, failingObjects{}, nil)
	req := Request{
		Symbols: []string{"BINANCE:BTCUSDT"},
		TFs:     []core.TF{core.TFM15},
		From:    core.MustParseTime("2024-01-01T00:00:00Z"),
		To:      core.MustParseTime("2024-02-01T00:00:00Z"),
	}
	if err := loader.Ensure(ctx, req, nil); err != nil {
		t.Fatalf("ensure warm: %v", err)
	}
}

func TestEnsureFallsBackToVenueAndBarsMatchPublished(t *testing.T) {
	ctx := context.Background()
	published := t.TempDir()
	want := publishFixture(t, ctx, published, "BINANCE:BTCUSDT", core.TFM15, "2024-01")
	cache := store.NewLocalStore(t.TempDir())
	fallback := &fixtureFetcher{series: want}
	loader := New(cache, failingObjects{}, fallback)
	req := Request{
		Symbols: []string{"BINANCE:BTCUSDT"},
		TFs:     []core.TF{core.TFM15},
		From:    core.MustParseTime("2024-01-01T00:00:00Z"),
		To:      core.MustParseTime("2024-02-01T00:00:00Z"),
	}
	if err := loader.Ensure(ctx, req, nil); err != nil {
		t.Fatalf("ensure fallback: %v", err)
	}
	if fallback.calls != 1 {
		t.Fatalf("fallback calls=%d, want 1", fallback.calls)
	}
	got, _, err := loader.Load(ctx, "BINANCE:BTCUSDT", core.TFM15, core.MinTime, core.MaxTime)
	if err != nil {
		t.Fatalf("load fallback: %v", err)
	}
	assertSeriesEqual(t, want, got)
}

func TestHashMismatchRetriesOnceThenFailsTyped(t *testing.T) {
	ctx := context.Background()
	published := t.TempDir()
	publishFixture(t, ctx, published, "BINANCE:BTCUSDT", core.TFM15, "2024-01")
	objects := &corruptingObjects{ObjectStore: pipeline.NewLocalPublisher(published), always: true}
	loader := New(store.NewLocalStore(t.TempDir()), objects, nil)
	req := Request{
		Symbols: []string{"BINANCE:BTCUSDT"},
		TFs:     []core.TF{core.TFM15},
		From:    core.MustParseTime("2024-01-01T00:00:00Z"),
		To:      core.MustParseTime("2024-02-01T00:00:00Z"),
	}
	err := loader.Ensure(ctx, req, nil)
	if !errors.Is(err, store.ErrHashMismatch) {
		t.Fatalf("expected hash mismatch, got %v", err)
	}
	if objects.gets != 2 {
		t.Fatalf("gets=%d, want retry exactly once", objects.gets)
	}
}

type countingObjects struct {
	ObjectStore
	heads int
	gets  int
}

func (o *countingObjects) Head(ctx context.Context, key string) (store.Manifest, bool, error) {
	o.heads++
	return o.ObjectStore.Head(ctx, key)
}

func (o *countingObjects) Get(ctx context.Context, key string) (io.ReadCloser, bool, error) {
	o.gets++
	return o.ObjectStore.Get(ctx, key)
}

type corruptingObjects struct {
	ObjectStore
	always bool
	gets   int
}

func (o *corruptingObjects) Get(ctx context.Context, key string) (io.ReadCloser, bool, error) {
	o.gets++
	rc, ok, err := o.ObjectStore.Get(ctx, key)
	if err != nil || !ok {
		return rc, ok, err
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, false, err
	}
	if o.always || o.gets == 1 {
		data[len(data)/2] ^= 0xFF
	}
	return io.NopCloser(bytes.NewReader(data)), true, nil
}

type failingObjects struct{}

func (failingObjects) Head(ctx context.Context, key string) (store.Manifest, bool, error) {
	return store.Manifest{}, false, errors.New("object store should not be called")
}

func (failingObjects) Get(ctx context.Context, key string) (io.ReadCloser, bool, error) {
	return nil, false, errors.New("object store should not be called")
}

type fixtureFetcher struct {
	series *core.Series
	calls  int
}

func (f *fixtureFetcher) Name() string { return "fixture-venue" }

func (f *fixtureFetcher) Available(ctx context.Context, sym string) (core.Time, core.Time, []core.TF, error) {
	first, last, _ := f.series.Bounds()
	return first, last, []core.TF{f.series.TF}, nil
}

func (f *fixtureFetcher) Fetch(ctx context.Context, sym string, tf core.TF, from, to core.Time) (*core.Series, error) {
	f.calls++
	return f.series.Slice(from, to), nil
}

func publishFixture(t *testing.T, ctx context.Context, root, symbol string, tf core.TF, month string) *core.Series {
	t.Helper()
	series := buildMonth(symbol, tf, month, 4)
	meta := store.Manifest{Symbol: symbol, TF: tf, Month: month, Source: "fixture", BuildVersion: "test"}
	data, completed, err := store.EncodeForPublish(series, meta)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	key := pipeline.ObjectKey(symbol, tf, month)
	if err := pipeline.NewLocalPublisher(root).Put(ctx, key, bytes.NewReader(data), completed); err != nil {
		t.Fatalf("publish: %v", err)
	}
	return series
}

func buildMonth(symbol string, tf core.TF, yyyyMM string, n int) *core.Series {
	start, err := time.Parse("2006-01", yyyyMM)
	if err != nil {
		panic(err)
	}
	s := core.NewSeries(symbol, tf, n)
	t := core.FromStd(start)
	for i := 0; i < n; i++ {
		price := core.Price(10000 + i)
		s.Append(t+core.Time(int64(i)*tf.Millis()), price, price+2, price-2, price+1, int64(100+i))
	}
	if err := s.Validate(); err != nil {
		panic(err)
	}
	return s
}

func assertSeriesEqual(t *testing.T, want, got *core.Series) {
	t.Helper()
	if got.Symbol != want.Symbol || got.TF != want.TF || got.Len() != want.Len() {
		t.Fatalf("got (%s,%s,%d), want (%s,%s,%d)", got.Symbol, got.TF, got.Len(), want.Symbol, want.TF, want.Len())
	}
	for i := 0; i < want.Len(); i++ {
		if got.Time[i] != want.Time[i] || got.Open[i] != want.Open[i] ||
			got.High[i] != want.High[i] || got.Low[i] != want.Low[i] ||
			got.Close[i] != want.Close[i] || got.Volume[i] != want.Volume[i] {
			t.Fatalf("bar %d got (%d,%d,%d,%d,%d,%d), want (%d,%d,%d,%d,%d,%d)",
				i, got.Time[i], got.Open[i], got.High[i], got.Low[i], got.Close[i], got.Volume[i],
				want.Time[i], want.Open[i], want.High[i], want.Low[i], want.Close[i], want.Volume[i])
		}
	}
}
