package store

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/olumide/assay/internal/core"
)

// Reader loads bars for a symbol, timeframe and time window, returning the
// series and the manifests of the files it touched. It is the interface batch
// 03 (the library client) and the engine consume.
type Reader interface {
	Load(ctx context.Context, sym string, tf core.TF, from, to core.Time) (*core.Series, []Manifest, error)
}

// Writer persists a single month of bars plus its manifest. It is the interface
// the pipeline binary uses to publish the library.
type Writer interface {
	Write(ctx context.Context, s *core.Series, meta Manifest) error
}

// LocalStore is a filesystem-backed Reader and Writer over the cache layout
//
//	<root>/<symbol>/<tf>/<YYYY-MM>.parquet
//	<root>/<symbol>/<tf>/<YYYY-MM>.manifest.json
//
// One month per file: coarse enough to keep file counts small, fine enough that
// fetching two years pulls 24 independently cacheable files.
type LocalStore struct {
	root string
}

// NewLocalStore returns a store rooted at dir.
func NewLocalStore(dir string) *LocalStore { return &LocalStore{root: dir} }

var (
	_ Reader = (*LocalStore)(nil)
	_ Writer = (*LocalStore)(nil)
)

// monthKey formats t's UTC year and month as "2006-01". Month enumeration is a
// calendar operation at the load boundary, not a hot path, so time.Time is
// appropriate here.
func monthKey(t core.Time) string {
	return t.Std().Format("2006-01")
}

func (s *LocalStore) dir(sym string, tf core.TF) string {
	return filepath.Join(s.root, sym, tf.String())
}

func (s *LocalStore) parquetPath(sym string, tf core.TF, month string) string {
	return filepath.Join(s.dir(sym, tf), month+".parquet")
}

func (s *LocalStore) manifestPath(sym string, tf core.TF, month string) string {
	return filepath.Join(s.dir(sym, tf), month+".manifest.json")
}

// Write encodes s to Parquet and persists it with a completed manifest. The
// caller supplies identity and provenance (symbol, timeframe, month, source,
// build version); Write fills the data-derived fields (rows, bounds, content
// hash, schema version) so a manifest can never disagree with its file.
//
// Both files are written through a temp-and-rename so a crash mid-write never
// leaves a valid manifest beside a truncated Parquet file.
func (s *LocalStore) Write(ctx context.Context, series *core.Series, meta Manifest) error {
	if series.Len() == 0 {
		return ErrEmptySeries
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if meta.Symbol != series.Symbol {
		return fmt.Errorf("store: %w: manifest symbol %q, series symbol %q",
			ErrManifestMismatch, meta.Symbol, series.Symbol)
	}
	if meta.TF != series.TF {
		return fmt.Errorf("store: %w: manifest tf %s, series tf %s",
			ErrManifestMismatch, meta.TF, series.TF)
	}

	first, last, _ := series.Bounds()
	if monthKey(first) != meta.Month || monthKey(last) != meta.Month {
		return fmt.Errorf("store: %w: bars span %s..%s but month is %q",
			ErrManifestMismatch, monthKey(first), monthKey(last), meta.Month)
	}

	data, err := encodeSeries(series)
	if err != nil {
		return err
	}

	meta.Rows = int64(series.Len())
	meta.First = first
	meta.Last = last
	meta.ContentHash = contentHash(data)
	meta.SchemaVersion = SchemaVersion

	manifestBytes, err := meta.encode()
	if err != nil {
		return fmt.Errorf("store: encoding manifest: %w", err)
	}

	dir := s.dir(meta.Symbol, meta.TF)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("store: creating %s: %w", dir, err)
	}
	if err := writeFileAtomic(s.parquetPath(meta.Symbol, meta.TF, meta.Month), data); err != nil {
		return err
	}
	if err := writeFileAtomic(s.manifestPath(meta.Symbol, meta.TF, meta.Month), manifestBytes); err != nil {
		return err
	}
	return nil
}

