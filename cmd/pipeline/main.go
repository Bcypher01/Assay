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

	"github.com/Bcypher01/Assay/internal/core"
	"github.com/Bcypher01/Assay/internal/instrument"
	"github.com/Bcypher01/Assay/internal/pipeline"
	"github.com/Bcypher01/Assay/internal/venue"
	"github.com/Bcypher01/Assay/internal/venue/binance"
	"github.com/Bcypher01/Assay/internal/venue/yahoo"
)

func main() {
	if err := loadDotEnv(".env"); err != nil {
		log.Fatalf("load .env: %v", err)
	}

	publisherKind := flag.String("publisher", env("ASSAY_PUBLISHER", "r2"), "publisher backend: r2 or local")
	localDir := flag.String("local-dir", env("ASSAY_LOCAL_PUBLISH_DIR", "published"), "local publisher directory")
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
	symbols, err := pipelineSymbols(reg, *symbolsFlag)
	if err != nil {
		log.Fatal(err)
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
			"dukascopy": &yahoo.Client{
				BaseURL:  env("ASSAY_YAHOO_CHART_BASE_URL", "https://query1.finance.yahoo.com/v8/finance/chart/"),
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
	report, err := (pipeline.Runner{Fetcher: fetcher, Publisher: pub}).Run(ctx, job)
	if err != nil {
		log.Fatal(err)
	}
	if *reconcile {
		if err := pipeline.Reconcile(ctx, fetcher, pub, job); err != nil {
			log.Fatal(err)
		}
	}
	fmt.Printf("published=%d skipped=%d\n", report.Published, report.Skipped)
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
	spec, err := reg.Get(symbol)
	if err != nil {
		return nil, err
	}
	if spec.Venue != "dukascopy" {
		return requested, nil
	}
	out := make([]core.TF, 0, len(requested))
	for _, tf := range requested {
		if tf == core.TFD1 || tf == core.TFW1 {
			out = append(out, tf)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s is FX daily mode only; request d1 and/or w1", symbol)
	}
	return out, nil
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
			os.Setenv(key, val)
		}
	}
	return sc.Err()
}
