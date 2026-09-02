package library

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/bcypher01/assay/internal/core"
	"github.com/bcypher01/assay/internal/instrument"
	"github.com/bcypher01/assay/internal/store"
	"github.com/bcypher01/assay/internal/venue"
)

// ErrUnsupportedResolution reports a strategy requesting a timeframe finer
// than the published library supports for an instrument.
var ErrUnsupportedResolution = errors.New("library: unsupported resolution")

// ErrDataNotReady reports that a run asked for data that is not fully present
// in the local cache. Preparation may fetch that data; execution must not.
var ErrDataNotReady = errors.New("library: data not ready")

// UnsupportedResolutionError names the failing instrument and timeframe.
type UnsupportedResolutionError struct {
	Symbol string
	TF     core.TF
}

func (e *UnsupportedResolutionError) Error() string {
	return fmt.Sprintf("%v: %s for %s", ErrUnsupportedResolution, e.TF, e.Symbol)
}

func (e *UnsupportedResolutionError) Unwrap() error { return ErrUnsupportedResolution }

// Request is the exact data window a strategy needs after requirement
// derivation.
type Request struct {
	Symbols []string
	TFs     []core.TF
	From    core.Time
	To      core.Time
}

// Progress is shaped for future RPC streaming.
type Progress struct {
	// Percent tracks completed cache partitions, not guessed transfer bytes.
	// Some sources cannot disclose a content length, but the requested months
	// are known before work begins, so this remains an honest overall measure.
	Percent     uint32
	Stage       string
	BytesDone   int64
	BytesTotal  int64
	CurrentFile string
}

// Readiness is the cache-only answer to whether an exact request can run.
type Readiness int

const (
	ReadinessUnspecified Readiness = iota
	ReadinessReady
	ReadinessPartial
	ReadinessMissing
	ReadinessSourceUnavailable
	ReadinessCorrupt
	ReadinessUnsupported
)

// MissingPartition names a cache partition required before a request can run.
type MissingPartition struct {
	Symbol string
	TF     core.TF
	Month  string
	From   core.Time
	To     core.Time
}

// Status describes local readiness without contacting object storage or a
// venue. It is safe to call from Run preflight.
type Status struct {
	Readiness          Readiness
	RequestedFrom      core.Time
	RequestedTo        core.Time
	CachedFrom         core.Time
	CachedTo           core.Time
	MissingPartitions  []MissingPartition
	Snapshot           string
	Sources            []string
	ExternalRequired   bool
	LastSuccessfulPrep core.Time
	ErrorCode          string
	Err                error
}

// DataNotReadyError carries the status that prevented cache-only execution.
type DataNotReadyError struct {
	Status Status
}

func (e *DataNotReadyError) Error() string {
	if e.Status.ErrorCode != "" {
		return fmt.Sprintf("%v: %s", ErrDataNotReady, e.Status.ErrorCode)
	}
	return ErrDataNotReady.Error()
}

func (e *DataNotReadyError) Unwrap() error { return ErrDataNotReady }

// Window is reserved for future fine-resolution escalation.
type Window struct {
	From core.Time
	To   core.Time
}

// ObjectStore is the read side of the published library object store.
type ObjectStore interface {
	Head(ctx context.Context, key string) (store.Manifest, bool, error)
	Get(ctx context.Context, key string) (io.ReadCloser, bool, error)
}

type sourceNamer interface {
	Source(sym string) (string, error)
}

// Loader resolves published library objects into the local cache.
type Loader struct {
	Cache    *store.LocalStore
	Objects  ObjectStore
	Fallback venue.Fetcher
	Registry *instrument.Registry
}

// New returns a Loader using cache as the local data manager.
func New(cache *store.LocalStore, objects ObjectStore, fallback venue.Fetcher) *Loader {
	return &Loader{Cache: cache, Objects: objects, Fallback: fallback}
}

// DeriveRequest turns strategy requirements into a concrete data request and
// enforces the published-library resolution policy.
func DeriveRequest(reg *instrument.Registry, symbols []string, needed []core.TF, from, to core.Time) (Request, error) {
	if len(symbols) == 0 {
		return Request{}, fmt.Errorf("library: no instruments requested")
	}
	if len(needed) == 0 {
		return Request{}, fmt.Errorf("library: no timeframes requested")
	}
	if to <= from {
		return Request{}, fmt.Errorf("library: invalid window")
	}
	if reg == nil {
		var err error
		reg, err = instrument.Default()
		if err != nil {
			return Request{}, err
		}
	}
	tfs := uniqueTFs(needed)
	for _, sym := range symbols {
		for _, tf := range tfs {
			if !supports(reg, sym, tf) {
				return Request{}, &UnsupportedResolutionError{Symbol: sym, TF: tf}
			}
		}
	}
	return Request{Symbols: append([]string(nil), symbols...), TFs: tfs, From: from, To: to}, nil
}

