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

_Record the object-storage client chosen, and the fixture strategy for CI._