// Load reads every stored month whose bars overlap [from, to), verifies each
// file against its manifest, and returns the concatenated bars restricted to
// the window together with the manifests actually touched. Months with no file
// are skipped: a gap in the library is an absence, not an error.
//
// It discovers files by listing the symbol/timeframe directory rather than
// enumerating calendar months, so an unbounded window (MinTime..MaxTime) costs
// a directory read, not a walk over every month in the representable range.
func (s *LocalStore) Load(ctx context.Context, sym string, tf core.TF, from, to core.Time) (*core.Series, []Manifest, error) {
	if to < from {
		return nil, nil, fmt.Errorf("store: load window end %d precedes start %d", int64(to), int64(from))
	}

	// First pass: read the manifests of files that overlap the window, to size
	// the result and learn which files to decode. Second pass decodes them.
	type touched struct {
		manifest Manifest
		path     string
	}
	var files []touched
	var capRows int

	entries, err := os.ReadDir(s.dir(sym, tf))
	if errors.Is(err, os.ErrNotExist) {
		entries = nil // no data for this symbol/timeframe yet
	} else if err != nil {
		return nil, nil, fmt.Errorf("store: listing %s: %w", s.dir(sym, tf), err)
	}

	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".manifest.json") {
			continue
		}
		mfPath := filepath.Join(s.dir(sym, tf), name)
		mfBytes, err := os.ReadFile(mfPath)
		if err != nil {
			return nil, nil, fmt.Errorf("store: reading manifest %s: %w", mfPath, err)
		}
		var mf Manifest
		if err := mf.UnmarshalJSON(mfBytes); err != nil {
			return nil, nil, fmt.Errorf("store: parsing manifest %s: %w", mfPath, err)
		}
		if mf.SchemaVersion != SchemaVersion {
			return nil, nil, fmt.Errorf("store: %w: %s declares version %d, want %d",
				ErrSchemaVersion, mfPath, mf.SchemaVersion, SchemaVersion)
		}
		// A file overlaps [from, to) unless it ends before the window starts or
		// begins at or after the window ends.
		if mf.Last < from || mf.First >= to {
			continue
		}
		files = append(files, touched{manifest: mf, path: s.parquetPath(sym, tf, mf.Month)})
		capRows += int(mf.Rows)
	}

	// Directory listing order is not guaranteed; sort by month so concatenation
	// and the returned manifest order are deterministic.
	sort.Slice(files, func(i, j int) bool { return files[i].manifest.Month < files[j].manifest.Month })

	result := &core.Series{
		Symbol: sym,
		TF:     tf,
		Time:   make([]core.Time, 0, capRows),
		Open:   make([]core.Price, 0, capRows),
		High:   make([]core.Price, 0, capRows),
		Low:    make([]core.Price, 0, capRows),
		Close:  make([]core.Price, 0, capRows),
		Volume: make([]int64, 0, capRows),
	}
	manifests := make([]Manifest, 0, len(files))

	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		data, err := os.ReadFile(f.path)
		if err != nil {
			return nil, nil, fmt.Errorf("store: reading %s: %w", f.path, err)
		}
		if got := contentHash(data); got != f.manifest.ContentHash {
			return nil, nil, fmt.Errorf("store: %w: %s (manifest %s, file %s)",
				ErrHashMismatch, f.path, f.manifest.ContentHash, got)
		}
		month, err := decodeSeries(data, sym, tf)
		if err != nil {
			return nil, nil, fmt.Errorf("store: decoding %s: %w", f.path, err)
		}
		appendWindow(result, month, from, to)
		manifests = append(manifests, f.manifest)
	}

	return result, manifests, nil
}

// appendWindow appends bars of src whose open time is in [from, to) onto dst.
func appendWindow(dst, src *core.Series, from, to core.Time) {
	view := src.Slice(from, to)
	dst.Time = append(dst.Time, view.Time...)
	dst.Open = append(dst.Open, view.Open...)
	dst.High = append(dst.High, view.High...)
	dst.Low = append(dst.Low, view.Low...)
	dst.Close = append(dst.Close, view.Close...)
	dst.Volume = append(dst.Volume, view.Volume...)
}

func firstOfMonth(t time.Time) time.Time {
	y, m, _ := t.UTC().Date()
	return time.Date(y, m, 1, 0, 0, 0, 0, time.UTC)
}

// writeFileAtomic writes data to path via a temp file in the same directory
// followed by rename, so readers never observe a partial file.
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("store: creating temp file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after a successful rename

	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("store: writing %s: %w", tmpName, err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("store: syncing %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("store: closing %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("store: renaming %s to %s: %w", tmpName, path, err)
	}
	return nil
}
