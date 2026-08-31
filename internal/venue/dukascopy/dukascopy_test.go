package dukascopy

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ulikunitz/xz/lzma"

	"github.com/bcypher01/assay/internal/core"
	"github.com/bcypher01/assay/internal/instrument"
)

func TestFetchDailyBI5(t *testing.T) {
	body := encodeBI5(t,
		rawBar{0, 103503, 103507, 103462, 103577, 100},
		rawBar{86400, 103508, 103600, 103400, 103700, 200},
		rawBar{172800, 103600, 103600, 103600, 103600, 0},
	)
	var requested2025 atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/EURUSD/2025/BID_candles_day_1.bi5" {
			requested2025.Store(true)
			_, _ = w.Write(body)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, Registry: dukaRegistry(t)}
	got, err := c.Fetch(context.Background(), "DUKAS:EURUSD", core.TFD1,
		core.MustParseTime("2025-01-01T00:00:00Z"), core.MustParseTime("2025-01-04T00:00:00Z"))
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if !requested2025.Load() {
		t.Fatal("2025 annual artifact was not requested")
	}
	if got.Len() != 2 || got.Open[0] != 103503 || got.High[0] != 103577 || got.Low[0] != 103462 || got.Close[0] != 103507 {
		t.Fatalf("unexpected decoded daily series: %+v", got)
	}
}

func TestDecodeCapturedDailyWireRecords(t *testing.T) {
	// First two EURUSD records from Dukascopy's 2024 annual BID artifact. The
	// bytes are captured independently of rawBar/encodeBI5 so a matching mistake
	// in the fixture encoder cannot make the decoder test pass.
	raw, err := hex.DecodeString("000000000001af5b0001af1d0001af140001af6f45753f5c" +
		"000151800001af1e0001ab680001ab3f0001af6048875d2a")
	if err != nil {
		t.Fatal(err)
	}
	body := compressRaw(t, raw)
	got, err := decodeBI5("DUKAS:EURUSD", core.TFD1, body,
		time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
		core.MustParseTime("2024-01-01T00:00:00Z"), core.MustParseTime("2024-01-03T00:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	if got.Len() != 2 {
		t.Fatalf("decoded %d captured bars, want 2", got.Len())
	}
	if got.Open[0] != 110427 || got.Close[0] != 110365 || got.Low[0] != 110356 || got.High[0] != 110447 || got.Volume[0] != 3924 {
		t.Fatalf("first captured bar decoded incorrectly: %+v", got)
	}
	if got.Open[1] != 110366 || got.Close[1] != 109416 || got.Low[1] != 109375 || got.High[1] != 110432 || got.Volume[1] != 277225 {
		t.Fatalf("second captured bar decoded incorrectly: %+v", got)
	}
}

func TestFetchAggregatesHourlyFromDailyMinuteArtifacts(t *testing.T) {
	day := encodeBI5(t,
		rawBar{0, 110000, 110010, 109990, 110020, 10},
		rawBar{3600, 110015, 110000, 109980, 110025, 30},
	)
	var paths []string
	var pathsMu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pathsMu.Lock()
		paths = append(paths, r.URL.Path)
		pathsMu.Unlock()
		if r.URL.Path == "/EURUSD/2025/00/02/BID_candles_min_1.bi5" {
			_, _ = w.Write(day)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, Registry: dukaRegistry(t)}
	got, err := c.Fetch(context.Background(), "DUKAS:EURUSD", core.TFH1,
		core.MustParseTime("2025-01-02T00:00:00Z"), core.MustParseTime("2025-01-03T01:00:00Z"))
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if got.Len() != 2 {
		t.Fatalf("got %d hourly bars, want 2: %+v", got.Len(), got)
	}
	if got.Open[0] != 110000 || got.High[0] != 110020 || got.Low[0] != 109990 || got.Close[0] != 110010 || got.Volume[0] != 10 {
		t.Fatalf("first hourly candle is wrong: %+v", got)
	}
	foundDailyMinuteArtifact := false
	for _, path := range paths {
		if path == "/EURUSD/2025/00/02/BID_candles_min_1.bi5" {
			foundDailyMinuteArtifact = true
		}
	}
	if !foundDailyMinuteArtifact {
		t.Fatalf("daily minute artifact was not requested: %v", paths)
	}
}

func TestFetchDailyFallsBackToMonthlyHourlyArtifact(t *testing.T) {
	hourly := encodeBI5(t,
		rawBar{0, 110000, 110010, 109990, 110020, 10},
		rawBar{3600, 110010, 110015, 110005, 110030, 20},
	)
	priorYear := encodeBI5(t, rawBar{364 * 86400, 109900, 110000, 109800, 110100, 5})
	var minuteRequested atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/EURUSD/2025/BID_candles_day_1.bi5" {
			_, _ = w.Write(priorYear)
			return
		}
		if r.URL.Path == "/EURUSD/2026/00/01/BID_candles_min_1.bi5" {
			minuteRequested.Store(true)
			_, _ = w.Write(hourly)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, Registry: dukaRegistry(t)}
	got, err := c.Fetch(context.Background(), "DUKAS:EURUSD", core.TFD1,
		core.MustParseTime("2026-01-01T00:00:00Z"), core.MustParseTime("2026-01-02T00:00:00Z"))
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if !minuteRequested.Load() {
		t.Fatal("daily M1 fallback artifact was not requested")
	}
	if got.Len() != 1 || got.Open[0] != 110000 || got.High[0] != 110030 || got.Low[0] != 109990 || got.Close[0] != 110015 || got.Volume[0] != 30 {
		t.Fatalf("unexpected daily aggregate: %+v", got)
	}
}