// DeriveRequest is the Loader-bound form, using l.Registry.
func (l *Loader) DeriveRequest(symbols []string, needed []core.TF, from, to core.Time) (Request, error) {
	return DeriveRequest(l.Registry, symbols, needed, from, to)
}

// Ensure makes every object named by r available in the local cache. A warm
// valid cache returns before touching object storage.
func (l *Loader) Ensure(ctx context.Context, r Request, progress func(Progress)) error {
	if l.Cache == nil {
		return fmt.Errorf("library: missing local cache")
	}
	if len(r.Symbols) == 0 || len(r.TFs) == 0 {
		return fmt.Errorf("library: empty request")
	}
	if r.To <= r.From {
		return fmt.Errorf("library: invalid window")
	}
	if err := l.checkSupported(r); err != nil {
		return err
	}
	report(progress, Progress{Stage: "checking local cache"})
	type task struct {
		symbol string
		tf     core.TF
		month  monthWindow
	}
	var tasks []task
	for _, sym := range r.Symbols {
		for _, tf := range r.TFs {
			missing, err := l.missingMonths(ctx, sym, tf, r.From, r.To)
			if err != nil && len(missing) == 0 {
				missing = months(r.From, r.To)
			}
			if err != nil && l.Objects == nil && l.Fallback == nil {
				return err
			}
			for _, month := range missing {
				tasks = append(tasks, task{symbol: sym, tf: tf, month: month})
			}
		}
	}
	if len(tasks) == 0 {
		report(progress, Progress{Percent: 100, Stage: "data already available"})
		return nil
	}
	for i, task := range tasks {
		if err := ctx.Err(); err != nil {
			return err
		}
		start := uint32(i * 100 / len(tasks))
		forward := func(p Progress) {
			p.Percent = start
			report(progress, p)
		}
		if err := l.ensureMonth(ctx, task.symbol, task.tf, task.month, forward); err != nil {
			return err
		}
		report(progress, Progress{
			Percent:     uint32((i + 1) * 100 / len(tasks)),
			Stage:       "preparing data",
			CurrentFile: objectKey(task.symbol, task.tf, task.month.name),
		})
	}
	return nil
}

// Status checks whether r is fully available in the local cache. Unlike Ensure,
// it never contacts object storage or a fallback venue.
func (l *Loader) Status(ctx context.Context, r Request) Status {
	status := Status{
		Readiness:        ReadinessMissing,
		RequestedFrom:    r.From,
		RequestedTo:      r.To,
		ExternalRequired: true,
	}
	if l.Cache == nil {
		status.ErrorCode = "CACHE_NOT_CONFIGURED"
		status.Err = fmt.Errorf("library: missing local cache")
		return status
	}
	if len(r.Symbols) == 0 || len(r.TFs) == 0 {
		status.ErrorCode = "EMPTY_REQUEST"
		status.Err = fmt.Errorf("library: empty request")
		return status
	}
	if r.To <= r.From {
		status.ErrorCode = "INVALID_WINDOW"
		status.Err = fmt.Errorf("library: invalid window")
		return status
	}
	if err := l.checkSupported(r); err != nil {
		status.Readiness = ReadinessUnsupported
		status.ErrorCode = "UNSUPPORTED_RESOLUTION"
		status.Err = err
		return status
	}

	var allManifests []store.Manifest
	sourceSet := map[string]bool{}
	for _, sym := range r.Symbols {
		for _, tf := range r.TFs {
			_, manifests, err := l.Cache.Load(ctx, sym, tf, r.From, r.To)
			if err != nil {
				status.Readiness = ReadinessCorrupt
				status.ErrorCode = "CACHE_CORRUPT"
				status.Err = err
				return status
			}
			allManifests = append(allManifests, manifests...)
			for _, mf := range manifests {
				if status.CachedFrom == 0 || mf.First < status.CachedFrom {
					status.CachedFrom = mf.First
				}
				if mf.Last > status.CachedTo {
					status.CachedTo = mf.Last
				}
				if mf.Source != "" {
					sourceSet[mf.Source] = true
				}
				if mf.Last > status.LastSuccessfulPrep {
					status.LastSuccessfulPrep = mf.Last
				}
			}
			current := make([]store.Manifest, 0, len(manifests))
			for _, mf := range manifests {
				if !l.staleManifest(sym, mf) {
					current = append(current, mf)
				}
			}
			missing := missingPartitions(sym, tf, r.From, r.To, current, l.calendarFor(sym))
			status.MissingPartitions = append(status.MissingPartitions, missing...)
		}
	}
	status.Snapshot = store.SnapshotHash(allManifests)
	for source := range sourceSet {
		status.Sources = append(status.Sources, source)
	}
	sort.Strings(status.Sources)
	switch {
	case len(status.MissingPartitions) == 0 && len(allManifests) > 0:
		status.Readiness = ReadinessReady
		status.ExternalRequired = false
		status.ErrorCode = ""
	case len(allManifests) > 0:
		status.Readiness = ReadinessPartial
		status.ErrorCode = "DATA_PARTIAL"
	default:
		status.Readiness = ReadinessMissing
		status.ErrorCode = "DATA_MISSING"
	}
	return status
}

