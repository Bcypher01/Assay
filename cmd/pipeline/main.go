package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/bcypher01/assay/internal/core"
	"github.com/bcypher01/assay/internal/instrument"
	"github.com/bcypher01/assay/internal/pipeline"
	"github.com/bcypher01/assay/internal/venue"
	"github.com/bcypher01/assay/internal/venue/binance"
	"github.com/bcypher01/assay/internal/venue/dukascopy"
)

func main() {
	if err := loadDotEnv(".env"); err != nil {
		log.Fatalf("load .env: %v", err)
	}

	publisherKind := flag.String("publisher", env("ASSAY_PUBLISHER", "r2"), "publisher backend: r2 or local")
	localDir := flag.String("local-dir", env("ASSAY_LOCAL_PUBLISH_DIR", "data/published"), "local publisher directory")
	symbolsFlag := flag.String("symbols", env("ASSAY_PIPELINE_SYMBOLS", ""), "comma-separated registry symbols")
	tfsFlag := flag.String("tfs", env("ASSAY_PIPELINE_TFS", "m15,m30,h1,h4,d1,w1"), "comma-separated timeframes")
	fromFlag := flag.String("from", env("ASSAY_PIPELINE_FROM", ""), "inclusive UTC date, YYYY-MM-DD")
	toFlag := flag.String("to", env("ASSAY_PIPELINE_TO", ""), "exclusive UTC date, YYYY-MM-DD")
	buildVersion := flag.String("build-version", env("ASSAY_BUILD_VERSION", "dev"), "pipeline build version")
	reconcile := flag.Bool("reconcile", env("ASSAY_RECONCILE", "true") == "true", "reconcile published objects against venue-native aggregates")
	flag.Parse()

	ctx := context.Background()
	reg, err := instrument.Default()
	if err != nil {
		log.Fatal(err)
	}
	requested, err := pipelineSymbols(reg, *symbolsFlag)
	if err != nil {
		log.Fatal(err)
	}
	symbols, withheld := publishableSymbols(reg, requested, *publisherKind,
		env("ASSAY_DUKASCOPY_REDISTRIBUTION_AUTHORIZED", "false") == "true")
	if len(withheld) > 0 {
		log.Printf("withholding %d instrument(s) from the published library: %s — no confirmed redistribution rights, so clients fetch these directly from the venue",
			len(withheld), strings.Join(withheld, ", "))
	}
	if len(symbols) == 0 {
		log.Fatal("no instruments are licensed for publication; grant redistribution rights or choose a publishable venue")
	}
	tfs, err := parseTFs(*tfsFlag)
	if err != nil {
		log.Fatal(err)
	}
	from, to, err := parseWindow(*fromFlag, *toFlag)
	if err != nil {
		log.Fatal(err)
	}

	pub, err := publisher(*publisherKind, *localDir)
	if err != nil {
		log.Fatal(err)
	}
	fetcher := &venue.Router{
		Registry: reg,
		Fetchers: map[string]venue.Fetcher{
			"binance": &binance.Client{
				BaseURL:  env("ASSAY_BINANCE_DATA_BASE_URL", "https://data.binance.vision"),
				Registry: reg,
			},
			"dukascopy": &dukascopy.Client{
				BaseURL:  env("ASSAY_DUKASCOPY_BASE_URL", "https://datafeed.dukascopy.com/datafeed"),
				Registry: reg,
			},
		},
	}

	job := pipeline.Job{
		Symbols: symbols,
		TFs:     tfs,
		TFsForSymbol: func(symbol string, requested []core.TF) ([]core.TF, error) {
			return tfsForSymbol(reg, symbol, requested)
		},
		From:         from,
		To:           to,
		BuildVersion: *buildVersion,
	}
	report, err := (pipeline.Runner{
		Fetcher:   fetcher,
		Publisher: pub,
		Progress: func(progress pipeline.Progress) {
			log.Printf("%s %s %s: %s", progress.Symbol, progress.TF, progress.Month, progress.Stage)
		},
	}).Run(ctx, job)
	if err != nil {
		log.Fatal(err)
	}
	if *reconcile {
		if err := pipeline.Reconcile(ctx, fetcher, pub, job); err != nil {
			log.Fatal(err)
		}
	}
	fmt.Printf("published=%d skipped=%d withheld=%d\n", report.Published, report.Skipped, len(withheld))
}

