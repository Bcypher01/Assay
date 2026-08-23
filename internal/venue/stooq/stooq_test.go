package stooq

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/olumide/assay/internal/core"
	"github.com/olumide/assay/internal/instrument"
)

func TestFetchDailyFXNormalisesToTicks(t *testing.T) {
	csv := strings.Join([]string{
		"Date,Open,High,Low,Close,Volume",
		"2024-01-01,1.10000,1.10500,1.09500,1.10250,0",
		"2024-01-02,1.10250,1.11000,1.10000,1.10800,0",
		"",
	}, "\n")
	var rawQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawQuery = r.URL.RawQuery
		w.Write([]byte(csv))
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, Registry: instrument.MustDefault()}
	got, err := c.Fetch(context.Background(), "DUKAS:EURUSD", core.TFD1,
		core.MustParseTime("2024-01-01T00:00:00Z"),
		core.MustParseTime("2024-02-01T00:00:00Z"))
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if !strings.Contains(rawQuery, "s=eurusd") || !strings.Contains(rawQuery, "i=d") {
		t.Fatalf("unexpected query %q", rawQuery)
	}
	if got.Len() != 2 {
		t.Fatalf("got %d bars, want 2", got.Len())
	}
	if got.Open[0] != 110000 || got.High[0] != 110500 || got.Low[0] != 109500 || got.Close[0] != 110250 {
		t.Fatalf("first bar not converted to EURUSD ticks: %+v", got)
	}
}

func TestFetchRejectsIntradayFX(t *testing.T) {
	c := &Client{Registry: instrument.MustDefault()}
	_, err := c.Fetch(context.Background(), "DUKAS:EURUSD", core.TFH1,
		core.MustParseTime("2024-01-01T00:00:00Z"),
		core.MustParseTime("2024-02-01T00:00:00Z"))
	if err == nil || !strings.Contains(err.Error(), "d1 and w1") {
		t.Fatalf("expected d1/w1 error, got %v", err)
	}
}