// Load reads from the local cache.
func (l *Loader) Load(ctx context.Context, sym string, tf core.TF, from, to core.Time) (*core.Series, []store.Manifest, error) {
	if l.Cache == nil {
		return nil, nil, fmt.Errorf("library: missing local cache")
	}
	return l.Cache.Load(ctx, sym, tf, from, to)
}

// EscalateWindows is intentionally a stub until fine-resolution slices are
// published. It preserves the batch interface without pretending support.
func (l *Loader) EscalateWindows(ctx context.Context, sym string, w []Window, target core.TF) error {
	return &UnsupportedResolutionError{Symbol: sym, TF: target}
}

func (l *Loader) ensureMonth(ctx context.Context, sym string, tf core.TF, month monthWindow, progress func(Progress)) error {
	key := objectKey(sym, tf, month.name)
	if l.Objects != nil {
		meta, ok, err := l.Objects.Head(ctx, key)
		if err == nil && ok && !l.staleManifest(sym, meta) {
			err := l.downloadMonth(ctx, key, meta, progress)
			if err == nil {
				return nil
			}
			if errors.Is(err, store.ErrHashMismatch) {
				return l.downloadMonth(ctx, key, meta, progress)
			}
			if l.Fallback == nil {
				return err
			}
		}
		if err != nil && l.Fallback == nil {
			return err
		}
	}
	return l.fetchFallback(ctx, sym, tf, month, progress)
}

func (l *Loader) downloadMonth(ctx context.Context, key string, meta store.Manifest, progress func(Progress)) error {
	report(progress, Progress{Stage: "downloading", CurrentFile: key})
	rc, ok, err := l.Objects.Get(ctx, key)
	if err != nil {
		return err
	}
	if !ok {
		return fmt.Errorf("library: object disappeared: %s", key)
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		return err
	}
	report(progress, Progress{Stage: "verifying", BytesDone: int64(len(data)), BytesTotal: int64(len(data)), CurrentFile: key})
	if err := store.VerifyPublished(data, meta); err != nil {
		return err
	}
	return l.Cache.WritePublished(ctx, data, meta)
}

func (l *Loader) fetchFallback(ctx context.Context, sym string, tf core.TF, month monthWindow, progress func(Progress)) error {
	if l.Fallback == nil {
		return fmt.Errorf("library: %s %s %s unavailable and no fallback configured", sym, tf, month.name)
	}
	report(progress, Progress{Stage: "downloading", CurrentFile: objectKey(sym, tf, month.name)})
	// Cache objects are calendar-month partitions even when the user asks for
	// an exact rolling window beginning midway through a candle. Fetching only
	// the clamped request can otherwise produce an empty first partition (for
	// example, a D1 request starting Friday at 09:35). The final Load still
	// slices the cached month to the caller's exact range.
	partitionFrom := core.FromStd(firstOfMonth(month.from.Std()))
	partitionTo := core.FromStd(firstOfMonth(month.from.Std()).AddDate(0, 1, 0))
	series, err := l.Fallback.Fetch(ctx, sym, tf, partitionFrom, partitionTo)
	if err != nil {
		return fmt.Errorf("library: fallback fetch %s %s %s: %w", sym, tf, month.name, err)
	}
	if series == nil || series.Len() == 0 {
		return fmt.Errorf("library: fallback fetch %s %s %s: %w", sym, tf, month.name, venue.ErrUnavailable)
	}
	source := l.Fallback.Name()
	if namer, ok := l.Fallback.(sourceNamer); ok {
		source, err = namer.Source(sym)
		if err != nil {
			return err
		}
	}
	meta := store.Manifest{Symbol: sym, TF: tf, Month: month.name, Source: source, BuildVersion: "fallback"}
	return l.Cache.Write(ctx, series, meta)
}

