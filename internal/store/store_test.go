package store

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Bcypher01/Assay/internal/core"
)

// buildMonth builds n valid, aligned bars for tf starting at the first instant
// of the UTC month containing start. Prices wander deterministically around a
// base tick value; every bar satisfies core.Series.Validate.
func buildMonth(t *testing.T, symbol string, tf core.TF, start core.Time, n int) *core.Series {
	t.Helper()
	open := firstOfMonth(start.Std())
	base := core.FromStd(open)
	s := core.NewSeries(symbol, tf, n)
	step := tf.Millis()
	price := int64(108000)
	for i := 0; i < n; i++ {
		ts := base + core.Time(int64(i)*step)
		// Deterministic pseudo-random walk without touching package rand.
		price += int64((i*2654435761)%7) - 3
		o := price
		c := price + int64((i*40503)%5) - 2
		hi := o
		if c > hi {
			hi = c
		}
		hi += int64((i * 15485863) % 4)
		lo := o
		if c < lo {
			lo = c
		}
		lo -= int64((i * 12582917) % 4)
		s.Append(ts, core.Price(o), core.Price(hi), core.Price(lo), core.Price(c), int64(1000+i))
	}
	if err := s.Validate(); err != nil {
		t.Fatalf("built invalid series: %v", err)
	}
	return s
}

func monthStart(yyyyMM string) core.Time {
	tm, err := time.Parse("2006-01", yyyyMM)
	if err != nil {
		panic(err)
	}
	return core.FromStd(tm)
}

func assertSeriesEqual(t *testing.T, want, got *core.Series) {
	t.Helper()
	if got.Symbol != want.Symbol || got.TF != want.TF {
		t.Fatalf("identity mismatch: got (%s,%s) want (%s,%s)", got.Symbol, got.TF, want.Symbol, want.TF)
	}
	if got.Len() != want.Len() {
		t.Fatalf("length mismatch: got %d want %d", got.Len(), want.Len())
	}
	for i := 0; i < want.Len(); i++ {
		if got.Time[i] != want.Time[i] || got.Open[i] != want.Open[i] ||
			got.High[i] != want.High[i] || got.Low[i] != want.Low[i] ||
			got.Close[i] != want.Close[i] || got.Volume[i] != want.Volume[i] {
			t.Fatalf("bar %d differs:\n got  t=%d o=%d h=%d l=%d c=%d v=%d\n want t=%d o=%d h=%d l=%d c=%d v=%d",
				i, got.Time[i], got.Open[i], got.High[i], got.Low[i], got.Close[i], got.Volume[i],
				want.Time[i], want.Open[i], want.High[i], want.Low[i], want.Close[i], want.Volume[i])
		}
	}
}

func TestRoundTripExact(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	st := NewLocalStore(dir)

	want := buildMonth(t, "EURUSD", core.TFM1, monthStart("2026-08"), 5000)
	meta := Manifest{Symbol: "EURUSD", TF: core.TFM1, Month: "2026-08", Source: "dukascopy", BuildVersion: "test"}
	if err := st.Write(ctx, want, meta); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, manifests, err := st.Load(ctx, "EURUSD", core.TFM1, core.MinTime, core.MaxTime)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	assertSeriesEqual(t, want, got)

	if len(manifests) != 1 {
		t.Fatalf("expected 1 manifest, got %d", len(manifests))
	}
	m := manifests[0]
	if m.Rows != int64(want.Len()) {
		t.Errorf("manifest rows: got %d want %d", m.Rows, want.Len())
	}
	if m.First != want.Time[0] || m.Last != want.Time[want.Len()-1] {
		t.Errorf("manifest bounds wrong: first=%d last=%d", m.First, m.Last)
	}
	if m.ContentHash == "" {
		t.Error("manifest content hash empty")
	}
	if m.SchemaVersion != SchemaVersion {
		t.Errorf("manifest schema version: got %d want %d", m.SchemaVersion, SchemaVersion)
	}
}

