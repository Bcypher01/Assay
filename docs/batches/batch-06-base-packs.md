# Batch 06 — Indicator, price-action and time packs

**Depends on:** 05
**Blocks:** 07 (needs at least one real pack)
**Packages:** `internal/packs/indicator`, `internal/packs/priceaction`, `internal/packs/timeofday`

## Goal

Populate the registry with the primitives that make the product general rather
than SMC-specific. These are individually cheap — RSI is twenty lines — and
collectively they prove the registry against real detectors before the expensive
SMC work begins.

## Deliverables

**indicator** — MA family (SMA, EMA, WMA) and slope, MA touch and cross, RSI,
MACD, Bollinger Bands, ATR, ADX, Stochastic, volume and OBV

**priceaction** — swing highs and lows (fractal), support and resistance
clusters, candlestick patterns (engulfing, pin bar, inside bar, doji),
consolidation and range detection, measured move projection

**timeofday** — session windows (London, New York, Asia, custom), day of week,
time-of-day range, bar-of-session index

Each primitive ships with: a proto parameter message, an overlay implementation,
a golden-value fixture, and documentation of its exact definition.

## Key decisions

**Golden values come from a reference implementation, not from intuition.**
Pick one reference per indicator, record it in the decision log, and assert
against it. "Looks about right" is how indicator bugs survive for years.

**Wilder smoothing versus simple smoothing is a parameter, not a choice.** RSI,
ATR and ADX all have competing conventions in the wild. Expose the convention;
default to Wilder; document it.

**Warm-up periods are explicit.** Every primitive reports how many bars it needs
before its output is valid, and the driver marks those bars invalid rather than
emitting garbage. Silent warm-up is a classic source of inflated early results.

**Session definitions are instrument-aware.** "London" means different clock
times depending on DST and on the instrument's calendar. This delegates to
`instrument.Calendar` rather than hardcoding hours.

## Interfaces

Nothing new — everything implements `primitive.Primitive` from batch 05. What
this batch adds is the pattern other packs follow:

```go
// One file per primitive. Params proto beside it. Golden fixture in testdata.
func init() { primitive.MustRegister(&RSI{}) }

type RSI struct{}
func (RSI) Spec() primitive.Spec { /* Out: KindNumber, Timeframed: true */ }
func (RSI) New(p proto.Message, tf core.TF) (primitive.Instance, error)
```

## Tests

- Golden values against the chosen reference, for at least three instruments and
  including the warm-up window
- Warm-up: bars before validity are marked invalid, and the driver excludes them
- Overlay output is stable and within the requested range
- Session primitives across DST transitions in both directions
- Benchmarks for the three most expensive primitives over 2.6M bars

## Exit criteria

- [ ] Every primitive matches its reference implementation within documented
      tolerance
- [ ] Every primitive declares a warm-up period and it is enforced
- [ ] Every primitive has a proto parameter message and generates a UI schema
- [ ] Definitions documented in `docs/primitives.md`, one entry each

## Out of scope

SMC primitives (batch 09). Exit primitives — those live with the simulator's
target and stop plans in batch 04 and are wired in batch 07.

## Decision log

_Record the reference implementation per indicator, and the smoothing defaults._
