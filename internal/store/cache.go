package store

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/bcypher01/assay/internal/core"
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
	root     string
	maxBytes int64
	mu       sync.Mutex
}

// NewLocalStore returns a store rooted at dir.
func NewLocalStore(dir string) *LocalStore { return NewLocalStoreWithMaxBytes(dir, 5<<30) }

// NewLocalStoreWithMaxBytes bounds cached Parquet files. A non-positive limit
// disables eviction. Manifests are evicted with their files, since they only
// describe cached payloads and can be fetched again from the published library.
func NewLocalStoreWithMaxBytes(dir string, maxBytes int64) *LocalStore {
	return &LocalStore{root: dir, maxBytes: maxBytes}
}

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

func (s *LocalStore) partialPath(sym string, tf core.TF, month string) string {
	return filepath.Join(s.dir(sym, tf), month+".parquet.partial")
}

// Write encodes s to Parquet and persists it with a completed manifest. The
// caller supplies identity and provenance (symbol, timeframe, month, source,
// build version); Write fills the data-derived fields (rows, bounds, content
// hash, schema version) so a manifest can never disagree with its file.
//
// Both files are written through a temp-and-rename so a crash mid-write never
// leaves a valid manifest beside a truncated Parquet file.
func (s *LocalStore) Write(ctx context.Context, series *core.Series, meta Manifest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
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
	return s.enforceLimit()
}

// WritePublished persists already-encoded published bytes plus their manifest
// into the local cache layout without re-encoding the Parquet payload.
func (s *LocalStore) WritePublished(ctx context.Context, data []byte, meta Manifest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writePublishedLocked(ctx, data, meta)
}

// writePublishedLocked is WritePublished's body without the lock, so composite
// operations (CommitPartial) can update the cache atomically. Callers must hold
// s.mu.
func (s *LocalStore) writePublishedLocked(ctx context.Context, data []byte, meta Manifest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := VerifyPublished(data, meta); err != nil {
		return err
	}
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
	return s.enforceLimit()
}

// PartialSize returns the number of bytes already written for an interrupted
// month download, or zero when there is none.
func (s *LocalStore) PartialSize(sym string, tf core.TF, month string) int64 {
	info, err := os.Stat(s.partialPath(sym, tf, month))
	if err != nil {
		return 0
	}
	return info.Size()
}

// AppendPartial appends src to the in-progress download for a month and returns
// the new length. The partial file sits beside the final object and is promoted
// only by CommitPartial, so an interrupted transfer never leaves a readable but
// incomplete month in the cache. Callers must not append to the same month from
// two goroutines; the library client prepares each month in one worker.
func (s *LocalStore) AppendPartial(ctx context.Context, sym string, tf core.TF, month string, src io.Reader) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	dir := s.dir(sym, tf)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, fmt.Errorf("store: creating %s: %w", dir, err)
	}
	path := s.partialPath(sym, tf, month)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return 0, fmt.Errorf("store: opening partial %s: %w", path, err)
	}
	if _, err := io.Copy(f, src); err != nil {
		_ = f.Close()
		return 0, fmt.Errorf("store: writing partial %s: %w", path, err)
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		return 0, fmt.Errorf("store: syncing partial %s: %w", path, err)
	}
	if err := f.Close(); err != nil {
		return 0, fmt.Errorf("store: closing partial %s: %w", path, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		return 0, fmt.Errorf("store: sizing partial %s: %w", path, err)
	}
	return info.Size(), nil
}

// DiscardPartial removes an in-progress month download. It is a no-op when none
// exists.
func (s *LocalStore) DiscardPartial(sym string, tf core.TF, month string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.Remove(s.partialPath(sym, tf, month)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("store: discarding partial %s: %w", s.partialPath(sym, tf, month), err)
	}
	return nil
}

// CommitPartial verifies a completed month download against its manifest,
// promotes it into the cache, and removes the partial on success. It returns
// ErrHashMismatch, leaving the partial in place, when the bytes do not match.
func (s *LocalStore) CommitPartial(ctx context.Context, sym string, tf core.TF, month string, meta Manifest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.partialPath(sym, tf, month))
	if err != nil {
		return fmt.Errorf("store: reading partial %s: %w", s.partialPath(sym, tf, month), err)
	}
	if err := s.writePublishedLocked(ctx, data, meta); err != nil {
		return err
	}
	if err := os.Remove(s.partialPath(sym, tf, month)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("store: removing partial %s: %w", s.partialPath(sym, tf, month), err)
	}
	return nil
}

