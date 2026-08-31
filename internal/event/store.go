package event

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Bcypher01/Assay/internal/core"
)

const SchemaVersion = 1

var (
	ErrSnapshotNotFound = errors.New("event: snapshot not found")
	ErrSnapshotMismatch = errors.New("event: snapshot manifest mismatch")
)

// Store persists content-addressed immutable snapshots beneath dir/snapshots.
type Store struct{ dir string }

func NewStore(dir string) *Store { return &Store{dir: dir} }

// SnapshotMetadata is the catalogue entry for one verified immutable
// snapshot. CreatedAt is the local creation time of its manifest; it is not
// part of the content hash or a claim about when the source events occurred.
type SnapshotMetadata struct {
	Manifest
	CreatedAt core.Time `json:"createdAtMs"`
}

// List returns metadata for every locally available, valid snapshot. A
// catalogue never exposes an unchecked file: each entry is loaded so the
// content hash, schema, row count and records are verified before it is
// returned. A fresh installation has an empty catalogue.
func (s *Store) List(ctx context.Context) ([]SnapshotMetadata, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	dir := filepath.Join(s.dir, "snapshots")
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return []SnapshotMetadata{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("event: list snapshots: %w", err)
	}
	const manifestSuffix = ".manifest.json"
	items := make([]SnapshotMetadata, 0, len(entries))
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), manifestSuffix) {
			continue
		}
		hash := strings.TrimSuffix(entry.Name(), manifestSuffix)
		snapshot, err := s.Load(ctx, hash)
		if err != nil {
			return nil, fmt.Errorf("event: catalogue entry %q: %w", hash, err)
		}
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		items = append(items, SnapshotMetadata{Manifest: snapshot.Manifest, CreatedAt: core.FromStd(info.ModTime())})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt != items[j].CreatedAt {
			return items[i].CreatedAt > items[j].CreatedAt
		}
		return items[i].ContentHash < items[j].ContentHash
	})
	return items, nil
}

// Write validates, canonically orders, and atomically creates an immutable
// snapshot. Equal content always receives the same hash regardless of source
// delivery order.
func (s *Store) Write(ctx context.Context, events []Event, meta Manifest) (Manifest, error) {
	if err := ctx.Err(); err != nil {
		return Manifest{}, err
	}
	if len(events) == 0 {
		return Manifest{}, fmt.Errorf("%w: empty snapshot", ErrInvalidEvent)
	}
	canonical := make([]Event, len(events))
	for i, e := range events {
		if err := e.Validate(); err != nil {
			return Manifest{}, fmt.Errorf("event %q: %w", e.ID, err)
		}
		canonical[i] = cloneEvent(e)
	}
	sort.Slice(canonical, func(i, j int) bool {
		if canonical[i].OccurredAt != canonical[j].OccurredAt {
			return canonical[i].OccurredAt < canonical[j].OccurredAt
		}
		return canonical[i].ID < canonical[j].ID
	})
	for i := 1; i < len(canonical); i++ {
		if canonical[i-1].ID == canonical[i].ID {
			return Manifest{}, fmt.Errorf("%w: duplicate id %q", ErrInvalidEvent, canonical[i].ID)
		}
	}
	payload, err := json.Marshal(canonical)
	if err != nil {
		return Manifest{}, fmt.Errorf("event: encode snapshot: %w", err)
	}
	h := sha256.Sum256(payload)
	meta.ContentHash = hex.EncodeToString(h[:])
	meta.Rows, meta.First, meta.Last, meta.SchemaVersion = len(canonical), canonical[0].OccurredAt, canonical[len(canonical)-1].OccurredAt, SchemaVersion
	if meta.TaxonomyVersion == "" {
		meta.TaxonomyVersion = TaxonomyVersion
	}
	if meta.Source == "" || meta.BuildVersion == "" {
		return Manifest{}, fmt.Errorf("%w: source and build version are required", ErrInvalidEvent)
	}
	manifest, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return Manifest{}, err
	}
	dir := filepath.Join(s.dir, "snapshots")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Manifest{}, fmt.Errorf("event: create snapshot directory: %w", err)
	}
	dataPath := filepath.Join(dir, meta.ContentHash+".json")
	manifestPath := filepath.Join(dir, meta.ContentHash+".manifest.json")
	if existing, err := os.ReadFile(dataPath); err == nil {
		if string(existing) != string(payload) {
			return Manifest{}, ErrSnapshotMismatch
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return Manifest{}, err
	} else if err := writeAtomic(dataPath, payload); err != nil {
		return Manifest{}, err
	}
	if existing, err := os.ReadFile(manifestPath); err == nil {
		var old Manifest
		if err := json.Unmarshal(existing, &old); err != nil || old != meta {
			return Manifest{}, ErrSnapshotMismatch
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return Manifest{}, err
	} else if err := writeAtomic(manifestPath, manifest); err != nil {
		return Manifest{}, err
	}
	return meta, nil
}

// Load reads and verifies an immutable snapshot by content hash.
func (s *Store) Load(ctx context.Context, hash string) (Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	base := filepath.Join(s.dir, "snapshots", hash)
	data, err := os.ReadFile(base + ".json")
	if errors.Is(err, os.ErrNotExist) {
		return Snapshot{}, fmt.Errorf("%w: %s", ErrSnapshotNotFound, hash)
	}
	if err != nil {
		return Snapshot{}, err
	}
	got := sha256.Sum256(data)
	if hex.EncodeToString(got[:]) != hash {
		return Snapshot{}, ErrSnapshotMismatch
	}
	var events []Event
	if err := json.Unmarshal(data, &events); err != nil {
		return Snapshot{}, fmt.Errorf("event: decode snapshot: %w", err)
	}
	for _, e := range events {
		if err := e.Validate(); err != nil {
			return Snapshot{}, err
		}
	}
	mb, err := os.ReadFile(base + ".manifest.json")
	if err != nil {
		return Snapshot{}, err
	}
	var mf Manifest
	if err := json.Unmarshal(mb, &mf); err != nil {
		return Snapshot{}, err
	}
	if mf.ContentHash != hash || mf.Rows != len(events) || mf.SchemaVersion != SchemaVersion {
		return Snapshot{}, ErrSnapshotMismatch
	}
	return Snapshot{Manifest: mf, Events: events}, nil
}

func cloneEvent(e Event) Event {
	e.Currencies = append([]string(nil), e.Currencies...)
	e.Evidence = append([]EvidenceRef(nil), e.Evidence...)
	return e
}

func writeAtomic(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".event-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
}
