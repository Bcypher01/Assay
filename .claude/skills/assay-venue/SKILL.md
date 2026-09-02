---
name: assay-venue
description: Add or change a market-data source in Assay (a venue adapter under internal/venue), or debug why bars for a symbol are missing, wrong or misaligned. Covers the Fetcher contract, the library-then-venue fetch order, aggregation and reconciliation rules, redistribution constraints, and the data-quality bar a new source must clear.
---

# Adding a venue

A venue adapter turns a public feed into `*core.Series`. It is the only layer
allowed to be uncertain: everything downstream treats its output as fact, so
the adapter must not paper over gaps.

## The contract

```go
type Fetcher interface {
	Name() string
	Available(ctx context.Context, sym string) (from, to core.Time, tfs []core.TF, err error)
	Fetch(ctx context.Context, sym string, tf core.TF, from, to core.Time) (*core.Series, error)
}
```

- Return `venue.Unsupported(name, reason)` for a timeframe or symbol you cannot
  serve. Never return a silently truncated series instead.
- `Available` may read the wall clock — it reports what the venue has *now*.
  Nothing else in the adapter may let the clock affect returned bars.
- Register the adapter in the `venue.Router.Fetchers` map wired in
  `cmd/assay/main.go` and `cmd/pipeline/main.go`, keyed by the `Venue` field in
  the instrument registry (`internal/instrument/specs`).

## Rules that are easy to get wrong

1. **Aggregate deterministically, never interpolate.** Coarser timeframes are
   folded from the venue's finest artifact (`internal/venue/dukascopy`
   aggregates M1). If a source bar is missing, the aggregate is missing — do
   not synthesise it from neighbours.
2. **Session boundaries are the venue's, not yours.** Dukascopy emits a
   separate Sunday daily session; folding it into the Monday candle is an
   explicit decision made in the adapter and documented there, not an accident.
3. **Prices convert once, through the instrument spec.**
   `instrument.Spec.PriceFromDecimal`. A `float64` that reaches storage is a bug.
4. **Retries and mirrors must not change the answer.** A mirror's 404 is not
   authoritative after a transient primary failure. Preserve the specific error
   through retries; do not overwrite it with a generic one.
5. **Redistribution is a licensing question, not a technical one.** Publishing
   a venue's candles to R2 requires confirmed rights. Dukascopy publication is
   gated behind `ASSAY_DUKASCOPY_REDISTRIBUTION_AUTHORIZED=true` and must stay
   that way for any source with comparable terms.

## Tests it must ship with

- Decode tests against **recorded real artifacts**, not fixtures you encoded
  yourself. `scripts/bi5-probe.py` is the precedent: probe the live feed once,
  settle the layout, then commit the recording.
- An aggregation test proving coarser timeframes match the venue's own
  aggregates where both exist (this is what `internal/pipeline` reconciliation
  does in production).
- A gap test: a weekend, a holiday, and a pre-listing date each return an empty
  series and no error.
- An HTTP-failure test: 429 and 5xx retry, 4xx does not.

Read `docs/fx-data-quality.md` before judging any new FX source — it records
what "good" looked like the last time this was measured, and how the cache can
mask a bad source. Clear the cached symbol before measuring anything.

Then run the `assay-verify` skill.