func TestFetchHourlyFromDailyMinuteArtifact(t *testing.T) {
	minutes := encodeBI5(t,
		rawBar{0, 110000, 110010, 109990, 110020, 10},
		rawBar{60, 110010, 110015, 110005, 110030, 20},
	)
	var minuteRequested atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/EURUSD/2026/00/02/BID_candles_min_1.bi5" {
			minuteRequested.Store(true)
			_, _ = w.Write(minutes)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, Registry: dukaRegistry(t)}
	got, err := c.Fetch(context.Background(), "DUKAS:EURUSD", core.TFH1,
		core.MustParseTime("2026-01-02T00:00:00Z"), core.MustParseTime("2026-01-02T01:00:00Z"))
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if !minuteRequested.Load() || got.Len() != 1 || got.Open[0] != 110000 || got.High[0] != 110030 || got.Low[0] != 109990 || got.Close[0] != 110015 {
		t.Fatalf("minute fallback result: requested=%v series=%+v", minuteRequested.Load(), got)
	}
}

func TestFetchSkipsClosedSaturdayArtifact(t *testing.T) {
	friday := encodeBI5(t,
		rawBar{0, 110000, 110010, 109990, 110020, 10},
	)
	var saturdayRequested atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/EURUSD/2025/00/03/BID_candles_min_1.bi5":
			_, _ = w.Write(friday)
		case "/EURUSD/2025/00/04/BID_candles_min_1.bi5":
			saturdayRequested.Store(true)
			http.Error(w, "temporarily unavailable", http.StatusServiceUnavailable)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, Registry: dukaRegistry(t)}
	got, err := c.Fetch(context.Background(), "DUKAS:EURUSD", core.TFH4,
		core.MustParseTime("2025-01-03T00:00:00Z"), core.MustParseTime("2025-01-05T00:00:00Z"))
	if err != nil {
		t.Fatalf("fetch across closed Saturday: %v", err)
	}
	if saturdayRequested.Load() {
		t.Fatal("closed Saturday artifact was requested")
	}
	if got.Len() != 1 || got.Open[0] != 110000 || got.Close[0] != 110010 {
		t.Fatalf("unexpected Friday aggregate: %+v", got)
	}
}

func TestFoldSundayIntoMonday(t *testing.T) {
	src := core.NewSeries("DUKAS:EURUSD", core.TFD1, 2)
	sunday := core.MustParseTime("2025-01-05T00:00:00Z")
	monday := core.MustParseTime("2025-01-06T00:00:00Z")
	src.Append(sunday, 100, 120, 90, 110, 10)
	src.Append(monday, 110, 130, 100, 125, 20)

	got := foldSundayIntoMonday(src)
	if got.Len() != 1 || got.Time[0] != monday || got.Open[0] != 100 || got.High[0] != 130 || got.Low[0] != 90 || got.Close[0] != 125 || got.Volume[0] != 30 {
		t.Fatalf("unexpected folded candle: %+v", got)
	}
}

