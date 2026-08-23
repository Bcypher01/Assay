# Batch 01 — Bar storage

**Depends on:** 00
**Blocks:** 02, 03
**Packages:** `internal/store`

## Goal

Define the Parquet bar format and its manifest, and implement a pure-Go reader
and writer. This format is shared by the pipeline binary and the client, and is
the thing a study's determinism is pinned to — so it is specified once, here, and
versioned.

## Deliverables

- Parquet schema for a bar file: `time int64`, `open/high/low/close int64`
  (ticks), `volume int64`. Delta encoding on time, dictionary or delta on prices,
  ZSTD compression.
- `Manifest` — symbol, timeframe, month, row count, first and last timestamp,
  content hash of the data pages, source venue, pipeline build version, schema
  version
- Writer: `Series` to Parquet plus manifest, deterministic byte output for
  identical input
- Reader: Parquet to `*core.Series`, decoding straight into preallocated slices
- Local cache layout: `<root>/<symbol>/<tf>/<YYYY-MM>.parquet` plus
  `<YYYY-MM>.manifest.json`
- Integrity check: verify content hash on read, with a typed error on mismatch
- Snapshot identity: hash over the set of manifests a study consumed, so a run
  can pin exactly the data it saw

## Key decisions

**Partition by symbol / timeframe / month.** Month is coarse enough that file
counts stay manageable and fine enough that a client fetching two years pulls 24
small files it can cache individually.

**Store every timeframe rather than deriving from the finest.** Costs storage
that is negligible at these sizes; buys the removal of an entire class of
resampling inconsistency between users, and means an H4 strategy never downloads
M1.

**Deterministic writer output.** Same `Series` in, byte-identical file out. The
manifest hash is meaningless otherwise, and the pipeline's incremental append
depends on being able to tell a rebuild from a no-op.

**No cgo.** Rules out the DuckDB and Arrow C bindings. Pure-Go Parquet only.

## Interfaces

```go
type Manifest struct {
    Symbol       string
    TF           core.TF
    Month        string   // "2026-08"
    Rows         int64
    First, Last  core.Time
    ContentHash  string   // hex
    Source       string
    BuildVersion string
    SchemaVersion int
}

type Reader interface {
    Load(ctx context.Context, sym string, tf core.TF, from, to core.Time) (*core.Series, []Manifest, error)
}

type Writer interface {
    Write(ctx context.Context, s *core.Series, meta Manifest) error
}

func SnapshotHash(ms []Manifest) string   // sorted, stable
```

## Tests

- Round-trip: `Series` to file to `Series` is exact, including at month boundaries
- Determinism: writing the same series twice produces identical bytes
- Corruption: a flipped byte in a data page fails the hash check with a typed error
- Partial range reads return only the requested window and the manifests touched
- Benchmark: loading 2.6M M1 bars, with allocation count

## Exit criteria

- [x] Reading 2.6M M1 bars completes in under 500ms with allocations proportional
      to the series, not to the row count — ~130–145ms, ~5,200 allocs (per page)
- [x] Two writes of identical input produce identical bytes
- [x] `SnapshotHash` is stable across manifest ordering
- [x] No cgo in the dependency graph — verified by `CGO_ENABLED=0 go build ./...`

## Out of scope

Fetching from venues (02) or from the library (03). Tick storage — ticks are
fetched as venue-native slices and never enter this format.

## Decision log

**Parquet library: `github.com/parquet-go/parquet-go` v0.25.1.** The maintained
community successor to `segmentio/parquet-go`, and the only pure-Go option that
gives all of: per-column encoding and compression control (delta on every
column, then ZSTD), a byte-deterministic writer once provenance is pinned, and a
typed `Int64Reader` decode fast path. Its transitive dependencies —
`klauspost/compress` (ZSTD/pure Go), `andybalholm/brotli`, `pierrec/lz4`,
`google/uuid`, `golang.org/x/sys/cpu` — are all pure Go. `CGO_ENABLED=0 go build
./...` is clean, satisfying the no-cgo non-negotiable. The Arrow Go Parquet
implementation was rejected: it drags in the Arrow memory/array machinery
(the "dataframe libraries" `conventions.md` already rejects) for no benefit
here, where the format is six int64 columns.

**Content hash is SHA-256 of the persisted Parquet bytes, not of the decoded
values.** Verified on read *before* decoding. Because the writer is
byte-deterministic, identical data yields an identical file yields an identical
hash; any flipped byte in any data page changes it, so corruption fails the hash
check with a typed `ErrHashMismatch` regardless of whether the flip would also
have broken decompression. This is the S3/R2 object-integrity pattern and makes
the corruption test deterministic. `SnapshotHash` composes these file hashes
(plus symbol/tf/month/rows/bounds/schema-version) over the manifest set, sorted
so it is stable across ordering. Provenance fields (source, build version) are
deliberately excluded from `SnapshotHash`: two pipeline builds that emit
byte-identical bars are the same data snapshot.

**Writer determinism required pinning `CreatedBy`.** parquet-go otherwise derives
the file's "created by" string from build info (module version + VCS sha), which
varies between builds and would break byte-for-byte reproducibility. It is pinned
to a fixed constant (`assay/store`, `v1`). ZSTD is left at its
default single-worker concurrency, which is deterministic; no bloom filters or
other optional, potentially non-deterministic structures are enabled.

**Reader decodes column-at-a-time through the typed `Int64Reader` fast path**
straight into the preallocated `core.Series` slices, via one reused page-sized
scratch buffer. Benchmark (`BenchmarkDecode2p6M`): 2.6M M1 bars decode in
~130–145ms (target < 500ms), at ~5,200 allocations — one per decoded page
(≈ rows ÷ page size × 6 columns), i.e. proportional to the series structure, not
to the row count. Live memory is the six typed slices (~125MB); the larger
reported B/op is transient per-page decode buffers that the GC reclaims.

**Deferred question answered: `Series` does NOT carry a bid/ask spread column,
and the bar Parquet format stays OHLCV.** `CostModel.SpreadFromData` uses the
spread "recorded in the data where available, falling back to Fixed where it is
not" — and where it is available is tick data, which batch 02 fetches as
venue-native slices for the specific windows containing unresolved trades, and
which this format explicitly never stores. Adding a per-bar spread column would
put a fill-time, tick-level quantity onto every bar of every timeframe — mostly
interpolated or absent, paid for in every file of the library — to serve one
spread mode whose specified fallback is already `SpreadFixed`. The cost/benefit
fails, and it would also force a change to a batch-00 core type that "touching
later touches every file." So: bars are OHLCV; spread-from-data is a batch-02
tick concern.

**Dependency added:** `github.com/parquet-go/parquet-go` v0.25.1 (+ the pure-Go
transitive deps above), used only in `internal/store`. No dependency enters
`internal/core`, `internal/sim`, or `internal/primitive`.

**Build note — `golang.org/x/sys` local replace.** This batch was implemented in
a sandbox whose network egress blocks the Go module proxy and the `golang.org`
vanity host, so `golang.org/x/sys` (needed transitively by parquet-go, for
`cpu`) is pinned via `replace golang.org/x/sys v0.21.0 => ./third_party/x-sys`,
a trimmed copy containing only the `cpu` package (264K). This is a sandbox
artifact, not a design choice: on a machine with normal network it can be
removed with `go mod edit -dropreplace golang.org/x/sys && rm -rf third_party &&
go mod tidy`. The other five dependencies resolve normally from the proxy.
