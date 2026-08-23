package store

import "errors"

// Sentinel errors returned by the store package. They are wrapped with
// fmt.Errorf("...: %w", err) at the call site so callers can match with
// errors.Is while still seeing the offending path or symbol.
var (
	// ErrHashMismatch is returned when a Parquet file's bytes do not hash to
	// the ContentHash recorded in its manifest. It signals corruption or a
	// stale manifest, and a study must never proceed on data that fails it.
	ErrHashMismatch = errors.New("store: content hash mismatch")

	// ErrSchemaVersion is returned when a file or manifest was written by an
	// incompatible schema version.
	ErrSchemaVersion = errors.New("store: unsupported schema version")

	// ErrEmptySeries is returned when a write is attempted with no bars. An
	// empty month is represented by the absence of a file, not an empty one.
	ErrEmptySeries = errors.New("store: cannot write an empty series")

	// ErrManifestMismatch is returned when a manifest's identity fields
	// (symbol, timeframe, month) do not match the file it accompanies or the
	// series being written.
	ErrManifestMismatch = errors.New("store: manifest does not match series")

	// ErrColumnLayout is returned when a Parquet file's columns do not match
	// the expected bar schema.
	ErrColumnLayout = errors.New("store: unexpected parquet column layout")
)