func (l *Loader) missingMonths(ctx context.Context, sym string, tf core.TF, from, to core.Time) ([]monthWindow, error) {
	_, manifests, err := l.Cache.Load(ctx, sym, tf, from, to)
	if err != nil {
		return months(from, to), err
	}
	byMonth := make(map[string]store.Manifest, len(manifests))
	for _, mf := range manifests {
		byMonth[mf.Month] = mf
	}
	var missing []monthWindow
	for _, month := range months(from, to) {
		mf, ok := byMonth[month.name]
		calendar := l.calendarFor(sym)
		if !ok {
			if _, _, requiresBars := requiredBounds(month, tf, calendar); !requiresBars {
				continue
			}
			missing = append(missing, month)
			continue
		}
		if l.staleManifest(sym, mf) || !manifestCovers(mf, month, tf, calendar) {
			missing = append(missing, month)
		}
	}
	return missing, nil
}

// staleManifest identifies known pre-migration FX sources whose candles must
// not survive a feed switch. Unknown source names remain valid so fixture,
// private, and user-supplied libraries keep working.
func (l *Loader) staleManifest(symbol string, mf store.Manifest) bool {
	if l.Registry == nil {
		return false
	}
	spec, err := l.Registry.Get(symbol)
	if err != nil || spec.Source != "dukascopy-bid" || mf.Source == spec.Source {
		return false
	}
	switch mf.Source {
	case "yahoo", "yahoo-fx", "stooq-fx", "twelve-data-composite", "dukascopy", "venue-router":
		return true
	default:
		return false
	}
}

func missingPartitions(sym string, tf core.TF, from, to core.Time, manifests []store.Manifest, calendar *instrument.Calendar) []MissingPartition {
	byMonth := make(map[string]store.Manifest, len(manifests))
	for _, mf := range manifests {
		byMonth[mf.Month] = mf
	}
	var missing []MissingPartition
	for _, month := range months(from, to) {
		mf, ok := byMonth[month.name]
		if !ok {
			if _, _, requiresBars := requiredBounds(month, tf, calendar); !requiresBars {
				continue
			}
			missing = append(missing, MissingPartition{
				Symbol: sym,
				TF:     tf,
				Month:  month.name,
				From:   month.from,
				To:     month.to,
			})
			continue
		}
		if !manifestCovers(mf, month, tf, calendar) {
			missing = append(missing, MissingPartition{
				Symbol: sym,
				TF:     tf,
				Month:  month.name,
				From:   month.from,
				To:     month.to,
			})
		}
	}
	return missing
}

func (l *Loader) calendarFor(symbol string) *instrument.Calendar {
	if l.Registry == nil {
		return nil
	}
	spec, err := l.Registry.Get(symbol)
	if err != nil {
		return nil
	}
	return spec.Calendar
}

func manifestCovers(mf store.Manifest, month monthWindow, tf core.TF, calendar *instrument.Calendar) bool {
	firstRequired, lastRequired, requiresBars := requiredBounds(month, tf, calendar)
	if !requiresBars {
		return true
	}
	if calendar != nil {
		// Instrument calendars describe regular sessions, but do not enumerate
		// every historical exchange/bank holiday. A complete daily partition can
		// therefore begin or end a few calendar days inside its nominal boundary;
		// intraday FX also closes early around dates such as New Year's Eve. Keep
		// this allowance small so a genuinely truncated download remains detectable.
		const holidayBoundaryAllowance = core.Time(4 * 24 * time.Hour / time.Millisecond)
		return mf.First <= firstRequired+holidayBoundaryAllowance && mf.Last >= lastRequired-holidayBoundaryAllowance
	}
	return mf.First <= firstRequired && mf.Last >= lastRequired
}

