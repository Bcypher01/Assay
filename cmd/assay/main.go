package main

import (
	"flag"
	"log"
	"net/http"

	"github.com/Bcypher01/Assay/internal/instrument"
	"github.com/Bcypher01/Assay/internal/rpc"
	"github.com/Bcypher01/Assay/internal/store"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8080", "local HTTP listen address")
	cacheDir := flag.String("cache-dir", "cache", "local bar-cache directory")
	flag.Parse()
	registry, err := instrument.Default()
	if err != nil {
		log.Fatal(err)
	}
	server, err := rpc.NewLocalServer(store.NewLocalStore(*cacheDir), registry)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Assay listening on http://%s", *addr)
	if err := http.ListenAndServe(*addr, rpc.NewHandler(server)); err != nil {
		log.Fatal(err)
	}
}
