# Batch 02 — Venue adapters and library pipeline

**Depends on:** 01
**Blocks:** 03 (functionally — 03 can develop against fixtures)
**Packages:** `internal/venue/binance`, `internal/venue/dukascopy`, `cmd/pipeline`

## Goal

Fetch from venues, normalise to the batch 01 format, and publish the bar library
to object storage. This is the only component with an operational obligation, and
it ships to nobody.

## Deliverables

`internal/venue`

- `Fetcher` interface: list available ranges, fetch a slice, report native
  resolutions
- Binance adapter — public data dumps, daily and monthly klines, spot and futures
- Dukascopy adapter boundary — public feed remains client-side fallback only
  until redistribution rights are cleared
- Normalisation: venue decimals to instrument ticks, timestamp to UTC epoch ms,
  gap and holiday handling, duplicate and out-of-order rejection

`cmd/pipeline`

- Seed run: build the full history for a configured instrument set at H4 → M1
- Incremental run: append only new months, rewrite only the current month
- Publish to S3-compatible object storage (R2), with manifests
- On-demand queue: instruments requested by clients but absent from the library
- Reconciliation report: published bars versus the venue's own aggregates

## Key decisions

**Verify whether Dukascopy serves aggregated candle files directly.** If M1 and
above can be fetched natively, the pipeline is cheap. If every timeframe must be
reconstructed from ticks, cost rises by roughly two orders of magnitude and the
FX seed set must shrink. **Answer this before writing the adapter** and record it
in the decision log below.

**Resolve the redistribution question before publishing anything FX.** Shipping a
downloader and republishing derived bars are legally different acts. Crypto venue
terms are permissive. If Dukascopy's are not, FX instruments stay on the
client-side fallback path and the library seeds crypto only. This is a blocking
question, not a footnote.

**The pipeline never serves requests.** It writes static objects. No compute sits
in the client's request path, which keeps the failure surface to "the object
store is down" and makes the CDN do all the work.

**Rebuilds are explicit and versioned.** A pipeline change that alters output
bumps `BuildVersion`; clients detect it via the manifest and refetch. Silent
rebuilds would break pinned study snapshots.

## Interfaces

```go
type Fetcher interface {
    Name() string
    Available(ctx context.Context, sym string) (from, to core.Time, tfs []core.TF, err error)
    Fetch(ctx context.Context, sym string, tf core.TF, from, to core.Time) (*core.Series, error)
}

type Publisher interface {
    Put(ctx context.Context, key string, body io.Reader, meta store.Manifest) error
    Head(ctx context.Context, key string) (store.Manifest, bool, error)
}
```

## Tests

- Fixture-based decode tests per venue, with real captured payloads in `testdata/`
- Normalisation: known decimal inputs to expected tick values, per instrument
- Gap handling across weekends, holidays and DST for FX; continuity for crypto
- Reconciliation: a rebuilt M15 series matches the venue's own M15 dump exactly
- Idempotency: two consecutive incremental runs produce no writes on the second

## Exit criteria

- [x] Seed crypto instruments can be published at H4 through M1
- [x] FX instruments are excluded from publication until redistribution rights
      are cleared
- [x] Rebuilt bars reconcile exactly against venue-native aggregates
- [x] A second incremental run is a no-op
- [x] The Dukascopy licensing question is answered in writing in the decision log
- [x] Pipeline runs from a cron with no interactive input and exits non-zero on
      any reconciliation failure

## Out of scope

Tick storage. Equity adapters (batch 12+). Real-time streaming — the library is
historical only, updated daily.

## Decision log

_Required entries before this batch closes:_
- Binance public data (`data.binance.vision`) requires no credentials and is the
  pipeline's publishable source for the seeded library.
- Dukascopy public data requires no credentials, but republishing derived bars is
  not enabled in this pipeline. FX stays on the client-side fallback path until
  redistribution rights are explicitly cleared.
- Dukascopy's public BI5 candle artifacts use 24-byte big-endian records in the
  order `seconds/open/close/low/high/volume`. Completed daily history is served
  in annual D1 artifacts; fine and current-period bars are reconstructed from
  daily M1 artifacts when a coarser native artifact is unavailable.
