# Batch 10 — Analytics and the credibility layer

**Depends on:** 07
**Blocks:** 11
**Packages:** `internal/analytics`

## Goal

The reason the product exists. Turn single runs into studies whose output a
sceptical reader would accept: out-of-sample by default, distributions rather
than point estimates, and every source of uncertainty surfaced rather than
resolved away.

## Deliverables

- **Walk-forward orchestration** — optimise on window N, test on N+1, roll;
  anchored and rolling variants; the out-of-sample curve is the primary result
- **Parameter sweeps** — bounded worker pool, memoised detection, deterministic
  ordering of results regardless of completion order
- **Monte Carlo** — trade-order resampling and block bootstrap of returns;
  distributions of max drawdown, final equity and time to recovery, with the
  realised path located inside them
- **Sensitivity grids** — performance across parameter pairs, structured for a
  heatmap
- **Trial counting** — configurations tested against an instrument and period,
  with a deflated performance metric adjusting for multiplicity
- **Cost sensitivity** — re-run at 1×, 2× and 3× assumed spread and slippage
- **Bounds propagation** — every metric affected by unresolved bars reported as a
  pessimistic-to-optimistic range, never collapsed
- Metrics: CAGR, max and average drawdown with recovery time, Sharpe, Sortino,
  profit factor, expectancy in R, longest losing streak, exposure, per-instrument
  breakdown

## Key decisions

**Prototype the statistics in Python, then port.** Deciding what the bootstrap,
deflation and window logic should be while simultaneously writing them in Go is
slow. Export trade lists, settle the formulas against numpy, port the settled
version. Record the reference notebook in the decision log.

**Out-of-sample is the primary result, structurally.** The API does not return an
in-sample headline figure without its out-of-sample counterpart. This is enforced
in the result type, not left to the UI — a UI can be redesigned, a type cannot be
bypassed.

**Bounds never collapse.** A metric derived from trades with unresolved ordering
carries its range all the way to the wire. Any code path that picks one bound to
display a single number is a bug.

**Deterministic result ordering.** Sweep results are written into a preallocated
indexed slice, never appended from goroutines. Two identical sweeps produce
identical output including order.

**Seeds are explicit and recorded.** Monte Carlo results must be reproducible
from the study document alone.

## Interfaces

```go
type Study struct {
    Strategy   *ir.Strategy
    Sweep      []ParamAxis
    WalkForward WFConfig
    MonteCarlo  MCConfig
    Seed        int64
}

type StudyResult struct {
    OutOfSample Metrics          // primary
    InSample    Metrics          // subordinate, never returned alone
    Distribution MCDistribution
    Sensitivity  Grid
    Trials       int
    Deflated     Metrics
    CostCurve    []Metrics       // 1x, 2x, 3x
    Unresolved   BoundsSummary
}

type Bounded struct{ Pessimistic, Optimistic float64 }
```

Every field in `Metrics` affected by ambiguity is `Bounded`, not `float64`.

## Tests

- Walk-forward: window boundaries, no leakage between optimisation and test
  windows — asserted by a strategy that memorises its optimisation window and
  must fail out of sample
- Monte Carlo: with a fixed seed, distributions are reproducible; the realised
  path falls inside the distribution
- Sensitivity: a synthetic knife-edge strategy produces a visibly isolated cell
- Trial counting increments once per distinct configuration hash, not per run
- Bounds: a study containing ambiguous trades produces ranges on every affected
  metric, and none collapse
- Sweep determinism: identical output including ordering across repeated runs
- Benchmark: 1,000-config sweep on 8 cores

## Exit criteria

- [ ] 1,000-config sweep completes in under 60 seconds on 8 cores with memoisation
- [ ] The leakage test fails a leaky implementation and passes a correct one
- [ ] No API path returns an in-sample metric without its out-of-sample pair
- [ ] Monte Carlo reproducible from seed alone
- [ ] Every ambiguity-affected metric is `Bounded` at the type level

## Out of scope

Persisting results (batch 11). Charts. The "what would fix this" suggestion
feature — deliberately deferred, since a naive version is an overfitting machine.

## Decision log

_Record the deflation formula and its source, the bootstrap block length, and the Python reference notebook._