func TestDecodeRejectsMalformedCandle(t *testing.T) {
	body := encodeBI5(t, rawBar{0, 110000, 110100, 109900, 110050, 1})
	_, err := decodeBI5("DUKAS:EURUSD", core.TFM1, body,
		time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), core.MinTime, core.MaxTime)
	if err == nil || !strings.Contains(err.Error(), "malformed BI5 bar") {
		t.Fatalf("expected malformed candle error, got %v", err)
	}
}

func TestMirrorURLAlternatesOnlyOfficialHosts(t *testing.T) {
	primary := "https://datafeed.dukascopy.com/datafeed/EURUSD/file.bi5"
	if got := mirrorURL(primary, 1); got != "https://www.dukascopy.com/datafeed/EURUSD/file.bi5" {
		t.Fatalf("mirror=%q", got)
	}
	if got := mirrorURL(primary, 2); got != primary {
		t.Fatalf("even retry should use primary, got %q", got)
	}
	local := "http://127.0.0.1/datafeed/EURUSD/file.bi5"
	if got := mirrorURL(local, 1); got != local {
		t.Fatalf("custom endpoint changed to %q", got)
	}
}

func TestMirrorNotFoundDoesNotHidePrimaryArtifact(t *testing.T) {
	primary := "https://datafeed.dukascopy.com/datafeed/EURUSD/file.bi5"
	var calls atomic.Int32
	client := &Client{HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		call := calls.Add(1)
		status := http.StatusServiceUnavailable
		body := "busy"
		if call == 2 {
			status, body = http.StatusNotFound, "missing on mirror"
		}
		if call == 3 {
			status, body = http.StatusOK, "artifact"
		}
		return &http.Response{
			StatusCode: status,
			Status:     http.StatusText(status),
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
		}, nil
	})}}
	body, err := client.download(context.Background(), primary, 0)
	if err != nil || string(body) != "artifact" || calls.Load() != 3 {
		t.Fatalf("body=%q calls=%d err=%v", body, calls.Load(), err)
	}
}

func TestFetchArtifactsRetriesFailedDayWithoutCancelingMonth(t *testing.T) {
	body := encodeBI5(t, rawBar{0, 110000, 110010, 109990, 110020, 10})
	var calls atomic.Int32
	client := &Client{
		HTTPClient: &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			call := calls.Add(1)
			status, responseBody := http.StatusServiceUnavailable, "busy"
			if call == 7 {
				status, responseBody = http.StatusOK, string(body)
			}
			return &http.Response{
				StatusCode: status,
				Status:     fmt.Sprintf("%d %s", status, http.StatusText(status)),
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(responseBody)),
			}, nil
		})},
		retryDelay: func(int, string) time.Duration { return 0 },
	}
	base := time.Date(2020, 2, 2, 0, 0, 0, 0, time.UTC)
	got, err := client.fetchArtifacts(context.Background(), "DUKAS:EURUSD", core.TFM1,
		[]artifact{{url: "https://datafeed.dukascopy.com/datafeed/EURUSD/file.bi5", base: base}},
		core.FromStd(base), core.FromStd(base.AddDate(0, 0, 1)))
	if err != nil {
		t.Fatalf("recovery pass failed: %v", err)
	}
	if calls.Load() != 7 || got.Len() != 1 {
		t.Fatalf("calls=%d series=%+v", calls.Load(), got)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

type rawBar struct {
	seconds                uint32
	open, close, low, high uint32
	volume                 float32
}

func encodeBI5(t *testing.T, bars ...rawBar) []byte {
	t.Helper()
	var raw bytes.Buffer
	for _, bar := range bars {
		for _, value := range []uint32{bar.seconds, bar.open, bar.close, bar.low, bar.high, math.Float32bits(bar.volume)} {
			if err := binary.Write(&raw, binary.BigEndian, value); err != nil {
				t.Fatal(err)
			}
		}
	}
	return compressRaw(t, raw.Bytes())
}

func compressRaw(t *testing.T, raw []byte) []byte {
	t.Helper()
	var compressed bytes.Buffer
	w, err := lzma.NewWriter(&compressed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return compressed.Bytes()
}

func dukaRegistry(t *testing.T) *instrument.Registry {
	t.Helper()
	r, err := instrument.Load([]byte(`[{"symbol":"DUKAS:EURUSD","class":"fx","display":"EUR/USD","venue":"dukascopy","tickSize":"0.00001","contractSize":"100000","lotStep":"0.01","minLot":"0.01","quoteCurrency":"USD","settleCurrency":"USD","quality":"m1"}]`))
	if err != nil {
		t.Fatal(err)
	}
	return r
}
