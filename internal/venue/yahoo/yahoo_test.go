package yahoo

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Bcypher01/Assay/internal/core"
	"github.com/Bcypher01/Assay/internal/instrument"
)

func TestFetchDailyFXNormalisesChartJSON(t *testing.T) {
	body := `{"chart":{"result":[{"timestamp":[1704067200,1704153600],"indicators":{"quote":[{"open":[1.1,1.1025],"high":[1.105,1.11],"low":[1.095,1.1],"close":[1.1025,1.108],"volume":[0,0]}]}}],"error":null}}`
	var gotPath, gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotQuery = r.URL.RawQuery
		w.Write([]byte(body))
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, Registry: instrument.MustDefault()}
	got, err := c.Fetch(context.Background(), "DUKAS:EURUSD", core.TFD1,
		core.MustParseTime("2024-01-01T00:00:00Z"),
		core.MustParseTime("2024-02-01T00:00:00Z"))
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if gotPath != "/EURUSD=X" || !strings.Contains(gotQuery, "interval=1d") {
		t.Fatalf("unexpected request path=%q query=%q", gotPath, gotQuery)
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

func TestFetchWidensInvalidYahooRange(t *testing.T) {
	body := `{"chart":{"result":[{"timestamp":[1786921200],"indicators":{"quote":[{"open":[1.1575547456741333],"high":[1.1613997220993042],"low":[1.1575547456741333],"close":[1.1573940515518188],"volume":[0]}]}}],"error":null}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(body))
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, Registry: instrument.MustDefault()}
	got, err := c.Fetch(context.Background(), "DUKAS:EURUSD", core.TFD1,
		core.MustParseTime("2026-08-01T00:00:00Z"),
		core.MustParseTime("2026-09-01T00:00:00Z"))
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if got.Len() != 1 {
		t.Fatalf("got %d bars, want 1", got.Len())
	}
	if got.Low[0] != got.Close[0] {
		t.Fatalf("low should have widened to close: low=%d close=%d", got.Low[0], got.Close[0])
	}
}

func TestFetchWeeklyMergesObservationsAlignedToSameWeek(t *testing.T) {
	body := `{"chart":{"result":[{"timestamp":[1711324800,1711843200],"indicators":{"quote":[{"open":[1.08,1.09],"high":[1.10,1.12],"low":[1.07,1.08],"close":[1.09,1.11],"volume":[2,3]}]}}],"error":null}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(body))
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL, Registry: instrument.MustDefault()}
	got, err := c.Fetch(context.Background(), "DUKAS:EURUSD", core.TFW1,
		core.MustParseTime("2024-03-01T00:00:00Z"),
		core.MustParseTime("2024-04-01T00:00:00Z"))
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if got.Len() != 1 {
		t.Fatalf("got %d bars, want one merged weekly bar", got.Len())
	}
	if got.Open[0] != 108000 || got.High[0] != 112000 || got.Low[0] != 107000 || got.Close[0] != 111000 || got.Volume[0] != 5 {
		t.Fatalf("merged weekly bar = O:%d H:%d L:%d C:%d V:%d", got.Open[0], got.High[0], got.Low[0], got.Close[0], got.Volume[0])
	}
}
