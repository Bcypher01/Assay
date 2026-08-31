// event-pipeline imports a provider's normalised records into an immutable
// local event snapshot. The first provider is the checked-in reviewed catalogue.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"path/filepath"
	"time"

	"github.com/Bcypher01/Assay/internal/core"
	"github.com/Bcypher01/Assay/internal/event"
	"github.com/Bcypher01/Assay/internal/eventpipeline"
	"github.com/Bcypher01/Assay/internal/eventsource"
	"github.com/Bcypher01/Assay/internal/eventsource/bls"
	"github.com/Bcypher01/Assay/internal/eventsource/curated"
	"github.com/Bcypher01/Assay/internal/userdir"
)

func main() {
	// Snapshots are written where the assay server reads them from by default.
	defaultDir, err := userdir.Data("events")
	if err != nil {
		log.Fatal(err)
	}
	dir := flag.String("dir", defaultDir, "directory for immutable event snapshots")
	providerName := flag.String("provider", "bls-archive", "event provider: bls-archive, bls, or curated")
	archiveDir := flag.String("archive-dir", filepath.Join(defaultDir, "bls-archive"), "locally captured official BLS release documents")
	fromFlag := flag.String("from", "", "inclusive UTC date, YYYY-MM-DD")
	toFlag := flag.String("to", "", "exclusive UTC date, YYYY-MM-DD")
	buildVersion := flag.String("build-version", "dev", "pipeline build version")
	flag.Parse()

	scope, err := parseScope(*fromFlag, *toFlag)
	if err != nil {
		log.Fatal(err)
	}
	var provider eventsource.Provider
	switch *providerName {
	case "bls-archive":
		provider = bls.Archive{Dir: *archiveDir}
	case "bls":
		provider = &bls.Client{}
	case "curated":
		provider = curated.Provider{}
	default:
		log.Fatalf("unknown provider %q", *providerName)
	}
	mf, err := (eventpipeline.Runner{Provider: provider, Store: event.NewStore(*dir)}).Run(context.Background(), eventpipeline.Job{Scope: scope, BuildVersion: *buildVersion})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("snapshot=%s events=%d\n", mf.ContentHash, mf.Rows)
}

func parseScope(from, to string) (eventsource.Scope, error) {
	var scope eventsource.Scope
	var err error
	if from == "" && to == "" {
		now := time.Now().UTC()
		scope.From = core.FromStd(now.AddDate(-10, 0, 0))
		scope.To = core.FromStd(now.AddDate(0, 0, 1))
		return scope, nil
	}
	if from != "" {
		scope.From, err = parseDate(from)
		if err != nil {
			return scope, err
		}
	}
	if to != "" {
		scope.To, err = parseDate(to)
		if err != nil {
			return scope, err
		}
	}
	if scope.From != 0 && scope.To != 0 && scope.To <= scope.From {
		return scope, fmt.Errorf("event pipeline window end must be after start")
	}
	return scope, nil
}

func parseDate(value string) (core.Time, error) {
	t, err := time.ParseInLocation("2006-01-02", value, time.UTC)
	if err != nil {
		return 0, err
	}
	return core.FromStd(t), nil
}