func publisher(kind, localDir string) (pipeline.Publisher, error) {
	switch kind {
	case "local":
		return pipeline.NewLocalPublisher(localDir), nil
	case "r2":
		return pipeline.NewR2Publisher(pipeline.R2Config{
			AccountID:       os.Getenv("ASSAY_R2_ACCOUNT_ID"),
			AccessKeyID:     os.Getenv("ASSAY_R2_ACCESS_KEY_ID"),
			SecretAccessKey: os.Getenv("ASSAY_R2_SECRET_ACCESS_KEY"),
			Bucket:          os.Getenv("ASSAY_R2_BUCKET"),
			Endpoint:        os.Getenv("ASSAY_R2_ENDPOINT"),
			Region:          os.Getenv("ASSAY_R2_REGION"),
		}, nil)
	default:
		return nil, fmt.Errorf("unknown publisher %q", kind)
	}
}

func pipelineSymbols(reg *instrument.Registry, explicit string) ([]string, error) {
	if explicit != "" {
		return splitCSV(explicit), nil
	}
	var out []string
	for _, sym := range reg.Symbols() {
		spec, err := reg.Get(sym)
		if err != nil {
			return nil, err
		}
		if spec.Venue == "binance" || spec.Venue == "dukascopy" {
			out = append(out, sym)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no Binance instruments configured")
	}
	return out, nil
}

// publishableSymbols splits the requested instruments into those the operator
// is licensed to publish to shared object storage and those withheld for
// licensing reasons. The local filesystem publisher is a development tool, not
// a distribution channel, so it publishes everything. Dukascopy FX is withheld
// from R2 until redistribution rights are confirmed; a withheld instrument stays
// available to clients through the direct-venue fallback, so smaller libraries
// and unlicensed venues degrade to a slower path rather than an empty one.
func publishableSymbols(reg *instrument.Registry, symbols []string, publisherKind string, dukascopyAuthorized bool) (publishable, withheld []string) {
	for _, symbol := range symbols {
		if publisherKind == "r2" && !dukascopyAuthorized && isVenue(reg, symbol, "dukascopy") {
			withheld = append(withheld, symbol)
			continue
		}
		publishable = append(publishable, symbol)
	}
	return publishable, withheld
}

func isVenue(reg *instrument.Registry, symbol, venueName string) bool {
	spec, err := reg.Get(symbol)
	return err == nil && spec.Venue == venueName
}

func parseTFs(s string) ([]core.TF, error) {
	parts := splitCSV(s)
	out := make([]core.TF, 0, len(parts))
	for _, part := range parts {
		tf, err := core.ParseTF(part)
		if err != nil {
			return nil, err
		}
		out = append(out, tf)
	}
	return out, nil
}

func tfsForSymbol(reg *instrument.Registry, symbol string, requested []core.TF) ([]core.TF, error) {
	_, err := reg.Get(symbol)
	if err != nil {
		return nil, err
	}
	return requested, nil
}

func parseWindow(fromS, toS string) (core.Time, core.Time, error) {
	if fromS == "" {
		return 0, 0, fmt.Errorf("ASSAY_PIPELINE_FROM or -from is required")
	}
	from, err := parseDate(fromS)
	if err != nil {
		return 0, 0, err
	}
	var to core.Time
	if toS == "" {
		now := time.Now().UTC()
		y, m, _ := now.Date()
		to = core.FromStd(time.Date(y, m, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 1, 0))
	} else {
		to, err = parseDate(toS)
		if err != nil {
			return 0, 0, err
		}
	}
	if to <= from {
		return 0, 0, fmt.Errorf("pipeline window end must be after start")
	}
	return from, to, nil
}

func parseDate(s string) (core.Time, error) {
	t, err := time.ParseInLocation("2006-01-02", s, time.UTC)
	if err != nil {
		return 0, err
	}
	return core.FromStd(t), nil
}

func splitCSV(s string) []string {
	raw := strings.Split(s, ",")
	out := make([]string, 0, len(raw))
	for _, part := range raw {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func loadDotEnv(path string) error {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.Trim(strings.TrimSpace(val), `"'`)
		if key != "" && os.Getenv(key) == "" {
			if err := os.Setenv(key, val); err != nil {
				return fmt.Errorf("setting %s from env file: %w", key, err)
			}
		}
	}
	return sc.Err()
}
