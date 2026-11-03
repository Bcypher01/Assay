# Batch 03 — Library client and data manager

**Depends on:** 01 (02 for real data; develop against fixtures)
**Blocks:** 07
**Packages:** `internal/library`

## Goal

Get a cold client from launch to running a backtest in under ten seconds, by
fetching only the resolution the strategy actually needs, and keep a
direct-from-venue fallback that works when the library does not.

## Deliverables

- `Loader` — resolve a data request to local files, fetching what is missing
- Requirement derivation: given a strategy's timeframes, compute the minimum
  resolution and the instruments and date range needed
- Fetch from object storage by symbol, timeframe and month; verify content hash;
  write into the local cache in the batch 01 layout
- Fallback path: when the library lacks an instrument or is unreachable, fetch
  via the venue adapters and build bars locally
- Incremental update: fetch only months newer than the local manifest
- Escalation: fetch fine-resolution slices (1s or ticks) for a specific list of
  time windows, not whole months
- Progress reporting suitable for streaming over RPC (batch 08)

## Key decisions

**Requirement derives from the strategy, not from a global setting.** An H4/M15
strategy pulls ~4MB. Prompting every user for M1 by default was the original
design mistake and is not repeated.

**Refuse rather than degrade.** If a strategy's finest timeframe is finer than
any available data, the loader returns `ErrUnsupportedResolution` naming what is
missing. It never silently substitutes a coarser series.

**The fallback stays exercised.** It runs in CI against a fixture venue, not just
in theory. It is the answer both to library downtime and to an instrument being
pulled for licensing reasons.

**Cache is content-addressed by manifest hash.** A rebuild upstream is detected
and refetched; an unchanged month is never refetched.

## Interfaces

```go
type Request struct {
    Symbols  []string
    TFs      []core.TF        // derived, not user-supplied
    From, To core.Time
}

type Loader interface {
    Ensure(ctx context.Context, r Request, progress func(Progress)) error
    Load(ctx context.Context, sym string, tf core.TF, from, to core.Time) (*core.Series, []store.Manifest, error)
    EscalateWindows(ctx context.Context, sym string, w []Window, target core.TF) error
}

type Progress struct {
    Stage       string  // "resolving" | "downloading" | "verifying"
    BytesDone   int64
    BytesTotal  int64
    CurrentFile string
}
```

## Tests

- Requirement derivation across strategy shapes, including multi-instrument
- Cold cache: fetch, verify, load, with a fixture object store
- Warm cache: no network calls, proven with a failing transport
- Hash mismatch triggers refetch exactly once, then fails with a typed error
- Fallback: library unreachable, venue fixture serves, result is byte-identical
  to the library's version
- Escalation fetches only the named windows

## Exit criteria

- [ ] Cold start to first backtest under 10 seconds for an H4/M15/M5 strategy on
      one instrument, measured end to end
- [ ] Warm start performs zero network calls
- [ ] Fallback path produces bars byte-identical to the published ones
- [ ] `ErrUnsupportedResolution` names the missing timeframe and instrument

## Out of scope

The UI data manager (batch 12). Streaming progress over the wire (batch 08) —
this batch exposes the callback, not the transport.

## Decision log

**Object-storage client: a minimal signed-request R2 client in `internal/pipeline`,
and a credential-free HTTP reader in `internal/library`.** Research clients never
hold publish credentials.

**Delivery hardening (this revision).** The cold path was reworked for hosted use,
where a long-lived server pays each cost repeatedly:

- `LocalStore.Manifests` reads and parses manifest files only. `Status` and
  `missingMonths` now use it, so a readiness preflight no longer decodes and
  hash-verifies every Parquet file in the window. Content hashes are still
  verified on `Load`, on exactly the months a study consumes; a structural
  manifest error still surfaces as `CACHE_CORRUPT` before a run.
- `HTTPStore` retries transient failures (network errors, 408, 429, 5xx) with
  exponential backoff capped at `maxRetryBackoff`, honouring `Retry-After`. A 404,
  any other 4xx, and a 2xx are never retried: they are definitive answers.
- `Ensure` prepares missing months with a bounded worker pool (`Loader.Workers`,
  default `GOMAXPROCS`). Each month is an independent file, so concurrency changes
  no bytes and no `SnapshotHash`; the progress sink is still invoked from one
  goroutine at a time, under a mutex, so a stream or test recorder is safe.
- Interrupted month downloads resume. `LocalStore` keeps a `.parquet.partial`
  sibling, committed only after `VerifyPublished` passes; `ObjectStore`
  implementations that satisfy `RangeReader` (`HTTPStore`, `LocalPipeline`) are
  asked for only the remainder via `Range: bytes=<offset>-`. An origin that
  ignores the range, a 416, or a non-range store falls back to a clean full
  download rather than concatenating mismatched bytes.

The fixture strategy for CI remains the in-process `pipeline.LocalPublisher`
backed by a temp directory.
