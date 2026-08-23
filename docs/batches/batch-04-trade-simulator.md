# Batch 04 — Trade simulator

**Depends on:** 00
**Blocks:** 07
**Packages:** `internal/sim`

## Goal

Turn signal arrays plus OHLC into trades, correctly and cheaply. This holds the
logic most likely to silently inflate results, so it is built and proven **before
any detector exists**, against synthetic signals.

## Deliverables

- Order types: market, limit, stop, with per-instrument gap policy on fills
- Position lifecycle: open, partial close, break-even move, trailing, full close
- Cost application: spread, commission, slippage model, funding and swap accrual
- Sizing: from stop distance and risk model, rounded to lot step, with rounding
  error recorded per trade
- Portfolio constraints: max concurrent positions, daily loss cutoff, direction
  filter — enforced across instruments, not per instrument
- **Ambiguity detection and bounding**: when a bar's range contains both stop and
  target, evaluate both orderings and carry a pessimistic and optimistic result
  through the whole run
- Intrabar narrowing: when a finer series is available for a bar, replay it to
  resolve or tighten the ordering
- `RunResult`: trades, equity series, per-trade diagnostics, unresolved count

## Key decisions

**Built against synthetic signals first.** Detectors and fills are each easy to
get wrong; debugging them against each other is much harder than debugging either
alone. Signal arrays are trivially constructible in tests.

**Dual-path evaluation, not a fill assumption.** Every other backtester picks an
ordering — usually the flattering one. Carrying both bounds is the product's
central honesty claim and it is implemented here or nowhere.

**Slippage defaults to non-zero.** A zero default is a lie that most users will
never change.

**Rounding error is reported, not swallowed.** On small accounts, lot-step
rounding materially changes results and no other tool surfaces it.

**No allocation per bar.** The simulator runs thousands of times per sweep.
Trades append into a preallocated slice sized from an estimate, growing at most
once.

## Interfaces

```go
type Signals struct {
    Enter      *core.Bitset       // per bar of the entry timeframe
    Side       []core.Side        // aligned with set bits of Enter
    StopLevel  []core.Price
    TargetPlan []TargetPlan
}

type Config struct {
    Spec        instrument.Spec
    Risk        RiskModel
    MaxConcurrent int
    DailyLossCap  core.Ratio
    Slippage      SlippageModel
}

type RunResult struct {
    Trades       []Trade          // each carries Pessimistic and Optimistic outcome
    Equity       []core.Price     // pessimistic path
    EquityOpt    []core.Price     // optimistic path
    Unresolved   int
    RoundingLoss core.Price
}

func Run(ctx context.Context, s *core.Series, fine *core.Series, sig Signals, cfg Config, out *RunResult) error
```

`fine` may be nil. When present it is used only to narrow ambiguous bars.

## Tests

- Table tests per order type against hand-computed fills, including gaps over
  weekends and session closes
- Ambiguity: a constructed bar containing both levels yields distinct pessimistic
  and optimistic outcomes, and the unresolved count increments exactly once
- Narrowing: supplying a fine series for that bar collapses the range correctly
- Costs: spread, commission, slippage and swap each verified in isolation, then
  combined
- Sizing: rounding behaviour at lot steps, including the case where risk rounds
  to zero size — which must be a skipped trade, not a zero-size position
- Portfolio: concurrency and daily-loss limits across two instruments
- Fuzz the fill logic against invariants: no position exceeds risk, no trade
  closes before it opens, equity is continuous
- Determinism test and an allocation-count benchmark

## Exit criteria

- [ ] A run over 2.6M bars allocates a bounded, series-independent number of times
- [ ] Ambiguous trades produce a strictly wider bound than resolved ones
- [ ] Fuzzing finds no invariant violation over a sustained run
- [ ] Same inputs twice produce byte-identical `RunResult`

## Out of scope

Anything that produces signals. Walk-forward or sweeps (batch 10). Fetching fine
data (batch 03 supplies it).

## Decision log

- The fill model uses the instrument's fixed slippage by default; it is applied
  adversely at entry and is intentionally never inferred as zero. `Config` can
  explicitly override it with another fixed model.
- Trades and open-position storage start at the number of sparse entry signals.
  `RunResult` retains its backing slices across runs, so an identical or smaller
  subsequent run allocates no result storage.
