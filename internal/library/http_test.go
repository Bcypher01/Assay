package library

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Bcypher01/Assay/internal/core"
	"github.com/Bcypher01/Assay/internal/store"
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
