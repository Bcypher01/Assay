package binance

import (
	"archive/zip"
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bcypher01/assay/internal/core"
	"github.com/bcypher01/assay/internal/instrument"
)

func TestFetchMonthlyKlinesNormalisesToTicks(t *testing.T) {
	zipBody := zipCSV(t, strings.Join([]string{
		"open_time,open,high,low,close,volume",
		"1704067200000,42000.00,42010.00,41990.00,42005.00,12.5",
		"1704067260000,42005.00,42020.00,42000.00,42015.00,13.9",
		"",
	}, "\n"))
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/zip")
		w.Write(zipBody)
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, Registry: instrument.MustDefault()}
	from := core.MustParseTime("2024-01-01T00:00:00Z")
	to := core.MustParseTime("2024-02-01T00:00:00Z")
	got, err := c.Fetch(context.Background(), "BINANCE:BTCUSDT", core.TFM1, from, to)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if gotPath != "/data/spot/monthly/klines/BTCUSDT/1m/BTCUSDT-1m-2024-01.zip" {
		t.Fatalf("unexpected path %q", gotPath)
	}
	if got.Len() != 2 {
		t.Fatalf("got %d bars, want 2", got.Len())
	}
	if got.Open[0] != 4200000 || got.High[0] != 4201000 || got.Low[0] != 4199000 || got.Close[0] != 4200500 {
		t.Fatalf("first bar prices not converted to BTCUSDT cents: %+v", got)
	}
	if got.Volume[0] != 12 || got.Volume[1] != 13 {
		t.Fatalf("volumes got %d,%d", got.Volume[0], got.Volume[1])
	}
}

func TestFetchMonthlyKlinesAcceptsMicrosecondTimestamps(t *testing.T) {
	zipBody := zipCSV(t, strings.Join([]string{
		"open_time,open,high,low,close,volume",
		"1748736000000000,2500.00,2510.00,2490.00,2505.00,12.5",
		"1748739600000000,2505.00,2520.00,2500.00,2515.00,13.9",
		"",
	}, "\n"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write(zipBody)
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, Registry: instrument.MustDefault()}
	got, err := c.Fetch(context.Background(), "BINANCE:ETHUSDT", core.TFH1,
		core.MustParseTime("2025-06-01T00:00:00Z"),
		core.MustParseTime("2025-06-02T00:00:00Z"))
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if got.Len() != 2 || got.Time[0] != 1748736000000 || got.Time[1] != 1748739600000 {
		t.Fatalf("microsecond timestamps were not normalised: %+v", got.Time)
	}
}

func TestFetchRejectsOutOfOrderRows(t *testing.T) {
	zipBody := zipCSV(t, strings.Join([]string{
		"1704067260000,42005.00,42020.00,42000.00,42015.00,13",
		"1704067200000,42000.00,42010.00,41990.00,42005.00,12",
		"",
	}, "\n"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(zipBody)
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, Registry: instrument.MustDefault()}
	_, err := c.Fetch(context.Background(), "BINANCE:BTCUSDT", core.TFM1,
		core.MustParseTime("2024-01-01T00:00:00Z"),
		core.MustParseTime("2024-02-01T00:00:00Z"))
	if err == nil || !strings.Contains(err.Error(), "out-of-order") {
		t.Fatalf("expected out-of-order error, got %v", err)
	}
}

func TestFetchFallsBackToDailyWhenMonthlyMissing(t *testing.T) {
	zipBody := zipCSV(t, "1704067200000,42000.00,42010.00,41990.00,42005.00,12\n")
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if strings.Contains(r.URL.Path, "/monthly/") {
			http.NotFound(w, r)
			return
		}
		w.Write(zipBody)
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, Registry: instrument.MustDefault()}
	got, err := c.Fetch(context.Background(), "BINANCE:BTCUSDT", core.TFM1,
		core.MustParseTime("2024-01-01T00:00:00Z"),
		core.MustParseTime("2024-01-02T00:00:00Z"))
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if got.Len() != 1 {
		t.Fatalf("got %d bars, want 1", got.Len())
	}
	if len(paths) != 2 || !strings.Contains(paths[0], "/monthly/") || !strings.Contains(paths[1], "/daily/") {
		t.Fatalf("unexpected paths: %v", paths)
	}
}

func TestFetchReportsUnavailableWhenNeitherArchiveHasBars(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, Registry: instrument.MustDefault()}
	_, err := c.Fetch(context.Background(), "BINANCE:BTCUSDT", core.TFH1,
		core.MustParseTime("2026-07-01T00:00:00Z"),
		core.MustParseTime("2026-08-01T00:00:00Z"))
	if err == nil || !strings.Contains(err.Error(), "no h1 bars for BINANCE:BTCUSDT in 2026-07") {
		t.Fatalf("expected an actionable unavailable-data error, got %v", err)
	}
}

func zipCSV(t *testing.T, csv string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	f, err := zw.Create("klines.csv")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write([]byte(csv)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