// requiredBounds returns the first and last bars expected in a partition.
// Daily instruments with market hours do not print a bar for every UTC
// calendar day (for example, FX is closed on weekends). Checking the calendar
// at noon UTC identifies whether that trading date should have a daily bar
// without treating the Sunday/Monday session boundary as a missing bar.
func requiredBounds(month monthWindow, tf core.TF, calendar *instrument.Calendar) (core.Time, core.Time, bool) {
	if calendar == nil {
		step := core.Time(tf.Millis())
		first := tf.AlignDown(month.from)
		if first < month.from {
			first += step
		}
		last := tf.AlignDown(month.to - 1)
		if last < first {
			// The exact partial window contains no bar boundary for this
			// timeframe, so it cannot require a stored observation.
			return 0, 0, false
		}
		return first, last, true
	}
	if tf != core.TFD1 {
		step := core.Time(tf.Millis())
		firstBucket := tf.AlignDown(month.from)
		if firstBucket < month.from {
			firstBucket += step
		}
		lastBucket := tf.AlignDown(month.to - 1)
		if lastBucket < firstBucket {
			return 0, 0, false
		}
		firstOpen, ok := calendar.NextOpen(firstBucket)
		if !ok || firstOpen >= month.to {
			return 0, 0, false
		}
		lastOpen, ok := calendar.PreviousOpen(month.to - core.Time(time.Minute/time.Millisecond))
		if !ok || lastOpen < firstBucket {
			return 0, 0, false
		}
		return tf.AlignDown(firstOpen), tf.AlignDown(lastOpen), true
	}

	day := month.from.Std().Truncate(24 * time.Hour)
	end := month.to.Std()
	var first, last core.Time
	found := false
	for ; day.Before(end); day = day.AddDate(0, 0, 1) {
		bar := core.FromStd(day)
		if !calendar.IsOpen(core.FromStd(day.Add(12 * time.Hour))) {
			continue
		}
		if !found {
			first = bar
			found = true
		}
		last = bar
	}
	if !found {
		// A closed-only window does not require a daily bar. A present manifest
		// is therefore sufficient.
		return 0, 0, false
	}
	return first, last, true
}

func (l *Loader) checkSupported(r Request) error {
	for _, sym := range r.Symbols {
		for _, tf := range r.TFs {
			if !supports(l.Registry, sym, tf) {
				return &UnsupportedResolutionError{Symbol: sym, TF: tf}
			}
		}
	}
	return nil
}

func supports(reg *instrument.Registry, sym string, tf core.TF) bool {
	if reg == nil {
		var err error
		reg, err = instrument.Default()
		if err != nil {
			return false
		}
	}
	spec, err := reg.Get(sym)
	if err != nil {
		return false
	}
	// Dukascopy is backed by M1 data and the adapter can either serve M1
	// directly or aggregate it into every advertised coarser timeframe. Keep
	// this policy in step with dukascopy.Client.SupportedTFs; the old generic FX
	// branch below incorrectly limited these instruments to D1/W1.
	if spec.Venue == "dukascopy" {
		switch tf {
		case core.TFM1, core.TFM5, core.TFM15, core.TFM30, core.TFH1, core.TFH4, core.TFD1, core.TFW1:
			return spec.Quality.Supports(tf)
		default:
			return false
		}
	}
	switch spec.Class {
	case instrument.ClassCrypto:
		return tf == core.TFM15 || tf == core.TFM30 || tf == core.TFH1 || tf == core.TFH4 || tf == core.TFD1 || tf == core.TFW1
	case instrument.ClassFX:
		return tf == core.TFD1 || tf == core.TFW1
	default:
		return tf == core.TFD1 || tf == core.TFW1
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

func objectKey(symbol string, tf core.TF, month string) string {
	return filepath.ToSlash(filepath.Join(symbol, tf.String(), month+".parquet"))
}

func uniqueTFs(in []core.TF) []core.TF {
	seen := make(map[core.TF]bool, len(in))
	out := make([]core.TF, 0, len(in))
	for _, tf := range in {
		if !tf.Valid() || seen[tf] {
			continue
		}
		seen[tf] = true
		out = append(out, tf)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Millis() == out[j].Millis() {
			return strings.Compare(out[i].String(), out[j].String()) < 0
		}
		return out[i].Finer(out[j])
	})
	return out
}

func report(progress func(Progress), p Progress) {
	if progress != nil {
		progress(p)
	}
}