func TestLoadReturnsManifestWhenWindowFallsInsideLastCandle(t *testing.T) {
	ctx := context.Background()
	st := NewLocalStore(t.TempDir())
	series := core.NewSeries("DUKAS:EURUSD", core.TFD1, 1)
	open := core.MustParseTime("2025-08-29T00:00:00Z")
	series.Append(open, 110000, 111000, 109000, 110500, 1)
	if err := st.Write(ctx, series, Manifest{
		Symbol: "DUKAS:EURUSD", TF: core.TFD1, Month: "2025-08", Source: "dukascopy-bid", BuildVersion: "test",
	}); err != nil {
		t.Fatal(err)
	}

	got, manifests, err := st.Load(ctx, "DUKAS:EURUSD", core.TFD1,
		core.MustParseTime("2025-08-29T09:35:00Z"), core.MustParseTime("2025-08-29T10:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Len() != 0 || len(manifests) != 1 {
		t.Fatalf("bars=%d manifests=%d, want 0 bars and the overlapping manifest", got.Len(), len(manifests))
	}
}

func TestRoundTripCodecDirect(t *testing.T) {
	// The reader must decode straight into typed slices identical to the input,
	// independent of the cache layer.
	want := buildMonth(t, "BTCUSDT", core.TFH1, monthStart("2026-01"), 700)
	data, err := encodeSeries(want)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	got, err := decodeSeries(data, "BTCUSDT", core.TFH1)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	assertSeriesEqual(t, want, got)
}

func TestDeterministicBytes(t *testing.T) {
	s := buildMonth(t, "EURUSD", core.TFM15, monthStart("2026-03"), 2000)
	a, err := encodeSeries(s)
	if err != nil {
		t.Fatalf("encode a: %v", err)
	}
	b, err := encodeSeries(s)
	if err != nil {
		t.Fatalf("encode b: %v", err)
	}
	if !bytes.Equal(a, b) {
		t.Fatalf("identical input produced differing bytes: %d vs %d bytes", len(a), len(b))
	}
	if contentHash(a) != contentHash(b) {
		t.Fatal("content hashes differ for identical input")
	}
}

func TestDeterministicViaStore(t *testing.T) {
	ctx := context.Background()
	st := NewLocalStore(t.TempDir())
	s := buildMonth(t, "EURUSD", core.TFM1, monthStart("2026-08"), 3000)
	meta := Manifest{Symbol: "EURUSD", TF: core.TFM1, Month: "2026-08", Source: "dukascopy", BuildVersion: "test"}

	if err := st.Write(ctx, s, meta); err != nil {
		t.Fatalf("write 1: %v", err)
	}
	first, err := os.ReadFile(st.parquetPath("EURUSD", core.TFM1, "2026-08"))
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Write(ctx, s, meta); err != nil {
		t.Fatalf("write 2: %v", err)
	}
	second, err := os.ReadFile(st.parquetPath("EURUSD", core.TFM1, "2026-08"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("two writes of identical input produced different files")
	}
}

func TestCorruptionFailsHashCheck(t *testing.T) {
	ctx := context.Background()
	st := NewLocalStore(t.TempDir())
	s := buildMonth(t, "EURUSD", core.TFM1, monthStart("2026-08"), 4000)
	meta := Manifest{Symbol: "EURUSD", TF: core.TFM1, Month: "2026-08", Source: "dukascopy", BuildVersion: "test"}
	if err := st.Write(ctx, s, meta); err != nil {
		t.Fatalf("write: %v", err)
	}

	path := st.parquetPath("EURUSD", core.TFM1, "2026-08")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Flip a byte inside a data page (well past the file header).
	off := len(data) / 2
	data[off] ^= 0xFF
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	_, _, err = st.Load(ctx, "EURUSD", core.TFM1, core.MinTime, core.MaxTime)
	if !errors.Is(err, ErrHashMismatch) {
		t.Fatalf("expected ErrHashMismatch, got %v", err)
	}
}

func TestPartialRangeAcrossMonths(t *testing.T) {
	ctx := context.Background()
	st := NewLocalStore(t.TempDir())

	aug := buildMonth(t, "EURUSD", core.TFH1, monthStart("2026-08"), 24*20) // 20 days of H1
	sep := buildMonth(t, "EURUSD", core.TFH1, monthStart("2026-09"), 24*20)
	for _, s := range []*core.Series{aug, sep} {
		month := monthKey(s.Time[0])
		meta := Manifest{Symbol: "EURUSD", TF: core.TFH1, Month: month, Source: "dukascopy", BuildVersion: "test"}
		if err := st.Write(ctx, s, meta); err != nil {
			t.Fatalf("write %s: %v", month, err)
		}
	}

	// Window from mid-August to mid-September: touches both files, returns only
	// the windowed bars.
	from := aug.Time[24*10]
	to := sep.Time[24*10]
	got, manifests, err := st.Load(ctx, "EURUSD", core.TFH1, from, to)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(manifests) != 2 {
		t.Fatalf("expected 2 manifests touched, got %d", len(manifests))
	}
	first, last, ok := got.Bounds()
	if !ok {
		t.Fatal("empty result")
	}
	if first != from {
		t.Errorf("first bar %d, want %d", first, from)
	}
	if last >= to {
		t.Errorf("last bar %d not below window end %d (half-open)", last, to)
	}
	// Every returned bar must lie in [from, to).
	for i := 0; i < got.Len(); i++ {
		if got.Time[i] < from || got.Time[i] >= to {
			t.Fatalf("bar %d at %d outside window [%d,%d)", i, got.Time[i], from, to)
		}
	}

	// A window inside August alone touches only August's manifest.
	_, oneMonth, err := st.Load(ctx, "EURUSD", core.TFH1, aug.Time[0], aug.Time[24*5])
	if err != nil {
		t.Fatalf("load single month: %v", err)
	}
	if len(oneMonth) != 1 || oneMonth[0].Month != "2026-08" {
		t.Fatalf("expected only 2026-08 manifest, got %v", oneMonth)
	}
}

func TestLoadMissingMonthsSkipped(t *testing.T) {
	ctx := context.Background()
	st := NewLocalStore(t.TempDir())
	// Nothing written. A load over any window returns an empty series, no error.
	got, manifests, err := st.Load(ctx, "EURUSD", core.TFM1, monthStart("2026-01"), monthStart("2026-06"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.Len() != 0 {
		t.Errorf("expected empty series, got %d bars", got.Len())
	}
	if len(manifests) != 0 {
		t.Errorf("expected no manifests, got %d", len(manifests))
	}
}

func TestWriteRejectsMismatch(t *testing.T) {
	ctx := context.Background()
	st := NewLocalStore(t.TempDir())
	s := buildMonth(t, "EURUSD", core.TFM1, monthStart("2026-08"), 100)

	// Wrong month in manifest.
	err := st.Write(ctx, s, Manifest{Symbol: "EURUSD", TF: core.TFM1, Month: "2026-09"})
	if !errors.Is(err, ErrManifestMismatch) {
		t.Errorf("wrong month: expected ErrManifestMismatch, got %v", err)
	}
	// Wrong symbol.
	err = st.Write(ctx, s, Manifest{Symbol: "GBPUSD", TF: core.TFM1, Month: "2026-08"})
	if !errors.Is(err, ErrManifestMismatch) {
		t.Errorf("wrong symbol: expected ErrManifestMismatch, got %v", err)
	}
	// Empty series.
	empty := core.NewSeries("EURUSD", core.TFM1, 0)
	if err := st.Write(ctx, empty, Manifest{Symbol: "EURUSD", TF: core.TFM1, Month: "2026-08"}); !errors.Is(err, ErrEmptySeries) {
		t.Errorf("empty: expected ErrEmptySeries, got %v", err)
	}
}

func TestSchemaVersionRejected(t *testing.T) {
	ctx := context.Background()
	st := NewLocalStore(t.TempDir())
	s := buildMonth(t, "EURUSD", core.TFM1, monthStart("2026-08"), 100)
	meta := Manifest{Symbol: "EURUSD", TF: core.TFM1, Month: "2026-08", Source: "x", BuildVersion: "y"}
	if err := st.Write(ctx, s, meta); err != nil {
		t.Fatal(err)
	}
	// Rewrite the manifest with a future schema version.
	mfPath := st.manifestPath("EURUSD", core.TFM1, "2026-08")
	bad := Manifest{Symbol: "EURUSD", TF: core.TFM1, Month: "2026-08", Rows: int64(s.Len()),
		First: s.Time[0], Last: s.Time[s.Len()-1], ContentHash: "deadbeef", SchemaVersion: 999}
	b, _ := bad.encode()
	if err := os.WriteFile(mfPath, b, 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := st.Load(ctx, "EURUSD", core.TFM1, core.MinTime, core.MaxTime)
	if !errors.Is(err, ErrSchemaVersion) {
		t.Fatalf("expected ErrSchemaVersion, got %v", err)
	}
}

func TestManifestJSONRoundTrip(t *testing.T) {
	m := Manifest{
		Symbol: "EURUSD", TF: core.TFM15, Month: "2026-08",
		Rows: 1234, First: 1000, Last: 2000,
		ContentHash: "abc123", Source: "dukascopy", BuildVersion: "pipeline-1.2.3",
		SchemaVersion: SchemaVersion,
	}
	b, err := m.encode()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(b, []byte(`"tf": "m15"`)) {
		t.Errorf("timeframe not serialised as canonical name:\n%s", b)
	}
	var back Manifest
	if err := back.UnmarshalJSON(b); err != nil {
		t.Fatal(err)
	}
	if back != m {
		t.Fatalf("round trip differs:\n got %+v\n want %+v", back, m)
	}
}

func TestSnapshotHashStableAcrossOrder(t *testing.T) {
	ms := []Manifest{
		{Symbol: "EURUSD", TF: core.TFM1, Month: "2026-08", Rows: 10, First: 1, Last: 9, ContentHash: "h1", SchemaVersion: 1},
		{Symbol: "EURUSD", TF: core.TFM1, Month: "2026-09", Rows: 11, First: 10, Last: 19, ContentHash: "h2", SchemaVersion: 1},
		{Symbol: "BTCUSDT", TF: core.TFH4, Month: "2026-08", Rows: 12, First: 2, Last: 8, ContentHash: "h3", SchemaVersion: 1},
	}
	reordered := []Manifest{ms[2], ms[0], ms[1]}

	if SnapshotHash(ms) != SnapshotHash(reordered) {
		t.Fatal("SnapshotHash not stable across manifest ordering")
	}
	// SnapshotHash must not mutate the caller's slice order.
	if ms[0].Symbol != "EURUSD" {
		t.Fatal("SnapshotHash mutated the input slice")
	}
	// A change to any data-identifying field changes the hash.
	changed := append([]Manifest(nil), ms...)
	changed[0].ContentHash = "different"
	if SnapshotHash(ms) == SnapshotHash(changed) {
		t.Fatal("SnapshotHash unchanged after content hash change")
	}
	// Provenance-only changes do NOT change the snapshot: identical data from a
	// different build is the same snapshot.
	prov := append([]Manifest(nil), ms...)
	for i := range prov {
		prov[i].Source = "other-venue"
		prov[i].BuildVersion = "pipeline-9.9.9"
	}
	if SnapshotHash(ms) != SnapshotHash(prov) {
		t.Fatal("SnapshotHash changed on provenance-only difference")
	}
}

// TestStoreDeterminism is the per-batch determinism test: the same write/load
// cycle run twice yields byte-identical files and identical loaded values.
func TestStoreDeterminism(t *testing.T) {
	ctx := context.Background()
	run := func() ([]byte, *core.Series) {
		st := NewLocalStore(t.TempDir())
		s := buildMonth(t, "EURUSD", core.TFM5, monthStart("2026-08"), 2500)
		meta := Manifest{Symbol: "EURUSD", TF: core.TFM5, Month: "2026-08", Source: "dukascopy", BuildVersion: "test"}
		if err := st.Write(ctx, s, meta); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(st.parquetPath("EURUSD", core.TFM5, "2026-08"))
		if err != nil {
			t.Fatal(err)
		}
		got, _, err := st.Load(ctx, "EURUSD", core.TFM5, core.MinTime, core.MaxTime)
		if err != nil {
			t.Fatal(err)
		}
		return data, got
	}
	d1, s1 := run()
	d2, s2 := run()
	if !bytes.Equal(d1, d2) {
		t.Fatal("determinism: files differ across runs")
	}
	assertSeriesEqual(t, s1, s2)
}

func TestPathLayout(t *testing.T) {
	st := NewLocalStore("/root")
	if p := st.parquetPath("EURUSD", core.TFM1, "2026-08"); p != filepath.FromSlash("/root/EURUSD/m1/2026-08.parquet") {
		t.Errorf("unexpected parquet path %q", p)
	}
	if p := st.manifestPath("EURUSD", core.TFM1, "2026-08"); p != filepath.FromSlash("/root/EURUSD/m1/2026-08.manifest.json") {
		t.Errorf("unexpected manifest path %q", p)
	}
}
