package pipeline

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/olumide/assay/internal/core"
	"github.com/olumide/assay/internal/store"
)

// Publisher writes immutable bar-library objects and reads their manifests.
type Publisher interface {
	Put(ctx context.Context, key string, body io.Reader, meta store.Manifest) error
	Head(ctx context.Context, key string) (store.Manifest, bool, error)
	Get(ctx context.Context, key string) (io.ReadCloser, bool, error)
}

// LocalPublisher is an object-store-shaped publisher backed by the filesystem.
// It uses the same key layout intended for R2, making it useful for tests,
// local dry runs, and byte-for-byte publisher development.
type LocalPublisher struct {
	root string
}

// NewLocalPublisher returns a filesystem publisher rooted at dir.
func NewLocalPublisher(dir string) *LocalPublisher { return &LocalPublisher{root: dir} }

var _ Publisher = (*LocalPublisher)(nil)

// ObjectKey returns the canonical object key for one month of bars.
func ObjectKey(symbol string, tf core.TF, month string) string {
	return filepath.ToSlash(filepath.Join(symbol, tf.String(), month+".parquet"))
}

func manifestKey(key string) string {
	return strings.TrimSuffix(key, ".parquet") + ".manifest.json"
}

func (p *LocalPublisher) Put(ctx context.Context, key string, body io.Reader, meta store.Manifest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if filepath.IsAbs(key) || strings.Contains(key, "..") {
		return fmt.Errorf("pipeline: unsafe object key %q", key)
	}
	path := filepath.Join(p.root, filepath.FromSlash(key))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("pipeline: creating object dir: %w", err)
	}
	data, err := io.ReadAll(body)
	if err != nil {
		return fmt.Errorf("pipeline: reading object body: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("pipeline: writing %s: %w", path, err)
	}
	mf, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("pipeline: encoding manifest: %w", err)
	}
	mfPath := filepath.Join(p.root, filepath.FromSlash(manifestKey(key)))
	if err := os.WriteFile(mfPath, mf, 0o644); err != nil {
		return fmt.Errorf("pipeline: writing %s: %w", mfPath, err)
	}
	return nil
}

func (p *LocalPublisher) Head(ctx context.Context, key string) (store.Manifest, bool, error) {
	if err := ctx.Err(); err != nil {
		return store.Manifest{}, false, err
	}
	if filepath.IsAbs(key) || strings.Contains(key, "..") {
		return store.Manifest{}, false, fmt.Errorf("pipeline: unsafe object key %q", key)
	}
	path := filepath.Join(p.root, filepath.FromSlash(manifestKey(key)))
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return store.Manifest{}, false, nil
	}
	if err != nil {
		return store.Manifest{}, false, fmt.Errorf("pipeline: reading %s: %w", path, err)
	}
	var mf store.Manifest
	if err := json.Unmarshal(data, &mf); err != nil {
		return store.Manifest{}, false, fmt.Errorf("pipeline: parsing %s: %w", path, err)
	}
	return mf, true, nil
}

func (p *LocalPublisher) Get(ctx context.Context, key string) (io.ReadCloser, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if filepath.IsAbs(key) || strings.Contains(key, "..") {
		return nil, false, fmt.Errorf("pipeline: unsafe object key %q", key)
	}
	path := filepath.Join(p.root, filepath.FromSlash(key))
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("pipeline: opening %s: %w", path, err)
	}
	return f, true, nil
}
