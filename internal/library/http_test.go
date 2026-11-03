package library

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/bcypher01/assay/internal/core"
	"github.com/bcypher01/assay/internal/store"
)

func TestHTTPStoreReadsPublicObjectsAndManifests(t *testing.T) {
	manifest := store.Manifest{Symbol: "BINANCE:BTCUSDT", TF: core.TFM15, Month: "2025-01", SchemaVersion: store.SchemaVersion}
	mb, err := manifest.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/library/BINANCE:BTCUSDT/m15/2025-01.manifest.json":
			_, _ = w.Write(mb)
		case "/library/BINANCE:BTCUSDT/m15/2025-01.parquet":
			_, _ = w.Write([]byte("parquet"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer s.Close()
	objects := &HTTPStore{BaseURL: s.URL + "/library"}
	got, ok, err := objects.Head(context.Background(), "BINANCE:BTCUSDT/m15/2025-01.parquet")
	if err != nil || !ok || got.Month != manifest.Month {
		t.Fatalf("Head = %#v, %v, %v", got, ok, err)
	}
	rc, ok, err := objects.Get(context.Background(), "BINANCE:BTCUSDT/m15/2025-01.parquet")
	if err != nil || !ok {
		t.Fatalf("Get: %v, %v", ok, err)
	}
	rc.Close()
	if _, _, err := objects.Get(context.Background(), "../secret"); err == nil {
		t.Fatal("unsafe path accepted")
	}
}

func TestHTTPStoreRetriesTransientThenSucceeds(t *testing.T) {
	var hits int
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("parquet"))
	}))
	defer s.Close()
	objects := &HTTPStore{BaseURL: s.URL, Backoff: time.Millisecond}
	rc, ok, err := objects.Get(context.Background(), "BINANCE:BTCUSDT/m15/2025-01.parquet")
	if err != nil || !ok {
		t.Fatalf("Get: ok=%v err=%v", ok, err)
	}
	rc.Close()
	if hits != 3 {
		t.Fatalf("hits=%d, want 3 (two transient failures then success)", hits)
	}
}

func TestHTTPStoreDoesNotRetryNotFound(t *testing.T) {
	var hits int
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		http.NotFound(w, r)
	}))
	defer s.Close()
	objects := &HTTPStore{BaseURL: s.URL, Backoff: time.Millisecond}
	rc, ok, err := objects.Get(context.Background(), "BINANCE:BTCUSDT/m15/2025-01.parquet")
	if err != nil || ok || rc != nil {
		t.Fatalf("Get = %v, %v, %v; want a clean miss", rc, ok, err)
	}
	if hits != 1 {
		t.Fatalf("hits=%d, want 1 (a 404 is a definitive answer)", hits)
	}
}

func TestHTTPStoreExhaustedRetriesReturnsError(t *testing.T) {
	var hits int
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer s.Close()
	objects := &HTTPStore{BaseURL: s.URL, Retries: 2, Backoff: time.Millisecond}
	if _, _, err := objects.Get(context.Background(), "x.parquet"); err == nil {
		t.Fatal("expected an error after exhausting retries")
	}
	if hits != 2 {
		t.Fatalf("hits=%d, want 2", hits)
	}
}

func TestHTTPStoreDelay(t *testing.T) {
	s := &HTTPStore{Backoff: time.Millisecond}
	if got := s.delay(0, "2"); got != 2*time.Second {
		t.Fatalf("Retry-After override = %s, want 2s", got)
	}
	if got := s.delay(0, "nonsense"); got != time.Millisecond {
		t.Fatalf("unusable Retry-After should fall back to base, got %s", got)
	}
	if got := s.delay(1, ""); got != 2*time.Millisecond {
		t.Fatalf("attempt 1 = %s, want 2ms", got)
	}
	if got := s.delay(20, ""); got != maxRetryBackoff {
		t.Fatalf("backoff should cap at %s, got %s", maxRetryBackoff, got)
	}
}
