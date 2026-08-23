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

	"github.com/olumide/assay/internal/core"
	"github.com/olumide/assay/internal/instrument"
	"github.com/olumide/assay/internal/store"
	"github.com/olumide/assay/internal/venue"
)

// ErrUnsupportedResolution reports a strategy requesting a timeframe finer
// than the published library supports for an instrument.
var ErrUnsupportedResolution = errors.New("library: unsupported resolution")

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
	Stage       string
	BytesDone   int64
	BytesTotal  int64
	CurrentFile string
}

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
	report(progress, Progress{Stage: "resolving"})
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
				if err := ctx.Err(); err != nil {
					return err
				}
				if err := l.ensureMonth(ctx, sym, tf, month, progress); err != nil {
					return err
				}
			}
		}
	}
	return nil
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
		if err == nil && ok {
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
	report(progress, Progress{Stage: "downloading", BytesTotal: meta.Rows, CurrentFile: key})
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
	series, err := l.Fallback.Fetch(ctx, sym, tf, month.from, month.to)
	if err != nil {
		return fmt.Errorf("library: fallback fetch %s %s %s: %w", sym, tf, month.name, err)
	}
	if series.Len() == 0 {
		return nil
	}
	meta := store.Manifest{Symbol: sym, TF: tf, Month: month.name, Source: l.Fallback.Name(), BuildVersion: "fallback"}
	return l.Cache.Write(ctx, series, meta)
}

func (l *Loader) missingMonths(ctx context.Context, sym string, tf core.TF, from, to core.Time) ([]monthWindow, error) {
	_, manifests, err := l.Cache.Load(ctx, sym, tf, from, to)
	if err != nil {
		return months(from, to), err
	}
	have := make(map[string]bool, len(manifests))
	for _, mf := range manifests {
		have[mf.Month] = true
	}
	var missing []monthWindow
	for _, month := range months(from, to) {
		if !have[month.name] {
			missing = append(missing, month)
		}
	}
	return missing, nil
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
