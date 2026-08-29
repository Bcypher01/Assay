package main

import (
	"flag"
	"log"
	"net/http"

	"github.com/Bcypher01/Assay/internal/event"
	"github.com/Bcypher01/Assay/internal/instrument"
	"github.com/Bcypher01/Assay/internal/library"
	"github.com/Bcypher01/Assay/internal/rpc"
	"github.com/Bcypher01/Assay/internal/runstore"
	"github.com/Bcypher01/Assay/internal/store"
	"github.com/Bcypher01/Assay/internal/telemetry"
	"github.com/Bcypher01/Assay/internal/venue"
	"github.com/Bcypher01/Assay/internal/venue/binance"
	"github.com/Bcypher01/Assay/internal/venue/dukascopy"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8080", "local HTTP listen address")
	cacheDir := flag.String("cache-dir", "cache", "local bar-cache directory")
	cacheMaxMB := flag.Int64("cache-max-mb", 5120, "maximum local bar-cache size in MiB; zero disables eviction")
	historyDir := flag.String("history-dir", "history", "persistent study history directory")
	eventDir := flag.String("event-dir", "event-data", "immutable event-snapshot directory")
	libraryURL := flag.String("library-url", "", "public read-only R2/CDN library URL")
	metricsDir := flag.String("metrics-dir", "", "opt-in local-only product metrics directory; no telemetry is recorded when empty")
	flag.Parse()
	registry, err := instrument.Default()
	if err != nil {
		log.Fatal(err)
	}
	cache := store.NewLocalStoreWithMaxBytes(*cacheDir, *cacheMaxMB<<20)
	history, err := runstore.Open(*historyDir)
	if err != nil {
		log.Fatal(err)
	}
	var objects library.ObjectStore
	if *libraryURL != "" {
		objects = &library.HTTPStore{BaseURL: *libraryURL}
	}
	// The public-library URL is preferred when configured. The venue router lets
	// a new local installation use real public historical data immediately when
	// an object has not yet been published to that library.
	fallback := &venue.Router{Registry: registry, Fetchers: map[string]venue.Fetcher{
		"binance":   &binance.Client{BaseURL: "https://data.binance.vision", Registry: registry},
		"dukascopy": &dukascopy.Client{BaseURL: "https://datafeed.dukascopy.com/datafeed", Registry: registry},
	}}
	loader := library.New(cache, objects, fallback)
	loader.Registry = registry
	server, err := rpc.NewLocalServerWithEventServices(cache, registry, loader, history, event.NewStore(*eventDir))
	if err != nil {
		log.Fatal(err)
	}
	if *metricsDir != "" {
		recorder, err := telemetry.Open(*metricsDir)
		if err != nil {
			log.Fatal(err)
		}
		server.Recorder = recorder
		log.Printf("Local-only product metrics enabled at %s; Assay will not send them anywhere", *metricsDir)
	}
	log.Printf("Assay listening on http://%s", *addr)
	if err := http.ListenAndServe(*addr, rpc.NewHandler(server)); err != nil {
		log.Fatal(err)
	}
}
