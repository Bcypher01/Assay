package pipeline

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/Bcypher01/Assay/internal/core"
	"github.com/Bcypher01/Assay/internal/store"
	"github.com/Bcypher01/Assay/internal/venue"
)

// Job is one pipeline build request.
type Job struct {
	Symbols      []string
	TFs          []core.TF
	TFsForSymbol func(symbol string, requested []core.TF) ([]core.TF, error)
	From         core.Time
	To           core.Time
	BuildVersion string
}

// Report summarises a pipeline run.
type Report struct {
	Published int
	Skipped   int
}

// Progress describes one monthly partition as it moves through the pipeline.
type Progress struct {
	Symbol string
	TF     core.TF
	Month  string
	Stage  string
}

// Runner fetches, normalises, encodes, and publishes monthly bar objects.
type Runner struct {
	Fetcher   venue.Fetcher
	Publisher Publisher
	Now       func() time.Time
	Progress  func(Progress)
}

type sourceNamer interface {
	Source(sym string) (string, error)
}

// Run executes a seed or incremental publication pass. Complete historical
// months are immutable and skipped when already present with the same build
// version; the current UTC month is always rewritten.
func (r Runner) Run(ctx context.Context, job Job) (Report, error) {
	if r.Fetcher == nil {
		return Report{}, fmt.Errorf("pipeline: missing fetcher")
	}
	if r.Publisher == nil {
		return Report{}, fmt.Errorf("pipeline: missing publisher")
	}
	if len(job.Symbols) == 0 || len(job.TFs) == 0 {
		return Report{}, fmt.Errorf("pipeline: no instruments or timeframes")
	}
	if job.BuildVersion == "" {
		return Report{}, fmt.Errorf("pipeline: missing build version")
	}
	if job.To <= job.From {
		return Report{}, fmt.Errorf("pipeline: invalid window")
	}

	now := time.Now().UTC()
	if r.Now != nil {
		now = r.Now().UTC()
	}
	currentMonth := now.Format("2006-01")

	var report Report
	for _, sym := range job.Symbols {
		source := r.Fetcher.Name()
		if sn, ok := r.Fetcher.(sourceNamer); ok {
			var err error
			source, err = sn.Source(sym)
			if err != nil {
				return report, err
			}
		}
		tfs := job.TFs
		if job.TFsForSymbol != nil {
			var err error
			tfs, err = job.TFsForSymbol(sym, job.TFs)
			if err != nil {
				return report, err
			}
		}
		for _, tf := range tfs {
			for _, month := range months(job.From, job.To) {
				if err := ctx.Err(); err != nil {
					return report, err
				}
				key := ObjectKey(sym, tf, month.name)
				r.progress(sym, tf, month.name, "checking")
				existing, ok, err := r.Publisher.Head(ctx, key)
				if err != nil {
					return report, err
				}
				if ok && month.name != currentMonth && existing.BuildVersion == job.BuildVersion && existing.Source == source {
					report.Skipped++
					r.progress(sym, tf, month.name, "skipped")
					continue
				}
				r.progress(sym, tf, month.name, "fetching")
				series, err := r.Fetcher.Fetch(ctx, sym, tf, month.from, month.to)
				if err != nil {
					return report, fmt.Errorf("pipeline: fetch %s %s %s: %w", sym, tf, month.name, err)
				}
				if series.Len() == 0 {
					report.Skipped++
					r.progress(sym, tf, month.name, "empty")
					continue
				}
				meta := store.Manifest{Symbol: sym, TF: tf, Month: month.name, Source: source, BuildVersion: job.BuildVersion}
				data, completed, err := store.EncodeForPublish(series, meta)
				if err != nil {
					return report, fmt.Errorf("pipeline: encode %s: %w", key, err)
				}
				if err := r.Publisher.Put(ctx, key, bytes.NewReader(data), completed); err != nil {
					return report, err
				}
				report.Published++
				r.progress(sym, tf, month.name, "published")
			}
		}
	}
	return report, nil
}

func (r Runner) progress(symbol string, tf core.TF, month, stage string) {
	if r.Progress != nil {
		r.Progress(Progress{Symbol: symbol, TF: tf, Month: month, Stage: stage})
	}
}

type monthWindow struct {
	name     string
	from, to core.Time
}

func months(from, to core.Time) []monthWindow {
	start := firstOfMonth(from.Std())
	end := firstOfMonth((to - 1).Std())
	var out []monthWindow
	for cur := start; !cur.After(end); cur = cur.AddDate(0, 1, 0) {
		next := cur.AddDate(0, 1, 0)
		mFrom := core.FromStd(cur)
		mTo := core.FromStd(next)
		if mFrom < from {
			mFrom = from
		}
		if mTo > to {
			mTo = to
		}
		out = append(out, monthWindow{name: cur.Format("2006-01"), from: mFrom, to: mTo})
	}
	return out
}

func firstOfMonth(t time.Time) time.Time {
	y, m, _ := t.UTC().Date()
	return time.Date(y, m, 1, 0, 0, 0, 0, time.UTC)
}