func (s *LocalStore) enforceLimit() error {
	if s.maxBytes <= 0 {
		return nil
	}
	type cached struct {
		parquet, manifest string
		size              int64
		modified          time.Time
	}
	var files []cached
	var total int64
	err := filepath.WalkDir(s.root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".parquet") {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		files = append(files, cached{parquet: path, manifest: strings.TrimSuffix(path, ".parquet") + ".manifest.json", size: info.Size(), modified: info.ModTime()})
		total += info.Size()
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("store: measuring cache: %w", err)
	}
	sort.Slice(files, func(i, j int) bool { return files[i].modified.Before(files[j].modified) })
	for _, file := range files {
		if total <= s.maxBytes {
			break
		}
		if err := os.Remove(file.parquet); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if err := os.Remove(file.manifest); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		total -= file.size
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
	s.mu.Lock()
	defer s.mu.Unlock()
	if to < from {
		return nil, nil, fmt.Errorf("store: load window end %d precedes start %d", int64(to), int64(from))
	}

	manifests, err := s.readManifests(ctx, sym, tf)
	if err != nil {
		return nil, nil, err
	}

	// First pass: select the manifests whose files overlap the window, to size
	// the result and learn which files to decode. Second pass decodes them.
	type touched struct {
		manifest Manifest
		path     string
	}
	var files []touched
	var capRows int
	for _, mf := range manifests {
		// A manifest's Last is the final candle's open, not an instantaneous
		// observation. Treat the file as covering through that candle's close;
		// otherwise a rolling D1 request beginning midday ignores a perfectly
		// valid candle opened at midnight and repeatedly refetches the month.
		lastEnd := mf.Last + core.Time(tf.Millis())
		if lastEnd <= from || mf.First >= to {
			continue
		}
		files = append(files, touched{manifest: mf, path: s.parquetPath(sym, tf, mf.Month)})
		capRows += int(mf.Rows)
	}

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
	manifests = make([]Manifest, 0, len(files))

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

// Manifests returns the manifest of every cached month for a symbol and
// timeframe, sorted by month, without reading or decoding any Parquet payload.
//
// It is the cheap preflight path: readiness and missing-month checks need
// provenance, bounds and row counts, not the bars. It deliberately does not
// verify a file's content hash against its manifest — that is Load's job, on
// the exact months a study actually consumes. A manifest that cannot be parsed,
// or that declares an incompatible schema version, is still an error here, so
// structural cache damage still surfaces before a run.
func (s *LocalStore) Manifests(ctx context.Context, sym string, tf core.TF) ([]Manifest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readManifests(ctx, sym, tf)
}

// readManifests lists and parses every manifest for a symbol and timeframe.
// Callers must hold s.mu.
func (s *LocalStore) readManifests(ctx context.Context, sym string, tf core.TF) ([]Manifest, error) {
	entries, err := os.ReadDir(s.dir(sym, tf))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil // no data for this symbol/timeframe yet
	}
	if err != nil {
		return nil, fmt.Errorf("store: listing %s: %w", s.dir(sym, tf), err)
	}

	var manifests []Manifest
	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".manifest.json") {
			continue
		}
		mfPath := filepath.Join(s.dir(sym, tf), name)
		mfBytes, err := os.ReadFile(mfPath)
		if err != nil {
			return nil, fmt.Errorf("store: reading manifest %s: %w", mfPath, err)
		}
		var mf Manifest
		if err := mf.UnmarshalJSON(mfBytes); err != nil {
			return nil, fmt.Errorf("store: parsing manifest %s: %w", mfPath, err)
		}
		if mf.SchemaVersion != SchemaVersion {
			return nil, fmt.Errorf("store: %w: %s declares version %d, want %d",
				ErrSchemaVersion, mfPath, mf.SchemaVersion, SchemaVersion)
		}
		manifests = append(manifests, mf)
	}

	// Directory listing order is not guaranteed; sort by month so callers and
	// the concatenation in Load see a deterministic order.
	sort.Slice(manifests, func(i, j int) bool { return manifests[i].Month < manifests[j].Month })
	return manifests, nil
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
		_ = tmp.Close()
		return fmt.Errorf("store: writing %s: %w", tmpName, err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
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
